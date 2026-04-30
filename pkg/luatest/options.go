package luatest

import lua "github.com/akzj/go-lua/pkg/lua"

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

// WithVerbose enables verbose output for the default TB reporter.
func WithVerbose() Option {
	return func(r *Runner) {
		r.verbose = true
	}
}

// WithReporter overrides the default TB reporter. Use MultiReporter to combine
// several reporters (for example TB + JUnit).
func WithReporter(rep Reporter) Option {
	return func(r *Runner) {
		r.reporter = rep
	}
}

// WithSubtests registers each Lua case as a Go subtest when tb is *testing.T.
func WithSubtests() Option {
	return func(r *Runner) {
		r.subtests = true
	}
}

// WithRecursive makes RunDir / RunDirContext walk subdirectories.
func WithRecursive(recursive bool) Option {
	return func(r *Runner) {
		r.recursive = recursive
	}
}

// WithFileMatcher keeps default filename rules (test_*.lua, *_test.lua) and
// additionally requires fn(fullPath) to be true. If fn is nil, the option is a no-op.
func WithFileMatcher(fn func(fullPath string) bool) Option {
	return func(r *Runner) {
		r.fileMatcher = fn
	}
}

// WithBeforeState runs immediately after a new Lua state is created (before package.path).
func WithBeforeState(fn func(L *lua.State)) Option {
	return func(r *Runner) {
		r.beforeState = fn
	}
}

// WithAfterState runs in a defer immediately before the state is closed.
func WithAfterState(fn func(L *lua.State)) Option {
	return func(r *Runner) {
		r.afterState = fn
	}
}

// WithBeforeFile runs after setup and package.path, immediately before DoFile / DoString.
func WithBeforeFile(fn func(path string, L *lua.State)) Option {
	return func(r *Runner) {
		r.beforeFile = fn
	}
}

// WithAfterFile runs after tests execute and before reporting. summary is nil if loading failed.
func WithAfterFile(fn func(path string, L *lua.State, summary *RunSummary)) Option {
	return func(r *Runner) {
		r.afterFile = fn
	}
}
