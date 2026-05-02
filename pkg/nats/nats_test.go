package nats

import (
	"testing"
	"time"

	natscli "github.com/nats-io/nats.go"
	"github.com/nats-io/nats-server/v2/server"

	lua "github.com/akzj/go-lua/pkg/lua"
)

// startEmbeddedNATS starts an embedded NATS server on a random port
// and returns the server and its client URL.
func startEmbeddedNATS(t *testing.T) (*server.Server, string) {
	t.Helper()

	opts := &server.Options{
		Host:   "127.0.0.1",
		Port:   -1, // random port
		NoLog:  true,
		NoSigs: true,
	}

	s, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("failed to create NATS server: %v", err)
	}

	s.Start()

	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server not ready within 5s")
	}

	t.Cleanup(func() {
		s.Shutdown()
	})

	return s, s.ClientURL()
}

// newLuaState creates a Lua state with the nats module registered.
func newLuaState(t *testing.T) *lua.State {
	t.Helper()

	L := lua.NewState()
	t.Cleanup(func() {
		L.Close()
	})

	Register(L, Config{})
	return L
}

// ---------------------------------------------------------------------------
// TestConnect
// ---------------------------------------------------------------------------

func TestConnect(t *testing.T) {
	_, url := startEmbeddedNATS(t)
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(conn ~= nil, "conn should not be nil")
		assert(err == nil, "err should be nil: " .. tostring(err))
		conn:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestConnectWithOptions
// ---------------------------------------------------------------------------

func TestConnectWithOptions(t *testing.T) {
	_, url := startEmbeddedNATS(t)
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `", {name = "lumi-test"})
		assert(conn ~= nil, "conn should not be nil")
		assert(err == nil, "err should be nil: " .. tostring(err))
		conn:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestConnectFailure
// ---------------------------------------------------------------------------

func TestConnectFailure(t *testing.T) {
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("nats://127.0.0.1:19999")
		assert(conn == nil, "conn should be nil on failure")
		assert(err ~= nil, "err should not be nil")
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestPublishSubscribe
// ---------------------------------------------------------------------------

func TestPublishSubscribe(t *testing.T) {
	_, url := startEmbeddedNATS(t)
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(err == nil, "connect failed: " .. tostring(err))

		local sub, err = conn:subscribe("test.pubsub")
		assert(err == nil, "subscribe failed: " .. tostring(err))
		assert(sub ~= nil, "sub should not be nil")

		local ok, err = conn:publish("test.pubsub", "hello-world")
		assert(ok == true, "publish failed: " .. tostring(err))

		local msg = sub:next_msg(2000)
		assert(msg ~= nil, "msg should not be nil (timeout)")
		assert(msg:subject() == "test.pubsub", "subject mismatch")
		assert(msg:data() == "hello-world", "data mismatch")
		assert(msg:reply() == nil, "reply should be nil")

		sub:unsubscribe()
		conn:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestRequestReply
// ---------------------------------------------------------------------------

func TestRequestReply(t *testing.T) {
	_, url := startEmbeddedNATS(t)
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(err == nil, "connect failed: " .. tostring(err))

		-- Set up a replier
		local sub, err = conn:subscribe("test.request")
		assert(err == nil, "subscribe failed: " .. tostring(err))

		-- Second connection for making requests
		local conn2, err = nats.connect("` + url + `")
		assert(err == nil, "connect2 failed: " .. tostring(err))

		-- Send request (times out since no responder yet, but msg is queued)
		local reply, err = conn2:request("test.request", "ping", 500)
		-- reply is nil on timeout, err is non-nil

		-- Get the request from the sub
		local req = sub:next_msg(2000)
		assert(req ~= nil, "should receive request")
		assert(req:data() == "ping", "request data mismatch")

		-- Respond to it
		local ok, err = req:respond("pong")
		assert(ok == true, "respond failed: " .. tostring(err))

		sub:unsubscribe()
		conn:close()
		conn2:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestNextMsgTimeout
// ---------------------------------------------------------------------------

func TestNextMsgTimeout(t *testing.T) {
	_, url := startEmbeddedNATS(t)
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(err == nil, "connect failed: " .. tostring(err))

		local sub, err = conn:subscribe("test.timeout")
		assert(err == nil, "subscribe failed: " .. tostring(err))

		-- No one publishes — should time out
		local msg = sub:next_msg(200)
		assert(msg == nil, "msg should be nil on timeout")

		sub:unsubscribe()
		conn:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestClose
// ---------------------------------------------------------------------------

func TestClose(t *testing.T) {
	_, url := startEmbeddedNATS(t)
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(err == nil, "connect failed: " .. tostring(err))
		conn:close()
		-- Closing again should not crash
		conn:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestPublishMsg
// ---------------------------------------------------------------------------

func TestPublishMsg(t *testing.T) {
	_, url := startEmbeddedNATS(t)
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(err == nil, "connect failed: " .. tostring(err))

		local sub, err = conn:subscribe("test.pubmsg")
		assert(err == nil, "subscribe failed: " .. tostring(err))

		local ok, err = conn:publish_msg("test.pubmsg", "test.reply", "body")
		assert(ok == true, "publish_msg failed: " .. tostring(err))

		local msg = sub:next_msg(2000)
		assert(msg ~= nil, "msg should not be nil")
		assert(msg:subject() == "test.pubmsg", "subject mismatch")
		assert(msg:data() == "body", "data mismatch")
		assert(msg:reply() == "test.reply", "reply mismatch")

		sub:unsubscribe()
		conn:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestMultipleMessages
// ---------------------------------------------------------------------------

func TestMultipleMessages(t *testing.T) {
	_, url := startEmbeddedNATS(t)
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(err == nil, "connect failed: " .. tostring(err))

		local sub, err = conn:subscribe("test.multi")
		assert(err == nil, "subscribe failed: " .. tostring(err))

		for i = 1, 5 do
			local ok, err = conn:publish("test.multi", "msg-" .. tostring(i))
			assert(ok == true, "publish " .. tostring(i) .. " failed: " .. tostring(err))
		end

		for i = 1, 5 do
			local msg = sub:next_msg(2000)
			assert(msg ~= nil, "msg " .. tostring(i) .. " should not be nil")
			assert(msg:data() == "msg-" .. tostring(i), "data mismatch at " .. tostring(i))
		end

		sub:unsubscribe()
		conn:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestDrain
// ---------------------------------------------------------------------------

func TestDrain(t *testing.T) {
	_, url := startEmbeddedNATS(t)
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(err == nil, "connect failed: " .. tostring(err))

		local sub, err = conn:subscribe("test.drain")
		assert(err == nil, "subscribe failed: " .. tostring(err))

		local ok, err = conn:publish("test.drain", "drain-msg")
		assert(ok == true, "publish failed: " .. tostring(err))

		sub:drain()

		conn:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestRespondWithoutRequest — Go-level test using NATS client directly
// ---------------------------------------------------------------------------

func TestRespondWithoutRequest(t *testing.T) {
	_, url := startEmbeddedNATS(t)

	// Connect via Go client
	nc, err := natscli.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()

	// Subscribe and respond via Go
	sub, err := nc.SubscribeSync("svc.echo")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	// Make a request
	reply, err := nc.Request("svc.echo", []byte("hello"), 2*time.Second)
	if err != nil {
		// The request might time out since we haven't set up responder yet
		// But the message should be in the sub queue
	}

	// Get the request from the sub
	req, err := sub.NextMsg(2 * time.Second)
	if err != nil {
		t.Fatalf("nextMsg: %v", err)
	}

	if string(req.Data) != "hello" {
		t.Fatalf("got %q, want %q", req.Data, "hello")
	}

	// Respond
	if err := req.Respond([]byte("world")); err != nil {
		t.Fatalf("respond: %v", err)
	}

	// If we had the reply from above, it would be "world"
	if reply != nil {
		if string(reply.Data) != "world" {
			t.Fatalf("reply got %q, want %q", reply.Data, "world")
		}
	}
}
