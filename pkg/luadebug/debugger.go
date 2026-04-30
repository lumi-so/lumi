package luadebug

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	lua "github.com/akzj/go-lua/pkg/lua"
)

// DebugState represents the debugger's current state.
type DebugState int

const (
	StateRunning  DebugState = iota // executing normally
	StatePaused                     // stopped at breakpoint
	StateStepping                   // step-over mode
	StateStepIn                     // step-into mode
)

// Debugger provides interactive breakpoint debugging for Lua code.
//
// Example:
//
//	dbg := luadebug.NewDebugger()
//	dbg.SetBreakpoint("main.lua", 10)
//	dbg.Attach(L)
//	L.DoFile("main.lua")  // will pause at line 10
type Debugger struct {
	breakpoints map[string]map[int]bool // source → line → enabled
	state       DebugState
	stepDepth   int // call depth for step-over
	input       io.Reader
	output      io.Writer
	onPause     func(L *lua.State, source string, line int) // callback when paused
}

// NewDebugger creates a new interactive debugger.
func NewDebugger() *Debugger {
	return &Debugger{
		breakpoints: make(map[string]map[int]bool),
		state:       StateRunning,
		input:       os.Stdin,
		output:      os.Stdout,
	}
}

// DebuggerOption configures a Debugger.
type DebuggerOption func(*Debugger)

// WithInput sets the input reader for debugger commands (default: stdin).
func WithInput(r io.Reader) DebuggerOption {
	return func(d *Debugger) {
		d.input = r
	}
}

// WithOutput sets the output writer for debugger output (default: stdout).
func WithOutput(w io.Writer) DebuggerOption {
	return func(d *Debugger) {
		d.output = w
	}
}

// WithPauseHandler sets a callback invoked when the debugger pauses.
// Useful for non-interactive (programmatic) debugging.
// When set, the interactive REPL is skipped — the handler controls
// whether to continue (just return) or inspect state.
func WithPauseHandler(fn func(L *lua.State, source string, line int)) DebuggerOption {
	return func(d *Debugger) {
		d.onPause = fn
	}
}

// NewDebuggerWithOptions creates a debugger with options.
func NewDebuggerWithOptions(opts ...DebuggerOption) *Debugger {
	d := NewDebugger()
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// SetBreakpoint adds a breakpoint at the given source file and line.
func (d *Debugger) SetBreakpoint(source string, line int) {
	if d.breakpoints[source] == nil {
		d.breakpoints[source] = make(map[int]bool)
	}
	d.breakpoints[source][line] = true
}

// RemoveBreakpoint removes a breakpoint.
func (d *Debugger) RemoveBreakpoint(source string, line int) {
	if d.breakpoints[source] != nil {
		delete(d.breakpoints[source], line)
	}
}

// ClearBreakpoints removes all breakpoints.
func (d *Debugger) ClearBreakpoints() {
	d.breakpoints = make(map[string]map[int]bool)
}

// Attach installs debug hooks on the Lua state.
func (d *Debugger) Attach(L *lua.State) {
	depth := 0

	L.SetHook(func(L *lua.State, event int, currentLine int) {
		switch event {
		case lua.HookEventCall:
			depth++
		case lua.HookEventReturn:
			depth--
			if depth < 0 {
				depth = 0
			}
		case lua.HookEventLine:
			// Check if we should pause
			shouldPause := false

			switch d.state {
			case StateRunning:
				// Check breakpoints
				ar, ok := L.GetStack(0)
				if ok {
					L.GetInfo("S", ar)
					src := ar.ShortSrc
					if d.breakpoints[src] != nil && d.breakpoints[src][currentLine] {
						shouldPause = true
					}
				}
			case StateStepping:
				if depth <= d.stepDepth {
					shouldPause = true
				}
			case StateStepIn:
				shouldPause = true
			}

			if shouldPause {
				d.pause(L, currentLine, depth)
			}
		}
	}, lua.MaskCall|lua.MaskRet|lua.MaskLine, 0)
}

// Detach removes debug hooks.
func (d *Debugger) Detach(L *lua.State) {
	L.SetHook(nil, 0, 0)
}

func (d *Debugger) pause(L *lua.State, line int, depth int) {
	ar, ok := L.GetStack(0)
	if !ok {
		return
	}
	L.GetInfo("nSl", ar)

	source := ar.ShortSrc
	funcName := ar.Name
	if funcName == "" {
		funcName = "<anonymous>"
	}

	fmt.Fprintf(d.output, "\n⏸ Paused at %s:%d in %s\n", source, line, funcName)

	// If there's a pause handler (programmatic mode), call it and return
	if d.onPause != nil {
		d.onPause(L, source, line)
		return
	}

	// Interactive REPL
	scanner := bufio.NewScanner(d.input)
	for {
		fmt.Fprint(d.output, "(debug) ")
		if !scanner.Scan() {
			// EOF — continue execution
			d.state = StateRunning
			return
		}

		cmd := strings.TrimSpace(scanner.Text())
		if cmd == "" {
			continue
		}

		parts := strings.Fields(cmd)
		switch parts[0] {
		case "c", "continue":
			d.state = StateRunning
			return
		case "n", "next", "step-over":
			d.state = StateStepping
			d.stepDepth = depth
			return
		case "s", "step", "step-in":
			d.state = StateStepIn
			return
		case "bt", "backtrace", "where":
			frames := StackTrace(L)
			fmt.Fprint(d.output, FormatStackTrace(frames))
		case "locals", "l":
			d.printLocals(L)
		case "p", "print":
			if len(parts) > 1 {
				expr := strings.Join(parts[1:], " ")
				d.evalPrint(L, expr)
			} else {
				fmt.Fprintln(d.output, "Usage: p <expression>")
			}
		case "b", "break":
			if len(parts) >= 2 {
				d.parseBreakpoint(parts[1])
			} else {
				d.listBreakpoints()
			}
		case "q", "quit":
			d.state = StateRunning
			d.ClearBreakpoints()
			return
		case "h", "help":
			d.printHelp()
		default:
			fmt.Fprintf(d.output, "Unknown command: %s (type 'h' for help)\n", parts[0])
		}
	}
}

func (d *Debugger) printLocals(L *lua.State) {
	ar, ok := L.GetStack(0)
	if !ok {
		return
	}

	for i := 1; ; i++ {
		name := L.GetLocal(ar, i)
		if name == "" {
			break
		}
		if strings.HasPrefix(name, "(") {
			L.Pop(1)
			continue
		}
		val := luaValueToString(L, -1)
		typ := L.TypeName(L.Type(-1))
		L.Pop(1)
		fmt.Fprintf(d.output, "  %s = %s (%s)\n", name, val, typ)
	}
}

func (d *Debugger) evalPrint(L *lua.State, expr string) {
	// Try to evaluate as expression
	code := "return " + expr
	err := L.DoString(code)
	if err != nil {
		fmt.Fprintf(d.output, "Error: %v\n", err)
		return
	}
	if L.GetTop() > 0 {
		val := luaValueToString(L, -1)
		fmt.Fprintf(d.output, "= %s\n", val)
		L.Pop(1)
	}
}

func (d *Debugger) parseBreakpoint(spec string) {
	// Format: "file:line" or just "line" (current file)
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) == 2 {
		line, err := strconv.Atoi(parts[1])
		if err != nil {
			fmt.Fprintf(d.output, "Invalid line number: %s\n", parts[1])
			return
		}
		d.SetBreakpoint(parts[0], line)
		fmt.Fprintf(d.output, "Breakpoint set at %s:%d\n", parts[0], line)
	} else {
		line, err := strconv.Atoi(parts[0])
		if err != nil {
			fmt.Fprintf(d.output, "Usage: b [file:]line\n")
			return
		}
		fmt.Fprintf(d.output, "Breakpoint set at line %d (all files)\n", line)
		d.SetBreakpoint("", line) // empty source = match any
	}
}

func (d *Debugger) listBreakpoints() {
	if len(d.breakpoints) == 0 {
		fmt.Fprintln(d.output, "No breakpoints set")
		return
	}
	fmt.Fprintln(d.output, "Breakpoints:")
	for src, lines := range d.breakpoints {
		for line := range lines {
			if src == "" {
				fmt.Fprintf(d.output, "  *:%d\n", line)
			} else {
				fmt.Fprintf(d.output, "  %s:%d\n", src, line)
			}
		}
	}
}

func (d *Debugger) printHelp() {
	help := `Commands:
  c, continue    Resume execution
  n, next        Step over (execute current line, skip into calls)
  s, step        Step into (stop at next line, enter calls)
  bt, backtrace  Show call stack
  l, locals      Show local variables
  p <expr>       Evaluate and print expression
  b [file:]line  Set breakpoint
  b              List breakpoints
  q, quit        Stop debugging, continue to end
  h, help        Show this help
`
	fmt.Fprint(d.output, help)
}
