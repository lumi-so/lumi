package luatest

import (
	"context"
	"fmt"
	"io/fs"
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
	libDir  string
	setup   func(L *lua.State)
	verbose bool

	reporter    Reporter
	subtests    bool
	recursive   bool
	fileMatcher func(fullPath string) bool

	beforeState func(L *lua.State)
	afterState  func(L *lua.State)
	beforeFile  func(path string, L *lua.State)
	afterFile   func(path string, L *lua.State, summary *RunSummary)
}

// New creates a test runner bound to a Go test.
func New(t testing.TB, opts ...Option) *Runner {
	r := &Runner{
		t: t,
	}
	for _, opt := range opts {
		opt(r)
	}
	if r.libDir == "" {
		r.libDir = findLibDir()
	}
	return r
}

// RunFile executes a single Lua test file.
func (r *Runner) RunFile(path string) *RunSummary {
	return r.runLuaSource(path, nil, func(L *lua.State) error {
		return L.DoFile(path)
	})
}

// RunFileContext is like RunFile but respects ctx cancellation before work starts
// and before loading the file. A running DoFile cannot be interrupted.
func (r *Runner) RunFileContext(ctx context.Context, path string) *RunSummary {
	return r.runLuaSource(path, ctx, func(L *lua.State) error {
		return L.DoFile(path)
	})
}

// RunString executes Lua test code from a string (useful for inline tests).
func (r *Runner) RunString(code string) *RunSummary {
	return r.runLuaSource("<string>", nil, func(L *lua.State) error {
		return L.DoString(code)
	})
}

// RunStringContext is like RunString with the same cancellation semantics as RunFileContext.
func (r *Runner) RunStringContext(ctx context.Context, code string) *RunSummary {
	return r.runLuaSource("<string>", ctx, func(L *lua.State) error {
		return L.DoString(code)
	})
}

// RunDir discovers and runs all Lua test files in a directory.
// Files matching test_*.lua or *_test.lua are considered test files.
// Use WithRecursive(true) to include nested directories.
func (r *Runner) RunDir(dir string) *RunSummary {
	return r.runDir(dir, nil)
}

// RunDirContext runs each discovered file with RunFileContext.
func (r *Runner) RunDirContext(ctx context.Context, dir string) *RunSummary {
	return r.runDir(dir, ctx)
}

func (r *Runner) runDir(dir string, ctx context.Context) *RunSummary {
	r.t.Helper()

	files := r.listTestFiles(dir)
	if len(files) == 0 {
		r.t.Logf("no test files found in %s", dir)
		return &RunSummary{}
	}

	combined := &RunSummary{}
	for _, f := range files {
		if err := ctxErr(ctx); err != nil {
			r.t.Fatalf("luatest: %v", err)
			return combined
		}
		var summary *RunSummary
		if ctx != nil {
			summary = r.RunFileContext(ctx, f)
		} else {
			summary = r.RunFile(f)
		}
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

func (r *Runner) runLuaSource(path string, ctx context.Context, load func(*lua.State) error) *RunSummary {
	r.t.Helper()

	if err := ctxErr(ctx); err != nil {
		r.t.Fatalf("luatest: %v", err)
		return nil
	}

	L := lua.NewState()
	defer func() {
		if r.afterState != nil {
			r.afterState(L)
		}
		L.Close()
	}()

	if r.beforeState != nil {
		r.beforeState(L)
	}

	r.setupPackagePath(L)
	if r.setup != nil {
		r.setup(L)
	}

	if err := ctxErr(ctx); err != nil {
		r.t.Fatalf("luatest: %v", err)
		return nil
	}

	if r.beforeFile != nil {
		r.beforeFile(path, L)
	}

	if err := load(L); err != nil {
		if r.afterFile != nil {
			r.afterFile(path, L, nil)
		}
		r.t.Fatalf("failed to load %s: %v", path, err)
		return nil
	}

	summary := r.executeTests(L)
	if r.afterFile != nil {
		r.afterFile(path, L, summary)
	}

	r.emitReport(path, summary)
	return summary
}

func (r *Runner) emitReport(source string, summary *RunSummary) {
	rep := r.reporter
	if rep == nil {
		rep = TBReporter{}
	}
	rep.Report(r.t, source, summary, ReportOptions{
		Verbose:  r.verbose,
		Subtests: r.subtests,
	})
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (r *Runner) setupPackagePath(L *lua.State) {
	if r.libDir == "" {
		return
	}
	code := fmt.Sprintf(`package.path = %q .. "/?.lua;" .. %q .. "/?/init.lua;" .. package.path`, r.libDir, r.libDir)
	L.DoString(code)

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
	err := L.DoString(`return require("lumi.test").run()`)
	if err != nil {
		r.t.Fatalf("test.run() failed: %v", err)
		return nil
	}
	return r.parseResults(L)
}

// parseResults reads the table left on the stack by require("lumi.test").run().
// Contract: see package doc (RunSummary / Lua keys).
func (r *Runner) parseResults(L *lua.State) *RunSummary {
	if !L.IsTable(-1) {
		r.t.Fatal("test.run() did not return a table")
		return nil
	}

	summary := &RunSummary{}
	summary.Passed = int(L.GetFieldInt(-1, "passed"))
	summary.Failed = int(L.GetFieldInt(-1, "failed"))
	summary.Errored = int(L.GetFieldInt(-1, "errored"))
	summary.Total = int(L.GetFieldInt(-1, "total"))

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
				dur := L.GetFieldNumber(-1, "duration")
				result.Duration = time.Duration(dur * float64(time.Second))
				summary.Results = append(summary.Results, result)
			}
			L.Pop(1)
		}
	}
	L.Pop(1)
	L.Pop(1)

	return summary
}

func (r *Runner) listTestFiles(dir string) []string {
	matchName := func(name string) bool {
		return (strings.HasPrefix(name, "test_") && strings.HasSuffix(name, ".lua")) ||
			strings.HasSuffix(name, "_test.lua")
	}

	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			cleanPath := filepath.Clean(path)
			cleanDir := filepath.Clean(dir)
			if cleanPath == cleanDir {
				return nil
			}
			if !r.recursive {
				return filepath.SkipDir
			}
			return nil
		}
		if !matchName(d.Name()) {
			return nil
		}
		if r.fileMatcher != nil && !r.fileMatcher(path) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil
	}
	return files
}

func findLibDir() string {
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
