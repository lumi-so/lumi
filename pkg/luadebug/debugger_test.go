package luadebug_test

import (
	"bytes"
	"strings"
	"testing"

	lua "github.com/akzj/go-lua/pkg/lua"
	"github.com/lumi-so/lumi/pkg/luadebug"
)

func TestDebuggerBreakpoint(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	var pausedAt int
	var pausedSource string

	dbg := luadebug.NewDebuggerWithOptions(
		luadebug.WithPauseHandler(func(L *lua.State, source string, line int) {
			pausedAt = line
			pausedSource = source
		}),
	)

	// Load code first to know the source name
	err := L.DoString(`
        function hello()
            local x = 1
            local y = 2
            return x + y
        end
    `)
	if err != nil {
		t.Fatal(err)
	}

	// Source is "(dostring)" in go-lua. Set breakpoint at line 4 (local y = 2)
	dbg.SetBreakpoint("(dostring)", 4)
	dbg.Attach(L)

	L.DoString(`hello()`)
	dbg.Detach(L)

	if pausedAt != 4 {
		t.Errorf("paused at line %d, want 4", pausedAt)
	}
	if pausedSource != "(dostring)" {
		t.Errorf("paused source = %q, want (dostring)", pausedSource)
	}
}

func TestDebuggerInteractive(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Simulate user input: "locals" then "continue"
	input := strings.NewReader("locals\ncontinue\n")
	output := &bytes.Buffer{}

	dbg := luadebug.NewDebuggerWithOptions(
		luadebug.WithInput(input),
		luadebug.WithOutput(output),
	)

	err := L.DoString(`
        function test_func()
            local name = "alice"
            local age = 30
            return name
        end
    `)
	if err != nil {
		t.Fatal(err)
	}

	dbg.SetBreakpoint("(dostring)", 4) // local age = 30
	dbg.Attach(L)

	L.DoString(`test_func()`)
	dbg.Detach(L)

	out := output.String()
	if !strings.Contains(out, "Paused at") {
		t.Errorf("expected 'Paused at' in output, got: %s", out)
	}
	// Should show locals
	t.Log(out)
}

func TestDebuggerStackTrace(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Test StackTrace via inspector
	err := L.DoString(`
        function inner()
            local x = 42
            return x
        end
        function outer()
            return inner()
        end
    `)
	if err != nil {
		t.Fatal(err)
	}

	var frames []luadebug.FrameInfo
	callDepth := 0

	L.SetHook(func(L *lua.State, event int, line int) {
		switch event {
		case lua.HookEventCall:
			callDepth++
		case lua.HookEventReturn:
			callDepth--
		case lua.HookEventLine:
			// Capture stack when we're inside inner() (depth >= 2)
			if frames == nil && callDepth >= 2 {
				frames = luadebug.StackTrace(L)
			}
		}
	}, lua.MaskCall|lua.MaskRet|lua.MaskLine, 0)

	L.DoString(`outer()`)
	L.SetHook(nil, 0, 0)

	if len(frames) < 2 {
		t.Fatalf("expected at least 2 frames, got %d", len(frames))
	}

	formatted := luadebug.FormatStackTrace(frames)
	if formatted == "" {
		t.Error("empty formatted stack trace")
	}
	t.Log(formatted)
}

func TestDebuggerStepOver(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	var pausedLines []int

	// First pause: breakpoint at line 3
	// Then send "next" to step over
	input := strings.NewReader("n\nc\n")
	output := &bytes.Buffer{}

	dbg := luadebug.NewDebuggerWithOptions(
		luadebug.WithInput(input),
		luadebug.WithOutput(output),
	)

	err := L.DoString(`
        function foo()
            local a = 1
            local b = 2
            local c = 3
            return a + b + c
        end
    `)
	if err != nil {
		t.Fatal(err)
	}

	dbg.SetBreakpoint("(dostring)", 3) // local a = 1
	dbg.Attach(L)

	L.DoString(`foo()`)
	dbg.Detach(L)

	out := output.String()
	// Should see two pauses: one at breakpoint, one after step
	count := strings.Count(out, "Paused at")
	if count < 2 {
		t.Errorf("expected at least 2 pauses, got %d. Output:\n%s", count, out)
	}
	_ = pausedLines
}
