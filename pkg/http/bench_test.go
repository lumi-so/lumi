package http

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	lua "github.com/akzj/go-lua/pkg/lua"
)

// benchServer returns a test server that responds with small JSON (~60 bytes).
func benchServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "bench-123")
		w.Write([]byte(`{"id":1,"name":"Alice","email":"alice@example.com","age":30}`))
	}))
}

// ============================================================
// SECTION 1: Call-Path Benchmarks (no network — pure overhead)
// ============================================================

// BenchmarkGoToGo_Call measures baseline Go function call cost.
func BenchmarkGoToGo_Call(b *testing.B) {
	type result struct {
		Echo string
		Len  int
	}
	fn := func(s string) result {
		return result{Echo: s, Len: len(s)}
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := fn("hello world")
		_ = r
	}
}

// BenchmarkGoToLua_Call measures Go calling a Lua function (SafeCall dispatch).
func BenchmarkGoToLua_Call(b *testing.B) {
	L := lua.NewState()
	defer L.Close()
	L.DoString(`function echo(x) return {result = x} end`)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		L.GetGlobal("echo")
		L.PushInteger(int64(i))
		if err := L.SafeCall(1, 1); err != nil {
			b.Fatal(err)
		}
		L.Pop(1)
	}
}

// BenchmarkLuaToGo_Call measures Lua calling a Go function (WrapSafe binding).
func BenchmarkLuaToGo_Call(b *testing.B) {
	L := lua.NewState()
	defer L.Close()

	// Register a minimal Go function
	L.PushFunction(lua.WrapSafe(func(L *lua.State) int {
		s := L.CheckString(1)
		L.PushAny(map[string]any{"echo": s, "len": len(s)})
		return 1
	}))
	L.SetGlobal("go_echo")

	L.DoString(`function bench() return go_echo("hello world") end`)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		L.GetGlobal("bench")
		if err := L.SafeCall(0, 1); err != nil {
			b.Fatal(err)
		}
		L.Pop(1)
	}
}

// BenchmarkLuaToLua_Call measures pure Lua→Lua function call (VM overhead only).
func BenchmarkLuaToLua_Call(b *testing.B) {
	L := lua.NewState()
	defer L.Close()

	L.DoString(`
		function inner(x) return {result = x, doubled = x * 2} end
		function bench() return inner(42) end
	`)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		L.GetGlobal("bench")
		if err := L.SafeCall(0, 1); err != nil {
			b.Fatal(err)
		}
		L.Pop(1)
	}
}

// ============================================================
// SECTION 2: HTTP Benchmarks (with network — real-world overhead)
// ============================================================

// BenchmarkPureGo_GET measures pure Go HTTP GET throughput.
func BenchmarkPureGo_GET(b *testing.B) {
	server := benchServer()
	defer server.Close()
	client := &http.Client{}
	url := server.URL + "/users/1"

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("GET", url, nil)
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		_ = body
	}
}

// BenchmarkLua_GET_DoString measures Lua HTTP GET including parse overhead.
func BenchmarkLua_GET_DoString(b *testing.B) {
	server := benchServer()
	defer server.Close()

	L := lua.NewState()
	defer L.Close()
	Register(L, Config{})

	if err := L.DoString(`local http = require("http")`); err != nil {
		b.Fatal(err)
	}

	code := fmt.Sprintf(`
		local http = require("http")
		local resp, err = http.get("%s/users/1")
		if err then error(err) end
	`, server.URL)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := L.DoString(code); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLua_GET_Precompiled measures Lua HTTP GET without parse overhead.
func BenchmarkLua_GET_Precompiled(b *testing.B) {
	server := benchServer()
	defer server.Close()

	L := lua.NewState()
	defer L.Close()
	Register(L, Config{})

	setupCode := fmt.Sprintf(`
		local http = require("http")
		function do_get()
			local resp, err = http.get("%s/users/1")
			if err then error(err) end
			return resp
		end
	`, server.URL)
	if err := L.DoString(setupCode); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		L.GetGlobal("do_get")
		if err := L.SafeCall(0, 1); err != nil {
			b.Fatal(err)
		}
		L.Pop(1)
	}
}

// BenchmarkPureGo_POST measures pure Go HTTP POST throughput.
func BenchmarkPureGo_POST(b *testing.B) {
	server := benchServer()
	defer server.Close()
	client := &http.Client{}
	url := server.URL + "/users"
	jsonBody := `{"name":"Bob","email":"bob@example.com","age":25}`

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("POST", url, strings.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		_ = body
	}
}

// BenchmarkLua_POST_DoString measures Lua HTTP POST including parse overhead.
func BenchmarkLua_POST_DoString(b *testing.B) {
	server := benchServer()
	defer server.Close()

	L := lua.NewState()
	defer L.Close()
	Register(L, Config{})

	code := fmt.Sprintf(`
		local http = require("http")
		local resp, err = http.post("%s/users", {
			headers = {["Content-Type"] = "application/json"},
			body = '{"name":"Bob","email":"bob@example.com","age":25}',
		})
		if err then error(err) end
	`, server.URL)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := L.DoString(code); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLua_POST_Precompiled measures Lua HTTP POST without parse overhead.
func BenchmarkLua_POST_Precompiled(b *testing.B) {
	server := benchServer()
	defer server.Close()

	L := lua.NewState()
	defer L.Close()
	Register(L, Config{})

	setupCode := fmt.Sprintf(`
		local http = require("http")
		function do_post()
			local resp, err = http.post("%s/users", {
				headers = {["Content-Type"] = "application/json"},
				body = '{"name":"Bob","email":"bob@example.com","age":25}',
			})
			if err then error(err) end
			return resp
		end
	`, server.URL)
	if err := L.DoString(setupCode); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		L.GetGlobal("do_post")
		if err := L.SafeCall(0, 1); err != nil {
			b.Fatal(err)
		}
		L.Pop(1)
	}
}

// ============================================================
// SECTION 3: Isolation Benchmarks (overhead components)
// ============================================================

// BenchmarkOverhead_PushAny_ResponseMap measures PushAny table construction cost.
// Uses SetTop(0) and periodic GC to prevent unbounded string table growth in tight loops.
func BenchmarkOverhead_PushAny_ResponseMap(b *testing.B) {
	L := lua.NewState()
	defer L.Close()

	resp := map[string]any{
		"status": 200,
		"body":   `{"id":1,"name":"Alice","email":"alice@example.com","age":30}`,
		"headers": map[string]any{
			"content-type": "application/json",
			"x-request-id": "bench-123",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		L.PushAny(resp)
		L.SetTop(0)
		if i%10000 == 0 {
			L.GCCollect()
		}
	}
}

// BenchmarkOverhead_LuaDoString measures Lua parse+exec cost (no I/O).
func BenchmarkOverhead_LuaDoString(b *testing.B) {
	L := lua.NewState()
	defer L.Close()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		L.DoString(`local x = 1 + 1`)
	}
}

// ============================================================
// SECTION 4: Memory Benchmarks
// ============================================================

// BenchmarkMemory_PureGo_GET measures memory per Go HTTP GET.
func BenchmarkMemory_PureGo_GET(b *testing.B) {
	server := benchServer()
	defer server.Close()
	client := &http.Client{}
	url := server.URL + "/users/1"

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	for i := 0; i < 1000; i++ {
		req, _ := http.NewRequest("GET", url, nil)
		resp, _ := client.Do(req)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		_ = body
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	b.ReportMetric(float64(m2.TotalAlloc-m1.TotalAlloc)/1000, "bytes/op")
	b.ReportMetric(float64(m2.Mallocs-m1.Mallocs)/1000, "allocs/op")
}

// BenchmarkMemory_Lua_GET measures memory per Lua HTTP GET (precompiled).
func BenchmarkMemory_Lua_GET(b *testing.B) {
	server := benchServer()
	defer server.Close()

	L := lua.NewState()
	defer L.Close()
	Register(L, Config{})

	setupCode := fmt.Sprintf(`
		local http = require("http")
		function do_get()
			local resp, err = http.get("%s/users/1")
			if err then error(err) end
			return resp
		end
	`, server.URL)
	L.DoString(setupCode)

	// Warm up
	for i := 0; i < 100; i++ {
		L.GetGlobal("do_get")
		L.SafeCall(0, 1)
		L.Pop(1)
	}

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	for i := 0; i < 1000; i++ {
		L.GetGlobal("do_get")
		L.SafeCall(0, 1)
		L.Pop(1)
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	b.ReportMetric(float64(m2.TotalAlloc-m1.TotalAlloc)/1000, "bytes/op")
	b.ReportMetric(float64(m2.Mallocs-m1.Mallocs)/1000, "allocs/op")
}
