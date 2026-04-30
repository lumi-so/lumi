// Package luatest runs Lua tests from Go's testing package.
//
// # Lua result contract
//
// The runner executes require("lumi.test").run() and expects a table with:
//   - passed, failed, errored, total — integer counts
//   - results — array of tables: suite, name, passed (bool), error (string, if failed),
//     duration (number, seconds from os.clock())
//
// Additive fields may be added later; Go parsing ignores unknown keys.
//
// # Cancellation and timeouts
//
// RunFileContext checks context cancellation only before starting work and
// before loading the test file. A running Lua VM cannot be safely interrupted
// from another goroutine; use cooperative timeouts inside Lua if needed.
//
// # Debugger (pkg/luadebug)
//
// Attach a debugger inside WithSetup, before DoFile:
//
//	dbg := luadebug.NewDebugger()
//	dbg.SetBreakpoint("script.lua", 10)
//	luatest.New(t, luatest.WithSetup(func(L *lua.State) {
//	    dbg.Attach(L)
//	}))
package luatest
