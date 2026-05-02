package nats

import (
	"sync"
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
// TestRequestReply — full round trip with Go-level async responder
// ---------------------------------------------------------------------------

func TestRequestReply(t *testing.T) {
	_, url := startEmbeddedNATS(t)

	// Set up an async responder in Go (goroutine-safe callback)
	nc, err := natscli.Connect(url)
	if err != nil {
		t.Fatalf("responder connect: %v", err)
	}
	defer nc.Close()

	nc.Subscribe("echo", func(msg *natscli.Msg) {
		response := "reply:" + string(msg.Data)
		msg.Respond([]byte(response))
	})

	// Requester in Lua
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(err == nil, "connect failed: " .. tostring(err))

		local reply, err = conn:request("echo", "hello", 2000)
		assert(err == nil, "request failed: " .. tostring(err))
		assert(reply ~= nil, "reply should not be nil")
		-- Reply subject starts with "_INBOX." (inbox prefix)
		assert(string.sub(reply:subject(), 1, 7) == "_INBOX.", "reply subject should start with _INBOX.")
		assert(reply:data() == "reply:hello", "unexpected reply data: " .. reply:data())
		assert(reply:reply() == nil, "reply.reply should be nil")

		-- Second request to verify reusability
		local reply2, err = conn:request("echo", "world", 2000)
		assert(err == nil, "request2 failed: " .. tostring(err))
		assert(reply2:data() == "reply:world", "unexpected reply2 data: " .. reply2:data())

		conn:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestRequestReplyTimeout — verify timeout when no responder exists
// ---------------------------------------------------------------------------

func TestRequestReplyTimeout(t *testing.T) {
	_, url := startEmbeddedNATS(t)
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(err == nil, "connect failed: " .. tostring(err))

		-- No one is subscribed to "nobody.home" — should time out
		local reply, err = conn:request("nobody.home", "ping", 300)
		assert(reply == nil, "reply should be nil on timeout")
		assert(err ~= nil, "err should not be nil on timeout")

		conn:close()
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
// TestNextMsgAfterUnsubscribe — verify error after unsubscribe
// ---------------------------------------------------------------------------

func TestNextMsgAfterUnsubscribe(t *testing.T) {
	_, url := startEmbeddedNATS(t)
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(err == nil, "connect failed: " .. tostring(err))

		local sub, err = conn:subscribe("test.unsub")
		assert(err == nil, "subscribe failed: " .. tostring(err))

		sub:unsubscribe()

		-- next_msg after unsubscribe should return nil + error
		local msg, err = sub:next_msg(500)
		assert(msg == nil, "msg should be nil after unsubscribe")
		assert(err ~= nil, "err should not be nil after unsubscribe")

		conn:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestPublishNoSubscribers — publishing without subscribers succeeds silently
// ---------------------------------------------------------------------------

func TestPublishNoSubscribers(t *testing.T) {
	_, url := startEmbeddedNATS(t)
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(err == nil, "connect failed: " .. tostring(err))

		-- Publish to a subject with no subscribers — should succeed
		local ok, err = conn:publish("no.one.listening", "hello")
		assert(ok == true, "publish should succeed even without subscribers")
		assert(err == nil, "err should be nil: " .. tostring(err))

		conn:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestBinaryData — verify binary/non-UTF8 data round-trips correctly
// ---------------------------------------------------------------------------

func TestBinaryData(t *testing.T) {
	_, url := startEmbeddedNATS(t)
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(err == nil, "connect failed: " .. tostring(err))

		local sub, err = conn:subscribe("test.binary")
		assert(err == nil, "subscribe failed: " .. tostring(err))

		-- Binary data with null bytes and high bytes
		local binary = string.char(0, 1, 2, 255, 254, 253)
		local ok, err = conn:publish("test.binary", binary)
		assert(ok == true, "publish binary failed: " .. tostring(err))

		local msg = sub:next_msg(2000)
		assert(msg ~= nil, "msg should not be nil")
		local data = msg:data()
		assert(#data == 6, "expected 6 bytes, got " .. tostring(#data))
		assert(string.byte(data, 1) == 0, "byte 1 mismatch")
		assert(string.byte(data, 2) == 1, "byte 2 mismatch")
		assert(string.byte(data, 3) == 2, "byte 3 mismatch")
		assert(string.byte(data, 4) == 255, "byte 4 mismatch")
		assert(string.byte(data, 5) == 254, "byte 5 mismatch")
		assert(string.byte(data, 6) == 253, "byte 6 mismatch")

		sub:unsubscribe()
		conn:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestRequestReplyBinary — request-reply with binary payload via Go responder
// ---------------------------------------------------------------------------

func TestRequestReplyBinary(t *testing.T) {
	_, url := startEmbeddedNATS(t)

	// Go-level responder that echoes binary data
	nc, err := natscli.Connect(url)
	if err != nil {
		t.Fatalf("responder connect: %v", err)
	}
	defer nc.Close()

	nc.Subscribe("bin.echo", func(msg *natscli.Msg) {
		// Prefix and return
		response := append([]byte("ok:"), msg.Data...)
		msg.Respond(response)
	})

	// Requester in Lua
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(err == nil, "connect failed: " .. tostring(err))

		-- Send binary with null bytes
		local payload = string.char(0, 255, 128, 0)
		local reply, err = conn:request("bin.echo", payload, 2000)
		assert(err == nil, "request failed: " .. tostring(err))
		assert(reply ~= nil, "reply should not be nil")

		local data = reply:data()
		-- Expected: "ok:" prefix + original payload (7 bytes total)
		assert(#data == 7, "expected 7 bytes, got " .. tostring(#data))
		assert(string.sub(data, 1, 3) == "ok:", "prefix mismatch")
		assert(string.byte(data, 4) == 0, "byte 4 mismatch")
		assert(string.byte(data, 5) == 255, "byte 5 mismatch")
		assert(string.byte(data, 6) == 128, "byte 6 mismatch")
		assert(string.byte(data, 7) == 0, "byte 7 mismatch")

		conn:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestConcurrentPublish — verify goroutine-safety with concurrent publishes
// ---------------------------------------------------------------------------

func TestConcurrentPublish(t *testing.T) {
	_, url := startEmbeddedNATS(t)
	L := newLuaState(t)

	code := `
		local nats = require("nats")
		local conn, err = nats.connect("` + url + `")
		assert(err == nil, "connect failed: " .. tostring(err))

		local sub, err = conn:subscribe("test.concurrent")
		assert(err == nil, "subscribe failed: " .. tostring(err))

		-- Publish many messages sequentially
		local received = {}
		local count = 0
		for i = 1, 20 do
			local ok, err = conn:publish("test.concurrent", "msg-" .. tostring(i))
			assert(ok == true, "publish " .. tostring(i) .. " failed")
		end

		for i = 1, 20 do
			local msg = sub:next_msg(2000)
			assert(msg ~= nil, "msg " .. tostring(i) .. " should not be nil")
			received[msg:data()] = true
			count = count + 1
		end

		-- Verify all 20 unique messages received
		assert(count == 20, "expected 20 messages, got " .. tostring(count))
		for i = 1, 20 do
			assert(received["msg-" .. tostring(i)], "missing msg-" .. tostring(i))
		end

		sub:unsubscribe()
		conn:close()
	`

	if err := L.DoString(code); err != nil {
		t.Fatalf("lua error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestGoLevelRequestReply — Go-level round trip using SubscribeSync + NextMsg
// ---------------------------------------------------------------------------

func TestGoLevelRequestReply(t *testing.T) {
	_, url := startEmbeddedNATS(t)

	nc, err := natscli.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()

	sub, err := nc.SubscribeSync("svc.echo")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Responder goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		req, err := sub.NextMsg(5 * time.Second)
		if err != nil {
			t.Errorf("nextMsg: %v", err)
			return
		}
		if string(req.Data) != "hello" {
			t.Errorf("got %q, want %q", req.Data, "hello")
		}
		if err := req.Respond([]byte("world")); err != nil {
			t.Errorf("respond: %v", err)
		}
	}()

	// Give goroutine time to start waiting
	time.Sleep(100 * time.Millisecond)

	reply, err := nc.Request("svc.echo", []byte("hello"), 2*time.Second)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if string(reply.Data) != "world" {
		t.Fatalf("reply got %q, want %q", reply.Data, "world")
	}

	wg.Wait()
	sub.Unsubscribe()
}
