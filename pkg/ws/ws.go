// Package ws provides WebSocket support for Lumi using gorilla/websocket.
// It integrates with the web.Engine to allow Lua handlers to manage
// WebSocket connections with send/recv methods.
package ws

import (
	"encoding/json"
	"net/http"
	"sync"

	lua "github.com/akzj/go-lua/pkg/lua"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/lumi-so/lumi/pkg/web"
)

// Config configures the WebSocket upgrader.
type Config struct {
	// ReadBufferSize is the I/O buffer size for reads (default: 1024).
	ReadBufferSize int

	// WriteBufferSize is the I/O buffer size for writes (default: 1024).
	WriteBufferSize int

	// CheckOrigin validates the request origin.
	// If nil, allows all origins (development mode).
	CheckOrigin func(r *http.Request) bool
}

// Register registers the "ws" module in the Lua state.
// Currently a placeholder for future utility functions (broadcast, rooms, etc.).
func Register(L *lua.State, cfg Config) {
	lua.RegisterModule(L, "ws", map[string]lua.Function{
		// Future: ws.broadcast, ws.rooms, etc.
	})
}

// Handler creates a gin.HandlerFunc that upgrades to WebSocket
// and calls the named Lua global function with a conn proxy.
//
// The Lua function signature is: function handler(conn) ... end
// where conn has methods: send, send_json, send_binary, recv, recv_json, close.
func Handler(engine *web.Engine, funcName string, cfgs ...Config) gin.HandlerFunc {
	cfg := Config{}
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if cfg.ReadBufferSize <= 0 {
		cfg.ReadBufferSize = 1024
	}
	if cfg.WriteBufferSize <= 0 {
		cfg.WriteBufferSize = 1024
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  cfg.ReadBufferSize,
		WriteBufferSize: cfg.WriteBufferSize,
		CheckOrigin:     cfg.CheckOrigin,
	}
	if upgrader.CheckOrigin == nil {
		upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	}

	return func(c *gin.Context) {
		// 1. Upgrade HTTP → WebSocket
		wsConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return // Upgrade already sent error response
		}
		defer wsConn.Close()

		// 2. Get Lua state from pool
		L := engine.Pool().Get()
		defer func() {
			L.DeleteUserValue("gin_ctx")
			L.SetTop(0)
			engine.Pool().Put(L)
		}()

		L.SetContext(c.Request.Context())
		L.SetUserValue("gin_ctx", c)

		// 3. Call Lua handler: funcName(conn)
		L.GetGlobal(funcName)
		pushConnProxy(L, wsConn)
		if err := L.SafeCall(1, 0); err != nil {
			// Handler error — connection will be closed by defer
			_ = err
		}
	}
}

// --- WebSocket Connection Proxy ---

// conn wraps a gorilla/websocket.Conn with a mutex for safe writes.
type conn struct {
	ws     *websocket.Conn
	mu     sync.Mutex // protects writes
	closed bool
}

// pushConnProxy pushes a userdata representing the WebSocket connection
// with a metatable providing send/recv/close methods.
func pushConnProxy(L *lua.State, ws *websocket.Conn) {
	c := &conn{ws: ws}
	L.PushUserdata(c)

	if L.NewMetatable("lumi.ws.conn") {
		methods := map[string]lua.Function{
			"send":        lua.WrapSafe(connSend),
			"send_json":   lua.WrapSafe(connSendJSON),
			"send_binary": lua.WrapSafe(connSendBinary),
			"recv":        lua.WrapSafe(connRecv),
			"recv_json":   lua.WrapSafe(connRecvJSON),
			"close":       lua.WrapSafe(connClose),
		}
		L.NewTable()
		L.SetFuncs(methods, 0)
		L.SetField(-2, "__index")
	}
	L.SetMetatable(-2)
}

func getConn(L *lua.State) *conn {
	return L.UserdataValue(1).(*conn)
}

// conn:send(text) — send text message
// Returns: true on success, or nil, err on failure.
func connSend(L *lua.State) int {
	c := getConn(L)
	msg := L.CheckString(2)

	c.mu.Lock()
	err := c.ws.WriteMessage(websocket.TextMessage, []byte(msg))
	c.mu.Unlock()

	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}

// conn:send_json(table) — serialize table to JSON and send as text message.
// Returns: true on success, or nil, err on failure.
func connSendJSON(L *lua.State) int {
	c := getConn(L)
	data := L.ToAny(2)

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		L.PushNil()
		L.PushString("json encode: " + err.Error())
		return 2
	}

	c.mu.Lock()
	err = c.ws.WriteMessage(websocket.TextMessage, jsonBytes)
	c.mu.Unlock()

	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}

// conn:send_binary(data) — send binary message.
// Returns: true on success, or nil, err on failure.
func connSendBinary(L *lua.State) int {
	c := getConn(L)
	msg := L.CheckString(2)

	c.mu.Lock()
	err := c.ws.WriteMessage(websocket.BinaryMessage, []byte(msg))
	c.mu.Unlock()

	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}

// conn:recv() → text, err — receive next message (blocks until message arrives).
// Returns: message string on success, or nil, err on failure/close.
func connRecv(L *lua.State) int {
	c := getConn(L)

	_, msg, err := c.ws.ReadMessage()
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	L.PushString(string(msg))
	return 1
}

// conn:recv_json() → table, err — receive and parse JSON message.
// Returns: Lua table on success, or nil, err on failure.
func connRecvJSON(L *lua.State) int {
	c := getConn(L)

	_, msg, err := c.ws.ReadMessage()
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	var data any
	if err := json.Unmarshal(msg, &data); err != nil {
		L.PushNil()
		L.PushString("json decode: " + err.Error())
		return 2
	}

	L.PushAny(data)
	return 1
}

// conn:close() or conn:close(code, message) — close the WebSocket connection.
// Default close code is 1000 (normal closure).
func connClose(L *lua.State) int {
	c := getConn(L)
	if c.closed {
		return 0
	}
	c.closed = true

	code := websocket.CloseNormalClosure
	msg := ""

	if L.GetTop() >= 2 {
		code = int(L.CheckInteger(2))
	}
	if L.GetTop() >= 3 {
		msg = L.CheckString(3)
	}

	closeMsg := websocket.FormatCloseMessage(code, msg)
	c.mu.Lock()
	_ = c.ws.WriteMessage(websocket.CloseMessage, closeMsg)
	c.mu.Unlock()
	c.ws.Close()

	return 0
}
