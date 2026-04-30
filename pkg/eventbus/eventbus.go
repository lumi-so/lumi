// Package eventbus provides an in-process publish/subscribe event bus for Lua.
// Lua code can subscribe to topics with exact or wildcard patterns, emit events,
// and manage subscriptions. Handlers are called synchronously on emit.
package eventbus

import (
	"strings"
	"sync"

	lua "github.com/akzj/go-lua/pkg/lua"
)

// Config configures the eventbus module.
type Config struct{}

type subscription struct {
	id    int64
	topic string // original pattern (may contain * or **)
	ref   int    // Lua registry ref to the callback function
	once  bool   // fire once then auto-remove
}

type eventBus struct {
	mu     sync.Mutex
	subs   []subscription
	nextID int64
	L      *lua.State
}

// Register registers the "eventbus" module in the Lua state.
func Register(L *lua.State, cfg Config) {
	lua.RegisterModule(L, "eventbus", map[string]lua.Function{
		"new": lua.WrapSafe(eventbusNew),
	})
}

func eventbusNew(L *lua.State) int {
	bus := &eventBus{
		subs:   make([]subscription, 0, 16),
		nextID: 1,
		L:      L,
	}
	L.PushUserdata(bus)

	if L.NewMetatable("lumi.eventbus") {
		methods := map[string]lua.Function{
			"on":    lua.WrapSafe(eventbusOn),
			"once":  lua.WrapSafe(eventbusOnce),
			"off":   lua.WrapSafe(eventbusOff),
			"emit":  lua.WrapSafe(eventbusEmit),
			"clear": lua.WrapSafe(eventbusClear),
			"count": lua.WrapSafe(eventbusCount),
		}
		L.NewTable()
		L.SetFuncs(methods, 0)
		L.SetField(-2, "__index")
	}
	L.SetMetatable(-2)
	return 1
}

func getBus(L *lua.State) *eventBus {
	return L.UserdataValue(1).(*eventBus)
}

// bus:on(topic, callback) → sub_id
func eventbusOn(L *lua.State) int {
	return subscribe(L, false)
}

// bus:once(topic, callback) → sub_id
func eventbusOnce(L *lua.State) int {
	return subscribe(L, true)
}

func subscribe(L *lua.State, once bool) int {
	bus := getBus(L)
	topic := L.CheckString(2)

	if !L.IsFunction(3) {
		L.ArgError(3, "function expected")
		return 0
	}

	// Store callback as registry ref
	L.PushValue(3)
	ref := L.Ref(lua.RegistryIndex)

	bus.mu.Lock()
	id := bus.nextID
	bus.nextID++
	bus.subs = append(bus.subs, subscription{
		id:    id,
		topic: topic,
		ref:   ref,
		once:  once,
	})
	bus.mu.Unlock()

	L.PushInteger(id)
	return 1
}

// bus:off(sub_id)
func eventbusOff(L *lua.State) int {
	bus := getBus(L)
	id := L.CheckInteger(2)

	bus.mu.Lock()
	for i, sub := range bus.subs {
		if sub.id == id {
			L.Unref(lua.RegistryIndex, sub.ref)
			bus.subs = append(bus.subs[:i], bus.subs[i+1:]...)
			break
		}
	}
	bus.mu.Unlock()
	return 0
}

// bus:emit(topic, data)
func eventbusEmit(L *lua.State) int {
	bus := getBus(L)
	topic := L.CheckString(2)

	hasData := L.GetTop() >= 3 && L.IsTable(3)

	bus.mu.Lock()
	// Collect matching subs (copy to avoid holding lock during callbacks)
	var matches []subscription
	var toRemove []int64
	for _, sub := range bus.subs {
		if topicMatch(sub.topic, topic) {
			matches = append(matches, sub)
			if sub.once {
				toRemove = append(toRemove, sub.id)
			}
		}
	}
	// Remove once-subs from the list before calling handlers
	if len(toRemove) > 0 {
		remaining := bus.subs[:0]
		for _, sub := range bus.subs {
			remove := false
			for _, rid := range toRemove {
				if sub.id == rid {
					remove = true
					break
				}
			}
			if !remove {
				remaining = append(remaining, sub)
			}
		}
		bus.subs = remaining
	}
	bus.mu.Unlock()

	// Call each matching handler synchronously
	for _, sub := range matches {
		// Push the callback function from registry
		L.RawGetI(lua.RegistryIndex, int64(sub.ref))

		if hasData {
			L.PushValue(3) // push the event table
			// Inject _topic
			L.PushString(topic)
			L.SetField(-2, "_topic")
		} else {
			// Create minimal event table with just _topic
			L.NewTable()
			L.PushString(topic)
			L.SetField(-2, "_topic")
		}

		// Call handler(event) — ignore errors from individual handlers
		_ = L.SafeCall(1, 0)

		// Unref once-subs after calling
		if sub.once {
			L.Unref(lua.RegistryIndex, sub.ref)
		}
	}

	return 0
}

// bus:clear([topic_pattern])
func eventbusClear(L *lua.State) int {
	bus := getBus(L)

	pattern := ""
	if L.GetTop() >= 2 && !L.IsNil(2) {
		pattern = L.CheckString(2)
	}

	bus.mu.Lock()
	if pattern == "" {
		// Clear all
		for _, sub := range bus.subs {
			L.Unref(lua.RegistryIndex, sub.ref)
		}
		bus.subs = bus.subs[:0]
	} else {
		remaining := bus.subs[:0]
		for _, sub := range bus.subs {
			if topicMatch(pattern, sub.topic) {
				L.Unref(lua.RegistryIndex, sub.ref)
			} else {
				remaining = append(remaining, sub)
			}
		}
		bus.subs = remaining
	}
	bus.mu.Unlock()
	return 0
}

// bus:count([topic_pattern]) → integer
func eventbusCount(L *lua.State) int {
	bus := getBus(L)

	pattern := ""
	if L.GetTop() >= 2 && !L.IsNil(2) {
		pattern = L.CheckString(2)
	}

	bus.mu.Lock()
	count := int64(0)
	if pattern == "" {
		count = int64(len(bus.subs))
	} else {
		for _, sub := range bus.subs {
			if topicMatch(pattern, sub.topic) {
				count++
			}
		}
	}
	bus.mu.Unlock()

	L.PushInteger(count)
	return 1
}

// topicMatch checks if a pattern matches a topic.
// "*" matches exactly one segment. "**" matches zero or more segments.
// Segments are separated by ".".
func topicMatch(pattern, topic string) bool {
	if pattern == topic {
		return true
	}

	patParts := strings.Split(pattern, ".")
	topParts := strings.Split(topic, ".")

	return matchParts(patParts, topParts)
}

func matchParts(pattern, topic []string) bool {
	pi, ti := 0, 0
	for pi < len(pattern) && ti < len(topic) {
		if pattern[pi] == "**" {
			// ** matches zero or more segments
			if pi == len(pattern)-1 {
				return true // ** at end matches everything remaining
			}
			// Try matching rest of pattern starting at each topic position
			for k := ti; k <= len(topic); k++ {
				if matchParts(pattern[pi+1:], topic[k:]) {
					return true
				}
			}
			return false
		}
		if pattern[pi] == "*" {
			// * matches exactly one segment
			pi++
			ti++
			continue
		}
		if pattern[pi] != topic[ti] {
			return false
		}
		pi++
		ti++
	}

	// Handle trailing **
	for pi < len(pattern) && pattern[pi] == "**" {
		pi++
	}

	return pi == len(pattern) && ti == len(topic)
}
