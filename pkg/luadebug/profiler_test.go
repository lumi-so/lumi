package luadebug_test

import (
	"testing"

	lua "github.com/akzj/go-lua/pkg/lua"
	"github.com/lumi-so/lumi/pkg/luadebug"
)

func TestProfilerBasic(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	p := luadebug.NewProfiler()
	p.Attach(L)

	err := L.DoString(`
        function fib(n)
            if n < 2 then return n end
            return fib(n-1) + fib(n-2)
        end
        fib(15)
    `)
	if err != nil {
		t.Fatal(err)
	}

	p.Detach(L)
	report := p.Report()

	if len(report.Functions) == 0 {
		t.Fatal("no functions profiled")
	}

	// fib should be the top function
	found := false
	for _, fp := range report.Functions {
		if fp.Name == "fib" {
			found = true
			if fp.Calls < 500 { // fib(15) = 987 calls
				t.Errorf("fib calls = %d, expected 500+", fp.Calls)
			}
			break
		}
	}
	if !found {
		t.Error("fib function not found in profile")
	}

	// Report should be printable
	str := report.String()
	if str == "" {
		t.Error("empty report string")
	}
	t.Log(str)
}

func TestProfilerHotLines(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	p := luadebug.NewProfiler()
	p.Attach(L)

	err := L.DoString(`
        local sum = 0
        for i = 1, 1000 do
            sum = sum + i
        end
    `)
	if err != nil {
		t.Fatal(err)
	}

	p.Detach(L)
	report := p.Report()

	if len(report.HotLines) == 0 {
		t.Fatal("no hot lines")
	}

	// The loop body should be the hottest line
	hottest := report.HotLines[0]
	if hottest.Hits < 1000 {
		t.Errorf("hottest line hits = %d, expected 1000+", hottest.Hits)
	}
}
