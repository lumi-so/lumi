package luadebug

import (
	"fmt"
	"sort"
	"strings"
	"time"

	lua "github.com/akzj/go-lua/pkg/lua"
)

// Profiler collects execution timing data using Lua debug hooks.
// Attach it to a State, run code, then call Report() for results.
//
// Example:
//
//	p := luadebug.NewProfiler()
//	p.Attach(L)
//	L.DoFile("app.lua")
//	p.Detach(L)
//	fmt.Println(p.Report())
type Profiler struct {
	functions map[string]*FuncProfile
	lines     map[string]*LineProfile
	callStack []callFrame
	startTime time.Time
}

type callFrame struct {
	key       string // "source:lineDefined"
	enteredAt time.Time
}

// FuncProfile holds timing data for a single function.
type FuncProfile struct {
	Source    string        // source file
	Name     string        // function name (or "<anonymous>")
	Line     int           // line where defined
	Calls    int           // number of times called
	TotalTime time.Duration // total time in this function (inclusive)
	SelfTime  time.Duration // time in this function excluding children
}

// LineProfile holds execution count for a source line.
type LineProfile struct {
	Source string
	Line   int
	Hits   int // number of times this line was executed
}

// ProfileReport is the output of a profiling session.
type ProfileReport struct {
	Functions []*FuncProfile // sorted by TotalTime descending
	HotLines  []*LineProfile // sorted by Hits descending, top 20
	Duration  time.Duration  // total profiling duration
}

// NewProfiler creates a new Profiler.
func NewProfiler() *Profiler {
	return &Profiler{
		functions: make(map[string]*FuncProfile),
		lines:     make(map[string]*LineProfile),
	}
}

// Attach installs profiling hooks on the Lua state.
// The profiler uses MaskCall | MaskRet | MaskLine hooks.
func (p *Profiler) Attach(L *lua.State) {
	p.startTime = time.Now()
	p.callStack = nil

	L.SetHook(func(L *lua.State, event int, currentLine int) {
		switch event {
		case lua.HookEventCall, lua.HookEventTailCall:
			p.onCall(L, event)
		case lua.HookEventReturn:
			p.onReturn()
		case lua.HookEventLine:
			p.onLine(L, currentLine)
		}
	}, lua.MaskCall|lua.MaskRet|lua.MaskLine, 0)
}

// Detach removes profiling hooks.
func (p *Profiler) Detach(L *lua.State) {
	L.SetHook(nil, 0, 0)
}

func (p *Profiler) onCall(L *lua.State, event int) {
	ar, ok := L.GetStack(0)
	if !ok {
		return
	}
	L.GetInfo("nSl", ar)

	key := funcKey(ar)
	now := time.Now()

	// If tail call, pop the current frame first
	if event == lua.HookEventTailCall && len(p.callStack) > 0 {
		p.popFrame(now)
	}

	p.callStack = append(p.callStack, callFrame{
		key:       key,
		enteredAt: now,
	})

	// Ensure function entry exists
	if _, exists := p.functions[key]; !exists {
		name := ar.Name
		if name == "" {
			name = "<anonymous>"
		}
		p.functions[key] = &FuncProfile{
			Source: ar.ShortSrc,
			Name:   name,
			Line:   ar.LineDefined,
		}
	}
	p.functions[key].Calls++
}

func (p *Profiler) onReturn() {
	if len(p.callStack) == 0 {
		return
	}
	now := time.Now()
	p.popFrame(now)
}

func (p *Profiler) popFrame(now time.Time) {
	if len(p.callStack) == 0 {
		return
	}
	frame := p.callStack[len(p.callStack)-1]
	p.callStack = p.callStack[:len(p.callStack)-1]

	elapsed := now.Sub(frame.enteredAt)

	if fp, exists := p.functions[frame.key]; exists {
		fp.TotalTime += elapsed
		fp.SelfTime += elapsed
	}

	// Subtract child time from parent's self time
	if len(p.callStack) > 0 {
		parentKey := p.callStack[len(p.callStack)-1].key
		if pp, exists := p.functions[parentKey]; exists {
			pp.SelfTime -= elapsed
		}
	}
}

func (p *Profiler) onLine(L *lua.State, line int) {
	ar, ok := L.GetStack(0)
	if !ok {
		return
	}
	L.GetInfo("S", ar)

	key := fmt.Sprintf("%s:%d", ar.ShortSrc, line)
	if lp, exists := p.lines[key]; exists {
		lp.Hits++
	} else {
		p.lines[key] = &LineProfile{
			Source: ar.ShortSrc,
			Line:   line,
			Hits:   1,
		}
	}
}

// Report generates the profiling report.
func (p *Profiler) Report() *ProfileReport {
	report := &ProfileReport{
		Duration: time.Since(p.startTime),
	}

	// Collect and sort functions by TotalTime
	for _, fp := range p.functions {
		report.Functions = append(report.Functions, fp)
	}
	sort.Slice(report.Functions, func(i, j int) bool {
		return report.Functions[i].TotalTime > report.Functions[j].TotalTime
	})

	// Collect and sort hot lines by Hits
	for _, lp := range p.lines {
		report.HotLines = append(report.HotLines, lp)
	}
	sort.Slice(report.HotLines, func(i, j int) bool {
		return report.HotLines[i].Hits > report.HotLines[j].Hits
	})
	// Top 20 hot lines
	if len(report.HotLines) > 20 {
		report.HotLines = report.HotLines[:20]
	}

	return report
}

// String returns a human-readable profiling report.
func (r *ProfileReport) String() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("=== Profile Report (%.3fs) ===\n\n", r.Duration.Seconds()))

	sb.WriteString("--- Top Functions (by total time) ---\n")
	sb.WriteString(fmt.Sprintf("%-40s %8s %8s %8s %6s\n", "Function", "Total", "Self", "Calls", "Avg"))
	sb.WriteString(strings.Repeat("-", 80) + "\n")

	limit := len(r.Functions)
	if limit > 20 {
		limit = 20
	}
	for i := 0; i < limit; i++ {
		fp := r.Functions[i]
		label := fmt.Sprintf("%s:%d %s", fp.Source, fp.Line, fp.Name)
		if len(label) > 40 {
			label = label[:37] + "..."
		}
		avg := time.Duration(0)
		if fp.Calls > 0 {
			avg = fp.TotalTime / time.Duration(fp.Calls)
		}
		sb.WriteString(fmt.Sprintf("%-40s %8s %8s %8d %6s\n",
			label,
			formatDuration(fp.TotalTime),
			formatDuration(fp.SelfTime),
			fp.Calls,
			formatDuration(avg),
		))
	}

	if len(r.HotLines) > 0 {
		sb.WriteString("\n--- Hot Lines (by execution count) ---\n")
		sb.WriteString(fmt.Sprintf("%-40s %10s\n", "Location", "Hits"))
		sb.WriteString(strings.Repeat("-", 52) + "\n")
		for _, lp := range r.HotLines {
			loc := fmt.Sprintf("%s:%d", lp.Source, lp.Line)
			sb.WriteString(fmt.Sprintf("%-40s %10d\n", loc, lp.Hits))
		}
	}

	return sb.String()
}

func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1000)
	}
	if d < time.Second {
		return fmt.Sprintf("%.1fms", float64(d.Nanoseconds())/1e6)
	}
	return fmt.Sprintf("%.3fs", d.Seconds())
}

func funcKey(ar *lua.DebugInfo) string {
	return fmt.Sprintf("%s:%d", ar.ShortSrc, ar.LineDefined)
}
