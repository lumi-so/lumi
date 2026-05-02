// Package nats provides NATS messaging support for Lumi using nats-io/nats.go.
// It exposes core NATS functionality (connect, publish, subscribe, request/reply)
// to Lua via go-lua bindings. All operations are synchronous and goroutine-safe.
package nats

import (
	"time"

	natscli "github.com/nats-io/nats.go"

	lua "github.com/akzj/go-lua/pkg/lua"
)

// Config configures the nats module. Currently a placeholder.
type Config struct{}

// Register registers the "nats" module in the Lua state.
func Register(L *lua.State, cfg Config) {
	lua.RegisterModule(L, "nats", map[string]lua.Function{
		"connect": lua.WrapSafe(connect),
	})
}

// ---------------------------------------------------------------------------
// connect(url [, opts]) → conn
// ---------------------------------------------------------------------------

func connect(L *lua.State) int {
	url := L.CheckString(1)

	var natsOpts []natscli.Option

	if L.GetTop() >= 2 && L.IsTable(2) {
		natsOpts = parseConnectOpts(L, 2)
	}

	nc, err := natscli.Connect(url, natsOpts...)
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	pushConn(L, nc)
	return 1
}

func parseConnectOpts(L *lua.State, idx int) []natscli.Option {
	var opts []natscli.Option

	// name
	L.GetField(idx, "name")
	if s, ok := L.ToString(-1); ok && s != "" {
		opts = append(opts, natscli.Name(s))
	}
	L.Pop(1)

	// token
	L.GetField(idx, "token")
	if s, ok := L.ToString(-1); ok && s != "" {
		opts = append(opts, natscli.Token(s))
	}
	L.Pop(1)

	// user + password
	L.GetField(idx, "user")
	user, _ := L.ToString(-1)
	L.Pop(1)
	L.GetField(idx, "password")
	pass, _ := L.ToString(-1)
	L.Pop(1)
	if user != "" || pass != "" {
		opts = append(opts, natscli.UserInfo(user, pass))
	}

	// max_reconnects
	L.GetField(idx, "max_reconnects")
	if n, ok := L.ToInteger(-1); ok {
		opts = append(opts, natscli.MaxReconnects(int(n)))
	}
	L.Pop(1)

	// reconnect_wait_ms
	L.GetField(idx, "reconnect_wait_ms")
	if n, ok := L.ToInteger(-1); ok {
		opts = append(opts, natscli.ReconnectWait(time.Duration(n)*time.Millisecond))
	}
	L.Pop(1)

	return opts
}

// ---------------------------------------------------------------------------
// Connection wrapper
// ---------------------------------------------------------------------------

type natsConn struct {
	nc *natscli.Conn
}

func pushConn(L *lua.State, nc *natscli.Conn) {
	c := &natsConn{nc: nc}
	L.PushUserdata(c)

	if L.NewMetatable("lumi.nats.conn") {
		methods := map[string]lua.Function{
			"publish":     lua.WrapSafe(connPublish),
			"publish_msg": lua.WrapSafe(connPublishMsg),
			"subscribe":   lua.WrapSafe(connSubscribe),
			"request":     lua.WrapSafe(connRequest),
			"close":       lua.WrapSafe(connClose),
		}
		L.NewTable()
		L.SetFuncs(methods, 0)
		L.SetField(-2, "__index")
	}
	L.SetMetatable(-2)
}

func getConn(L *lua.State) *natsConn {
	return L.UserdataValue(1).(*natsConn)
}

// conn:publish(subject, data)
// Returns: true on success, or nil, err on failure.
func connPublish(L *lua.State) int {
	c := getConn(L)
	subject := L.CheckString(2)
	data := L.CheckString(3)

	err := c.nc.Publish(subject, []byte(data))
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}

// conn:publish_msg(subject, reply, data)
// Returns: true on success, or nil, err on failure.
func connPublishMsg(L *lua.State) int {
	c := getConn(L)
	subject := L.CheckString(2)
	reply := L.CheckString(3)
	data := L.CheckString(4)

	msg := natscli.NewMsg(subject)
	msg.Reply = reply
	msg.Data = []byte(data)

	err := c.nc.PublishMsg(msg)
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}

// conn:subscribe(subject) → subscription
// Returns: subscription userdata on success, or nil, err on failure.
func connSubscribe(L *lua.State) int {
	c := getConn(L)
	subject := L.CheckString(2)

	sub, err := c.nc.SubscribeSync(subject)
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	pushSub(L, sub)
	return 1
}

// conn:request(subject, data, timeout_ms) → msg
// Returns: message userdata on success, or nil, err on failure/timeout.
func connRequest(L *lua.State) int {
	c := getConn(L)
	subject := L.CheckString(2)
	data := L.CheckString(3)

	timeout := 5 * time.Second
	if L.GetTop() >= 4 {
		if n, ok := L.ToInteger(4); ok {
			timeout = time.Duration(n) * time.Millisecond
		}
	}

	msg, err := c.nc.Request(subject, []byte(data), timeout)
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	pushMsg(L, msg)
	return 1
}

// conn:close()
func connClose(L *lua.State) int {
	c := getConn(L)
	c.nc.Close()
	return 0
}

// ---------------------------------------------------------------------------
// Subscription wrapper
// ---------------------------------------------------------------------------

type natsSub struct {
	sub *natscli.Subscription
}

func pushSub(L *lua.State, sub *natscli.Subscription) {
	s := &natsSub{sub: sub}
	L.PushUserdata(s)

	if L.NewMetatable("lumi.nats.sub") {
		methods := map[string]lua.Function{
			"next_msg":    lua.WrapSafe(subNextMsg),
			"unsubscribe": lua.WrapSafe(subUnsubscribe),
			"drain":       lua.WrapSafe(subDrain),
		}
		L.NewTable()
		L.SetFuncs(methods, 0)
		L.SetField(-2, "__index")
	}
	L.SetMetatable(-2)
}

func getSub(L *lua.State) *natsSub {
	return L.UserdataValue(1).(*natsSub)
}

// sub:next_msg(timeout_ms) → msg or nil
// Returns: message userdata on success, nil on timeout, nil+err on error.
func subNextMsg(L *lua.State) int {
	s := getSub(L)

	timeout := 5 * time.Second
	if L.GetTop() >= 2 {
		if n, ok := L.ToInteger(2); ok {
			timeout = time.Duration(n) * time.Millisecond
		}
	}

	msg, err := s.sub.NextMsg(timeout)
	if err != nil {
		// nats: timeout returns ErrTimeout — treat as nil (no message)
		if err == natscli.ErrTimeout {
			L.PushNil()
			return 1
		}
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}

	pushMsg(L, msg)
	return 1
}

// sub:unsubscribe()
func subUnsubscribe(L *lua.State) int {
	s := getSub(L)
	_ = s.sub.Unsubscribe()
	return 0
}

// sub:drain()
func subDrain(L *lua.State) int {
	s := getSub(L)
	_ = s.sub.Drain()
	return 0
}

// ---------------------------------------------------------------------------
// Message wrapper
// ---------------------------------------------------------------------------

type natsMsg struct {
	msg *natscli.Msg
}

func pushMsg(L *lua.State, msg *natscli.Msg) {
	m := &natsMsg{msg: msg}
	L.PushUserdata(m)

	if L.NewMetatable("lumi.nats.msg") {
		methods := map[string]lua.Function{
			"subject": lua.WrapSafe(msgSubject),
			"data":    lua.WrapSafe(msgData),
			"reply":   lua.WrapSafe(msgReply),
			"respond": lua.WrapSafe(msgRespond),
		}
		L.NewTable()
		L.SetFuncs(methods, 0)
		L.SetField(-2, "__index")
	}
	L.SetMetatable(-2)
}

func getMsg(L *lua.State) *natsMsg {
	return L.UserdataValue(1).(*natsMsg)
}

// msg:subject() → string
func msgSubject(L *lua.State) int {
	m := getMsg(L)
	L.PushString(m.msg.Subject)
	return 1
}

// msg:data() → string
func msgData(L *lua.State) int {
	m := getMsg(L)
	L.PushString(string(m.msg.Data))
	return 1
}

// msg:reply() → string or nil
func msgReply(L *lua.State) int {
	m := getMsg(L)
	if m.msg.Reply == "" {
		L.PushNil()
	} else {
		L.PushString(m.msg.Reply)
	}
	return 1
}

// msg:respond(data)
// Returns: true on success, or nil, err on failure.
func msgRespond(L *lua.State) int {
	m := getMsg(L)
	data := L.CheckString(2)

	err := m.msg.Respond([]byte(data))
	if err != nil {
		L.PushNil()
		L.PushString(err.Error())
		return 2
	}
	L.PushBoolean(true)
	return 1
}
