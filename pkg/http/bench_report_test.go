package http

import (
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	lua "github.com/akzj/go-lua/pkg/lua"
)

func TestBenchReport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping bench report in short mode")
	}

	server := benchServer()
	defer server.Close()

	iterations := 5000

	// ============================================================
	// PART 1: Call-Path Overhead (no network)
	// ============================================================

	callPathIters := 100000

	// --- Go→Go (baseline) ---
	type echoResult struct {
		Echo string
		Len  int
	}
	goFn := func(s string) echoResult {
		return echoResult{Echo: s, Len: len(s)}
	}

	start := time.Now()
	for i := 0; i < callPathIters; i++ {
		r := goFn("hello world")
		_ = r
	}
	goToGoDuration := time.Since(start)

	// --- Go→Lua ---
	L := lua.NewState()
	Register(L, Config{})
	L.DoString(`function echo(x) return {result = x} end`)

	// Warm up
	for i := 0; i < 1000; i++ {
		L.GetGlobal("echo")
		L.PushInteger(int64(i))
		L.SafeCall(1, 1)
		L.Pop(1)
	}

	start = time.Now()
	for i := 0; i < callPathIters; i++ {
		L.GetGlobal("echo")
		L.PushInteger(int64(i))
		if err := L.SafeCall(1, 1); err != nil {
			t.Fatal(err)
		}
		L.Pop(1)
	}
	goToLuaDuration := time.Since(start)

	// --- Lua→Go ---
	L.PushFunction(lua.WrapSafe(func(L *lua.State) int {
		s := L.CheckString(1)
		L.PushAny(map[string]any{"echo": s, "len": len(s)})
		return 1
	}))
	L.SetGlobal("go_echo")
	L.DoString(`function bench_lua_to_go() return go_echo("hello world") end`)

	// Warm up
	for i := 0; i < 1000; i++ {
		L.GetGlobal("bench_lua_to_go")
		L.SafeCall(0, 1)
		L.Pop(1)
	}

	start = time.Now()
	for i := 0; i < callPathIters; i++ {
		L.GetGlobal("bench_lua_to_go")
		if err := L.SafeCall(0, 1); err != nil {
			t.Fatal(err)
		}
		L.Pop(1)
	}
	luaToGoDuration := time.Since(start)

	// --- Lua→Lua ---
	L.DoString(`
		function inner(x) return {result = x, doubled = x * 2} end
		function bench_lua_to_lua() return inner(42) end
	`)

	// Warm up
	for i := 0; i < 1000; i++ {
		L.GetGlobal("bench_lua_to_lua")
		L.SafeCall(0, 1)
		L.Pop(1)
	}

	start = time.Now()
	for i := 0; i < callPathIters; i++ {
		L.GetGlobal("bench_lua_to_lua")
		if err := L.SafeCall(0, 1); err != nil {
			t.Fatal(err)
		}
		L.Pop(1)
	}
	luaToLuaDuration := time.Since(start)

	L.Close()

	// ============================================================
	// PART 2: HTTP Benchmarks (with network)
	// ============================================================

	// --- Pure Go GET ---
	client := &http.Client{}
	url := server.URL + "/users/1"

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	start = time.Now()
	for i := 0; i < iterations; i++ {
		req, _ := http.NewRequest("GET", url, nil)
		resp, _ := client.Do(req)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		_ = body
	}
	goGetDuration := time.Since(start)

	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	goGetAlloc := m2.TotalAlloc - m1.TotalAlloc
	goGetMallocs := m2.Mallocs - m1.Mallocs

	// --- Lua GET (precompiled) ---
	L = lua.NewState()
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

	runtime.GC()
	var m3 runtime.MemStats
	runtime.ReadMemStats(&m3)

	start = time.Now()
	for i := 0; i < iterations; i++ {
		L.GetGlobal("do_get")
		if err := L.SafeCall(0, 1); err != nil {
			t.Fatal(err)
		}
		L.Pop(1)
	}
	luaGetDuration := time.Since(start)

	runtime.GC()
	var m4 runtime.MemStats
	runtime.ReadMemStats(&m4)
	luaGetAlloc := m4.TotalAlloc - m3.TotalAlloc
	luaGetMallocs := m4.Mallocs - m3.Mallocs

	// --- Pure Go POST ---
	jsonBody := `{"name":"Bob","email":"bob@example.com","age":25}`

	runtime.GC()
	var m5 runtime.MemStats
	runtime.ReadMemStats(&m5)

	start = time.Now()
	for i := 0; i < iterations; i++ {
		req, _ := http.NewRequest("POST", server.URL+"/users", strings.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := client.Do(req)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		_ = body
	}
	goPostDuration := time.Since(start)

	runtime.GC()
	var m6 runtime.MemStats
	runtime.ReadMemStats(&m6)
	goPostAlloc := m6.TotalAlloc - m5.TotalAlloc
	goPostMallocs := m6.Mallocs - m5.Mallocs

	// --- Lua POST (precompiled) ---
	setupCode2 := fmt.Sprintf(`
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
	L.DoString(setupCode2)

	// Warm up
	for i := 0; i < 100; i++ {
		L.GetGlobal("do_post")
		L.SafeCall(0, 1)
		L.Pop(1)
	}

	runtime.GC()
	var m7 runtime.MemStats
	runtime.ReadMemStats(&m7)

	start = time.Now()
	for i := 0; i < iterations; i++ {
		L.GetGlobal("do_post")
		if err := L.SafeCall(0, 1); err != nil {
			t.Fatal(err)
		}
		L.Pop(1)
	}
	luaPostDuration := time.Since(start)

	runtime.GC()
	var m8 runtime.MemStats
	runtime.ReadMemStats(&m8)
	luaPostAlloc := m8.TotalAlloc - m7.TotalAlloc
	luaPostMallocs := m8.Mallocs - m7.Mallocs

	L.Close()

	// ============================================================
	// Print Report
	// ============================================================

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("  HTTP Client Benchmark: Lua vs Pure Go")
	fmt.Printf("  HTTP iterations: %d | Call-path iterations: %d\n", iterations, callPathIters)
	fmt.Printf("  Response payload: ~60 bytes JSON\n")
	fmt.Println(strings.Repeat("=", 70))

	// Call-path section
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("  Call Path Overhead (no network)")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("  %-18s %12s %12s\n", "Path", "ns/op", "ops/sec")
	fmt.Printf("  %-18s %12d %12s\n", "Go→Go (baseline)",
		goToGoDuration.Nanoseconds()/int64(callPathIters),
		formatOpsPerSec(callPathIters, goToGoDuration))
	fmt.Printf("  %-18s %12d %12s\n", "Go→Lua",
		goToLuaDuration.Nanoseconds()/int64(callPathIters),
		formatOpsPerSec(callPathIters, goToLuaDuration))
	fmt.Printf("  %-18s %12d %12s\n", "Lua→Go",
		luaToGoDuration.Nanoseconds()/int64(callPathIters),
		formatOpsPerSec(callPathIters, luaToGoDuration))
	fmt.Printf("  %-18s %12d %12s\n", "Lua→Lua",
		luaToLuaDuration.Nanoseconds()/int64(callPathIters),
		formatOpsPerSec(callPathIters, luaToLuaDuration))
	fmt.Println(strings.Repeat("=", 70))

	// HTTP section
	fmt.Println("\n--- HTTP GET ---")
	fmt.Printf("  %-20s %12s %12s %12s\n", "", "Duration", "Alloc/op", "Mallocs/op")
	fmt.Printf("  %-20s %12s %10s %12d\n", "Pure Go",
		goGetDuration.Round(time.Millisecond),
		formatBytes(goGetAlloc/uint64(iterations)),
		goGetMallocs/uint64(iterations))
	fmt.Printf("  %-20s %12s %10s %12d\n", "Lua (precompiled)",
		luaGetDuration.Round(time.Millisecond),
		formatBytes(luaGetAlloc/uint64(iterations)),
		luaGetMallocs/uint64(iterations))

	getOverhead := float64(luaGetDuration) / float64(goGetDuration)
	getAllocOverhead := float64(luaGetAlloc) / float64(goGetAlloc)
	fmt.Printf("  %-20s %11.2fx %11.2fx\n", "Lua overhead", getOverhead, getAllocOverhead)

	fmt.Println("\n--- HTTP POST ---")
	fmt.Printf("  %-20s %12s %12s %12s\n", "", "Duration", "Alloc/op", "Mallocs/op")
	fmt.Printf("  %-20s %12s %10s %12d\n", "Pure Go",
		goPostDuration.Round(time.Millisecond),
		formatBytes(goPostAlloc/uint64(iterations)),
		goPostMallocs/uint64(iterations))
	fmt.Printf("  %-20s %12s %10s %12d\n", "Lua (precompiled)",
		luaPostDuration.Round(time.Millisecond),
		formatBytes(luaPostAlloc/uint64(iterations)),
		luaPostMallocs/uint64(iterations))

	postOverhead := float64(luaPostDuration) / float64(goPostDuration)
	postAllocOverhead := float64(luaPostAlloc) / float64(goPostAlloc)
	fmt.Printf("  %-20s %11.2fx %11.2fx\n", "Lua overhead", postOverhead, postAllocOverhead)

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Printf("  Summary: Lua adds ~%.0f%% latency, ~%.0f%% memory overhead (GET)\n",
		(getOverhead-1)*100, (getAllocOverhead-1)*100)
	fmt.Printf("  (Network I/O dominates; binding overhead is marginal)\n")
	fmt.Println(strings.Repeat("=", 70))
}

func formatBytes(b uint64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}
	return fmt.Sprintf("%.1f KB", float64(b)/1024)
}

func formatOpsPerSec(iters int, d time.Duration) string {
	ops := float64(iters) / d.Seconds()
	if ops >= 1_000_000 {
		return fmt.Sprintf("%.1fM", ops/1_000_000)
	}
	if ops >= 1_000 {
		return fmt.Sprintf("%.0fK", ops/1_000)
	}
	return fmt.Sprintf("%.0f", ops)
}
