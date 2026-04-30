package eventbus

import (
	"testing"

	lua "github.com/akzj/go-lua/pkg/lua"
)

func newTestState(t *testing.T) *lua.State {
	t.Helper()
	L := lua.NewState()
	Register(L, Config{})
	return L
}

func TestEventBus_BasicEmit(t *testing.T) {
	L := newTestState(t)
	defer L.Close()

	err := L.DoString(`
		local eventbus = require("eventbus")
		local bus = eventbus.new()

		local received = nil
		bus:on("test.event", function(event)
			received = event.msg
		end)

		bus:emit("test.event", {msg = "hello"})
		assert(received == "hello", "expected 'hello', got: " .. tostring(received))
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	L := newTestState(t)
	defer L.Close()

	err := L.DoString(`
		local eventbus = require("eventbus")
		local bus = eventbus.new()

		local count = 0
		bus:on("ping", function(e) count = count + 1 end)
		bus:on("ping", function(e) count = count + 10 end)

		bus:emit("ping", {})
		assert(count == 11, "expected 11, got: " .. tostring(count))
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEventBus_WildcardStar(t *testing.T) {
	L := newTestState(t)
	defer L.Close()

	err := L.DoString(`
		local eventbus = require("eventbus")
		local bus = eventbus.new()

		local topics = {}
		bus:on("user.*", function(event)
			table.insert(topics, event._topic)
		end)

		bus:emit("user.created", {})
		bus:emit("user.deleted", {})
		bus:emit("order.created", {})  -- should NOT match

		assert(#topics == 2, "expected 2, got: " .. tostring(#topics))
		assert(topics[1] == "user.created")
		assert(topics[2] == "user.deleted")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEventBus_WildcardDoubleStar(t *testing.T) {
	L := newTestState(t)
	defer L.Close()

	err := L.DoString(`
		local eventbus = require("eventbus")
		local bus = eventbus.new()

		local count = 0
		bus:on("app.**", function(e) count = count + 1 end)

		bus:emit("app.start", {})
		bus:emit("app.module.loaded", {})
		bus:emit("app.module.sub.deep", {})
		bus:emit("other.event", {})  -- should NOT match

		assert(count == 3, "expected 3, got: " .. tostring(count))
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEventBus_Once(t *testing.T) {
	L := newTestState(t)
	defer L.Close()

	err := L.DoString(`
		local eventbus = require("eventbus")
		local bus = eventbus.new()

		local count = 0
		bus:once("fire", function(e) count = count + 1 end)

		bus:emit("fire", {})
		bus:emit("fire", {})
		bus:emit("fire", {})

		assert(count == 1, "expected 1, got: " .. tostring(count))
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEventBus_Off(t *testing.T) {
	L := newTestState(t)
	defer L.Close()

	err := L.DoString(`
		local eventbus = require("eventbus")
		local bus = eventbus.new()

		local count = 0
		local id = bus:on("tick", function(e) count = count + 1 end)

		bus:emit("tick", {})
		assert(count == 1)

		bus:off(id)
		bus:emit("tick", {})
		assert(count == 1, "should still be 1 after off")
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEventBus_Clear(t *testing.T) {
	L := newTestState(t)
	defer L.Close()

	err := L.DoString(`
		local eventbus = require("eventbus")
		local bus = eventbus.new()

		bus:on("a", function(e) end)
		bus:on("b", function(e) end)
		bus:on("c", function(e) end)

		assert(bus:count() == 3)
		bus:clear()
		assert(bus:count() == 0)
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEventBus_Count(t *testing.T) {
	L := newTestState(t)
	defer L.Close()

	err := L.DoString(`
		local eventbus = require("eventbus")
		local bus = eventbus.new()

		bus:on("user.created", function(e) end)
		bus:on("user.deleted", function(e) end)
		bus:on("order.created", function(e) end)

		assert(bus:count() == 3)
		assert(bus:count("user.*") >= 2)
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEventBus_TopicInjected(t *testing.T) {
	L := newTestState(t)
	defer L.Close()

	err := L.DoString(`
		local eventbus = require("eventbus")
		local bus = eventbus.new()

		local got_topic = nil
		bus:on("my.topic", function(event)
			got_topic = event._topic
		end)

		bus:emit("my.topic", {data = 123})
		assert(got_topic == "my.topic", "expected 'my.topic', got: " .. tostring(got_topic))
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestEventBus_EmitWithoutData(t *testing.T) {
	L := newTestState(t)
	defer L.Close()

	err := L.DoString(`
		local eventbus = require("eventbus")
		local bus = eventbus.new()

		local got = false
		bus:on("signal", function(event)
			got = true
			assert(event._topic == "signal")
		end)

		bus:emit("signal")  -- no data argument
		assert(got == true)
	`)
	if err != nil {
		t.Fatal(err)
	}
}

// Test topic matching function directly
func TestTopicMatch(t *testing.T) {
	cases := []struct {
		pattern string
		topic   string
		want    bool
	}{
		{"user.created", "user.created", true},
		{"user.created", "user.deleted", false},
		{"user.*", "user.created", true},
		{"user.*", "user.deleted", true},
		{"user.*", "order.created", false},
		{"user.*", "user.a.b", false}, // * matches exactly one segment
		{"app.**", "app.start", true},
		{"app.**", "app.module.loaded", true},
		{"app.**", "app.a.b.c.d", true},
		{"**", "anything.at.all", true},
		{"*", "single", true},
		{"*", "a.b", false},
	}

	for _, tc := range cases {
		got := topicMatch(tc.pattern, tc.topic)
		if got != tc.want {
			t.Errorf("topicMatch(%q, %q) = %v, want %v", tc.pattern, tc.topic, got, tc.want)
		}
	}
}
