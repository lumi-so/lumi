package luatest_test

import (
	"testing"

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
