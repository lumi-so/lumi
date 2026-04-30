package luatest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	lua "github.com/akzj/go-lua/pkg/lua"
)

// TestResult represents the result of a single test case.
type TestResult struct {
	Suite    string
	Name     string
	Passed   bool
	Error    string
	Duration time.Duration
}

// RunSummary contains the summary of a test run.
type RunSummary struct {
	Results []TestResult
	Passed  int
	Failed  int
	Errored int
	Total   int
}

// Option configures a Runner.
type Option func(*Runner)

// Runner executes Lua test files.
type Runner struct {
	t       testing.TB
	libDir  string              // directory containing lib/test.lua
	setup   func(L *lua.State)  // custom setup (register modules, mocks)
	verbose bool
}

// New creates a test runner bound to a Go test.
func New(t testing.TB, opts ...Option) *Runner {
	r := &Runner{
		t: t,
	}
	for _, opt := range opts {
		opt(r)
	}
	// Auto-detect lib dir: look for lib/test.lua relative to module root
	if r.libDir == "" {
		r.libDir = findLibDir()
	}
	return r
}

// WithLibDir sets the directory containing Lua libraries (lib/test.lua etc).
func WithLibDir(dir string) Option {
	return func(r *Runner) {
		r.libDir = dir
	}
}

// WithSetup provides a function to customize the Lua state before tests run.
// Use this to register application modules, mocks, etc.
func WithSetup(fn func(L *lua.State)) Option {
	return func(r *Runner) {
		r.setup = fn
	}
}

// WithVerbose enables verbose output.
func WithVerbose() Option {
	return func(r *Runner) {
		r.verbose = true
	}
}

// RunFile executes a single Lua test file.
func (r *Runner) RunFile(path string) *RunSummary {
	r.t.Helper()

	L := lua.NewState()
	defer L.Close()

	// Set up package.path to find lib/test.lua
	r.setupPackagePath(L)

	// Custom setup (register modules, mocks)
	if r.setup != nil {
		r.setup(L)
	}

	// Load and execute the test file (this collects tests via describe/it)
	if err := L.DoFile(path); err != nil {
		r.t.Fatalf("failed to load test file %s: %v", path, err)
		return nil
	}

	// Call test.run() to execute collected tests
	summary := r.executeTests(L)

	// Report results to Go testing
	r.reportToGoTest(summary, path)

	return summary
}

// RunDir discovers and runs all Lua test files in a directory.
// Files matching test_*.lua or *_test.lua are considered test files.
func (r *Runner) RunDir(dir string) *RunSummary {
	r.t.Helper()

	files := discoverTestFiles(dir)
	if len(files) == 0 {
		r.t.Logf("no test files found in %s", dir)
		return &RunSummary{}
	}

	combined := &RunSummary{}
	for _, f := range files {
		summary := r.RunFile(f)
		if summary != nil {
			combined.Results = append(combined.Results, summary.Results...)
			combined.Passed += summary.Passed
			combined.Failed += summary.Failed
			combined.Errored += summary.Errored
			combined.Total += summary.Total
		}
	}
	return combined
}

// RunString executes Lua test code from a string (useful for inline tests).
func (r *Runner) RunString(code string) *RunSummary {
	r.t.Helper()

	L := lua.NewState()
	defer L.Close()

	r.setupPackagePath(L)
	if r.setup != nil {
		r.setup(L)
	}

	if err := L.DoString(code); err != nil {
		r.t.Fatalf("failed to execute test code: %v", err)
		return nil
	}

	summary := r.executeTests(L)
	r.reportToGoTest(summary, "<string>")
	return summary
}

func (r *Runner) setupPackagePath(L *lua.State) {
	if r.libDir == "" {
		return
	}
	// Add lib dir to package.path so require("lumi.test") works
	code := fmt.Sprintf(`package.path = %q .. "/?.lua;" .. %q .. "/?/init.lua;" .. package.path`, r.libDir, r.libDir)
	L.DoString(code)

	// Register lib/test.lua as both "lumi.test" and "test" via package.preload
	testLuaPath := filepath.Join(r.libDir, "test.lua")
	if _, err := os.Stat(testLuaPath); err == nil {
		preloadCode := fmt.Sprintf(`
			local f = loadfile(%q)
			if f then
				package.preload["lumi.test"] = f
				package.preload["test"] = f
			end
		`, testLuaPath)
		L.DoString(preloadCode)
	}
}

func (r *Runner) executeTests(L *lua.State) *RunSummary {
	// Call: local result = require("lumi.test").run()
	err := L.DoString(`return require("lumi.test").run()`)
	if err != nil {
		r.t.Fatalf("test.run() failed: %v", err)
		return nil
	}

	// Parse the returned table
	return r.parseResults(L)
}

func (r *Runner) parseResults(L *lua.State) *RunSummary {
	// The result table is at the top of the stack
	if !L.IsTable(-1) {
		r.t.Fatal("test.run() did not return a table")
		return nil
	}

	summary := &RunSummary{}
	summary.Passed = int(L.GetFieldInt(-1, "passed"))
	summary.Failed = int(L.GetFieldInt(-1, "failed"))
	summary.Errored = int(L.GetFieldInt(-1, "errored"))
	summary.Total = int(L.GetFieldInt(-1, "total"))

	// Parse results array
	L.GetField(-1, "results")
	if L.IsTable(-1) {
		n := int(L.RawLen(-1))
		for i := 1; i <= n; i++ {
			L.RawGetI(-1, int64(i))
			if L.IsTable(-1) {
				result := TestResult{
					Suite:  L.GetFieldString(-1, "suite"),
					Name:   L.GetFieldString(-1, "name"),
					Passed: L.GetFieldBool(-1, "passed"),
					Error:  L.GetFieldString(-1, "error"),
				}
				// duration is in seconds (os.clock())
				dur := L.GetFieldNumber(-1, "duration")
				result.Duration = time.Duration(dur * float64(time.Second))
				summary.Results = append(summary.Results, result)
			}
			L.Pop(1)
		}
	}
	L.Pop(1) // pop results table
	L.Pop(1) // pop main result table

	return summary
}

func (r *Runner) reportToGoTest(summary *RunSummary, source string) {
	if summary == nil {
		return
	}

	for _, result := range summary.Results {
		testName := fmt.Sprintf("%s/%s", result.Suite, result.Name)
		if result.Passed {
			if r.verbose {
				r.t.Logf("  ✓ %s (%.1fms)", testName, float64(result.Duration)/float64(time.Millisecond))
			}
		} else {
			r.t.Errorf("  ✗ %s\n    %s", testName, result.Error)
		}
	}

	if summary.Failed > 0 || summary.Errored > 0 {
		r.t.Errorf("%s: %d passed, %d failed, %d errors (total %d)",
			source, summary.Passed, summary.Failed, summary.Errored, summary.Total)
	} else if r.verbose {
		r.t.Logf("%s: all %d tests passed", source, summary.Total)
	}
}

// discoverTestFiles finds test files in a directory (non-recursive).
func discoverTestFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "test_") && strings.HasSuffix(name, ".lua") {
			files = append(files, filepath.Join(dir, name))
		} else if strings.HasSuffix(name, "_test.lua") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	return files
}

// findLibDir tries to locate the lib/ directory relative to the working directory.
func findLibDir() string {
	// Walk up from cwd looking for lib/test.lua
	dir, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		candidate := filepath.Join(dir, "lib")
		if _, err := os.Stat(filepath.Join(candidate, "test.lua")); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
