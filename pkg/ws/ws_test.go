package ws

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	lua "github.com/akzj/go-lua/pkg/lua"
	"github.com/lumi-so/lumi/pkg/web"
)

func setupWSTest(t *testing.T, luaCode string) (*httptest.Server, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	engine := web.New(web.Config{
		PoolSize: 4,
		GinMode:  "test",
		InitFunc: func(L *lua.State) {
			if err := L.DoString(luaCode); err != nil {
				t.Fatalf("lua init failed: %v", err)
			}
		},
	})

	engine.Router.GET("/ws", Handler(engine, "ws_handler"))

	server := httptest.NewServer(engine.Router)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	return server, wsURL
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	return conn
}

func TestWS_Echo(t *testing.T) {
	server, wsURL := setupWSTest(t, `
		function ws_handler(conn)
			while true do
				local msg, err = conn:recv()
				if err then break end
				conn:send("Echo: " .. msg)
			end
		end
	`)
	defer server.Close()

	conn := dial(t, wsURL)
	defer conn.Close()

	// Send and receive
	if err := conn.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != "Echo: hello" {
		t.Fatalf("got %q, want %q", msg, "Echo: hello")
	}

	// Send another
	if err := conn.WriteMessage(websocket.TextMessage, []byte("world")); err != nil {
		t.Fatal(err)
	}
	_, msg, err = conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != "Echo: world" {
		t.Fatalf("got %q, want %q", msg, "Echo: world")
	}
}

func TestWS_SendJSON(t *testing.T) {
	server, wsURL := setupWSTest(t, `
		function ws_handler(conn)
			conn:send_json({type = "welcome", msg = "hello"})
			local data, err = conn:recv_json()
			if err then return end
			conn:send_json({type = "echo", data = data})
		end
	`)
	defer server.Close()

	conn := dial(t, wsURL)
	defer conn.Close()

	// Read welcome message
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var welcome map[string]any
	if err := json.Unmarshal(msg, &welcome); err != nil {
		t.Fatalf("unmarshal welcome: %v", err)
	}
	if welcome["type"] != "welcome" {
		t.Fatalf("got type %v, want welcome", welcome["type"])
	}

	// Send JSON
	if err := conn.WriteJSON(map[string]any{"ping": true}); err != nil {
		t.Fatal(err)
	}

	// Read echo
	_, msg, err = conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var echo map[string]any
	if err := json.Unmarshal(msg, &echo); err != nil {
		t.Fatalf("unmarshal echo: %v", err)
	}
	if echo["type"] != "echo" {
		t.Fatalf("got type %v, want echo", echo["type"])
	}
}

func TestWS_ClientClose(t *testing.T) {
	server, wsURL := setupWSTest(t, `
		function ws_handler(conn)
			conn:send("start")
			local msg, err = conn:recv()
			-- err should be non-nil when client closes
		end
	`)
	defer server.Close()

	conn := dial(t, wsURL)

	// Read start message
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != "start" {
		t.Fatalf("got %q, want 'start'", msg)
	}

	// Close client connection
	conn.Close()

	// Give server time to process
	time.Sleep(200 * time.Millisecond)
}

func TestWS_ServerClose(t *testing.T) {
	server, wsURL := setupWSTest(t, `
		function ws_handler(conn)
			conn:send("bye")
			conn:close()
		end
	`)
	defer server.Close()

	conn := dial(t, wsURL)
	defer conn.Close()

	// Read message before close
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != "bye" {
		t.Fatalf("got %q, want 'bye'", msg)
	}
}

func TestWS_MultipleMessages(t *testing.T) {
	server, wsURL := setupWSTest(t, `
		function ws_handler(conn)
			for i = 1, 3 do
				local msg, err = conn:recv()
				if err then break end
				conn:send(msg .. ":" .. tostring(i))
			end
		end
	`)
	defer server.Close()

	conn := dial(t, wsURL)
	defer conn.Close()

	for i := 1; i <= 3; i++ {
		if err := conn.WriteMessage(websocket.TextMessage, []byte("test")); err != nil {
			t.Fatal(err)
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		expected := fmt.Sprintf("test:%d", i)
		if string(msg) != expected {
			t.Fatalf("msg %d: got %q, want %q", i, msg, expected)
		}
	}
}
