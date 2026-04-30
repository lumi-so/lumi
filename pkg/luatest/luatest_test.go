package luatest_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lua "github.com/akzj/go-lua/pkg/lua"

	"github.com/lumi-so/lumi/pkg/luatest"
)

// mockTB implements testing.TB for capturing failures without failing the real test.
type mockTB struct {
	testing.TB
	failed bool
	logs   []string
}

func (m *mockTB) Helper()                       {}
func (m *mockTB) Log(args ...any)               {}
func (m *mockTB) Logf(format string, args ...any) {}
func (m *mockTB) Error(args ...any)             { m.failed = true }
func (m *mockTB) Errorf(format string, args ...any) { m.failed = true }
func (m *mockTB) Fatal(args ...any)             { m.failed = true; panic("fatal") }
func (m *mockTB) Fatalf(format string, args ...any) { m.failed = true; panic("fatal") }

func TestRunStringBasic(t *testing.T) {
	runner := luatest.New(t, luatest.WithLibDir("../../lib"), luatest.WithVerbose())
	summary := runner.RunString(`
		local test = require("lumi.test")

		test.describe("Math", function()
			test.it("adds numbers", function()
				test.assert_eq(1 + 1, 2)
			end)

			test.it("multiplies", function()
				test.assert_eq(3 * 4, 12)
			end)
		end)
	`)

	if summary.Total != 2 {
		t.Fatalf("total = %d, want 2", summary.Total)
	}
	if summary.Passed != 2 {
		t.Fatalf("passed = %d, want 2", summary.Passed)
	}
}

func TestRunStringFailure(t *testing.T) {
	// Use a mock TB so the Lua failure doesn't mark this Go test as failed.
	mock := &mockTB{}
	runner := luatest.New(mock, luatest.WithLibDir("../../lib"))
	summary := runner.RunString(`
		local test = require("lumi.test")

		test.describe("Failing", function()
			test.it("passes", function()
				test.assert(true)
			end)

			test.it("fails", function()
				test.assert_eq(1, 2, "math is broken")
			end)
		end)
	`)

	if summary.Passed != 1 {
		t.Fatalf("passed = %d, want 1", summary.Passed)
	}
	if summary.Failed != 1 {
		t.Fatalf("failed = %d, want 1", summary.Failed)
	}
	if !mock.failed {
		t.Fatal("expected mock TB to be marked as failed")
	}
	// Check error message
	if summary.Results[1].Error == "" {
		t.Fatal("expected error message for failed test")
	}
}

func TestRunStringAssertions(t *testing.T) {
	runner := luatest.New(t, luatest.WithLibDir("../../lib"), luatest.WithVerbose())
	summary := runner.RunString(`
		local test = require("lumi.test")

		test.describe("Assertions", function()
			test.it("assert_ne", function()
				test.assert_ne(1, 2)
			end)

			test.it("assert_nil", function()
				test.assert_nil(nil)
			end)

			test.it("assert_not_nil", function()
				test.assert_not_nil("hello")
			end)

			test.it("assert_type", function()
				test.assert_type("hello", "string")
				test.assert_type(42, "number")
				test.assert_type({}, "table")
			end)

			test.it("assert_gt", function()
				test.assert_gt(5, 3)
			end)

			test.it("assert_lt", function()
				test.assert_lt(3, 5)
			end)

			test.it("assert_contains", function()
				test.assert_contains("hello world", "world")
			end)

			test.it("assert_error", function()
				test.assert_error(function()
					error("boom")
				end, "boom")
			end)
		end)
	`)

	if summary.Failed != 0 {
		t.Fatalf("failed = %d, want 0; results: %+v", summary.Failed, summary.Results)
	}
}

func TestBeforeAfterEach(t *testing.T) {
	runner := luatest.New(t, luatest.WithLibDir("../../lib"), luatest.WithVerbose())
	summary := runner.RunString(`
		local test = require("lumi.test")

		test.describe("Hooks", function()
			local count = 0

			test.before_each(function()
				count = count + 1
			end)

			test.it("first", function()
				test.assert_eq(count, 1)
			end)

			test.it("second", function()
				test.assert_eq(count, 2)
			end)

			test.it("third", function()
				test.assert_eq(count, 3)
			end)
		end)
	`)

	if summary.Passed != 3 {
		t.Fatalf("passed = %d, want 3", summary.Passed)
	}
}

func TestRunDir(t *testing.T) {
	runner := luatest.New(t, luatest.WithLibDir("../../lib"), luatest.WithVerbose())
	summary := runner.RunDir("../../testdata/")
	if summary == nil {
		t.Fatal("RunDir returned nil")
	}
	if summary.Total == 0 {
		t.Fatal("expected at least one test in testdata/")
	}
	if summary.Failed != 0 {
		t.Fatalf("testdata tests failed: %d failures", summary.Failed)
	}
}

func TestHooks(t *testing.T) {
	var seq []string
	runner := luatest.New(t, luatest.WithLibDir("../../lib"),
		luatest.WithBeforeState(func(L *lua.State) { seq = append(seq, "beforeState") }),
		luatest.WithAfterState(func(L *lua.State) { seq = append(seq, "afterState") }),
		luatest.WithBeforeFile(func(path string, L *lua.State) { seq = append(seq, "beforeFile:"+path) }),
		luatest.WithAfterFile(func(path string, L *lua.State, sum *luatest.RunSummary) {
			if sum == nil {
				t.Fatal("expected summary")
			}
			seq = append(seq, "afterFile")
		}),
	)
	runner.RunString(`local test = require("lumi.test")
		test.describe("H", function() test.it("x", function() test.assert(true) end) end)`)

	want := []string{"beforeState", "beforeFile:<string>", "afterFile", "afterState"}
	if len(seq) != len(want) {
		t.Fatalf("seq = %v want %v", seq, want)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("seq[%d] = %q want %q (full %v)", i, seq[i], want[i], seq)
		}
	}
}

func TestMergeSummaries(t *testing.T) {
	a := &luatest.RunSummary{Passed: 1, Total: 1, Results: []luatest.TestResult{{Suite: "s", Name: "n", Passed: true}}}
	b := &luatest.RunSummary{Failed: 1, Total: 1, Results: []luatest.TestResult{{Suite: "s", Name: "f", Passed: false, Error: "e"}}}
	m := luatest.MergeSummaries(a, b)
	if m.Passed != 1 || m.Failed != 1 || m.Total != 2 || len(m.Results) != 2 {
		t.Fatalf("%+v", m)
	}
}

func TestRunDirRecursive(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	luaFile := filepath.Join(sub, "test_nested.lua")
	code := `local test = require("lumi.test")
test.describe("N", function() test.it("one", function() test.assert_eq(1,1) end) end)`
	if err := os.WriteFile(luaFile, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := luatest.New(t, luatest.WithLibDir("../../lib"), luatest.WithRecursive(true))
	sum := runner.RunDir(root)
	if sum.Total != 1 || sum.Passed != 1 {
		t.Fatalf("got %+v", sum)
	}

	empty := t.TempDir()
	flat := luatest.New(t, luatest.WithLibDir("../../lib"))
	if s := flat.RunDir(empty); s.Total != 0 {
		t.Fatalf("empty dir should find 0, got %+v", s)
	}
}

func TestFileMatcher(t *testing.T) {
	root := t.TempDir()
	p1 := filepath.Join(root, "test_keep.lua")
	p2 := filepath.Join(root, "test_skip.lua")
	for _, p := range []string{p1, p2} {
		body := `local test = require("lumi.test")
test.describe("X", function() test.it("k", function() test.assert(true) end) end)`
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runner := luatest.New(t, luatest.WithLibDir("../../lib"),
		luatest.WithFileMatcher(func(full string) bool {
			return strings.HasSuffix(full, "test_keep.lua")
		}),
	)
	sum := runner.RunDir(root)
	if sum.Total != 1 {
		t.Fatalf("want 1 test, got %+v", sum)
	}
}

func TestJUnitReporter(t *testing.T) {
	mock := &mockTB{}
	jr := luatest.NewJUnitReporter()
	runner := luatest.New(mock, luatest.WithLibDir("../../lib"),
		luatest.WithReporter(jr),
	)
	runner.RunString(`local test = require("lumi.test")
test.describe("J", function()
  test.it("ok", function() test.assert(true) end)
  test.it("bad", function() test.assert_eq(1, 2) end)
end)`)

	var buf bytes.Buffer
	if err := jr.WriteJUnit(&buf); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, "<testsuites>") || !strings.Contains(s, `failures="1"`) {
		t.Fatalf("unexpected xml: %s", s)
	}
}

func TestRunDirContextCancelled(t *testing.T) {
	mock := &mockTB{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := luatest.New(mock, luatest.WithLibDir("../../lib"))
	func() {
		defer func() { recover() }()
		runner.RunDirContext(ctx, "../../testdata/")
	}()
	if !mock.failed {
		t.Fatal("expected cancelled context to fail the run")
	}
}

func TestSubtestsWithRealT(t *testing.T) {
	t.Run("inner", func(t *testing.T) {
		runner := luatest.New(t, luatest.WithLibDir("../../lib"), luatest.WithSubtests())
		runner.RunString(`local test = require("lumi.test")
test.describe("S", function()
  test.it("a", function() test.assert(true) end)
  test.it("b", function() test.assert_eq(2, 2) end)
end)`)
	})
}
