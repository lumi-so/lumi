package luadebug

import (
	"fmt"
	"strings"

	lua "github.com/akzj/go-lua/pkg/lua"
)

// FrameInfo represents a single call stack frame.
type FrameInfo struct {
	Level      int
	Source     string
	Line       int
	Name       string
	NameWhat   string
	What       string // "Lua", "C", "main"
	Locals     []LocalVar
	IsTailCall bool
}

// LocalVar represents a local variable in a stack frame.
type LocalVar struct {
	Name  string
	Value string // string representation
	Type  string // Lua type name
}

// StackTrace returns the full call stack of the Lua state.
// Each frame includes source, line, function name, and locals.
//
// Example:
//
//	frames := luadebug.StackTrace(L)
//	for _, f := range frames {
//	    fmt.Printf("%s:%d in %s\n", f.Source, f.Line, f.Name)
//	}
func StackTrace(L *lua.State) []FrameInfo {
	var frames []FrameInfo

	for level := 0; ; level++ {
		ar, ok := L.GetStack(level)
		if !ok {
			break
		}
		L.GetInfo("nSltu", ar)

		frame := FrameInfo{
			Level:      level,
			Source:     ar.ShortSrc,
			Line:       ar.CurrentLine,
			Name:       ar.Name,
			NameWhat:   ar.NameWhat,
			What:       ar.What,
			IsTailCall: ar.IsTailCall,
		}

		if frame.Name == "" {
			frame.Name = "<anonymous>"
		}

		// Collect locals
		for i := 1; ; i++ {
			name := L.GetLocal(ar, i)
			if name == "" {
				break
			}
			// GetLocal pushes the value onto the stack
			local := LocalVar{
				Name:  name,
				Type:  L.TypeName(L.Type(-1)),
				Value: luaValueToString(L, -1),
			}
			L.Pop(1)

			// Skip internal variables (starting with '(')
			if strings.HasPrefix(name, "(") {
				continue
			}
			frame.Locals = append(frame.Locals, local)
		}

		frames = append(frames, frame)
	}

	return frames
}

// FormatStackTrace returns a formatted string of the call stack.
func FormatStackTrace(frames []FrameInfo) string {
	var sb strings.Builder
	sb.WriteString("Stack trace:\n")
	for _, f := range frames {
		prefix := "  "
		if f.IsTailCall {
			prefix = " >"
		}
		sb.WriteString(fmt.Sprintf("%s#%d %s:%d in %s (%s)\n",
			prefix, f.Level, f.Source, f.Line, f.Name, f.What))

		for _, local := range f.Locals {
			sb.WriteString(fmt.Sprintf("      %s = %s (%s)\n", local.Name, local.Value, local.Type))
		}
	}
	return sb.String()
}

// MemoryInfo returns current Lua memory usage information.
type MemoryInfo struct {
	UsedBytes int64
	GCMode    string
}

// GetMemoryInfo returns memory usage of the Lua state.
func GetMemoryInfo(L *lua.State) MemoryInfo {
	return MemoryInfo{
		UsedBytes: L.MemoryUsed(),
		GCMode:    L.GetGCMode(),
	}
}

// luaValueToString converts the Lua value at idx to a human-readable string.
func luaValueToString(L *lua.State, idx int) string {
	switch L.Type(idx) {
	case lua.TypeNil:
		return "nil"
	case lua.TypeBoolean:
		if L.ToBoolean(idx) {
			return "true"
		}
		return "false"
	case lua.TypeNumber:
		// Try integer first
		if n, ok := L.ToInteger(idx); ok {
			return fmt.Sprintf("%d", n)
		}
		n, _ := L.ToNumber(idx)
		return fmt.Sprintf("%g", n)
	case lua.TypeString:
		s, _ := L.ToString(idx)
		if len(s) > 50 {
			return fmt.Sprintf("%q...", s[:47])
		}
		return fmt.Sprintf("%q", s)
	case lua.TypeTable:
		return fmt.Sprintf("table: %s", L.ToPointer(idx))
	case lua.TypeFunction:
		return fmt.Sprintf("function: %s", L.ToPointer(idx))
	default:
		return L.TypeName(L.Type(idx))
	}
}
