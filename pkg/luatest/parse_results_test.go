package luatest

import (
	"fmt"
	"testing"
	"time"

	lua "github.com/akzj/go-lua/pkg/lua"
)

func TestParseResults_contract(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	err := L.DoString(`return {
		passed=2, failed=1, errored=0, total=3,
		results={
			{ suite="S", name="a", passed=true, duration=0.5 },
			{ suite="S", name="b", passed=false, error="nope", duration=0.001 },
			{ suite="T", name="c", passed=true, duration=0 },
		}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	run := &Runner{t: t}
	sum := run.parseResults(L)
	if sum.Passed != 2 || sum.Failed != 1 || sum.Total != 3 || sum.Errored != 0 {
		t.Fatalf("counts: %+v", sum)
	}
	if len(sum.Results) != 3 {
		t.Fatalf("len results %d", len(sum.Results))
	}
	if !sum.Results[0].Passed || sum.Results[0].Suite != "S" || sum.Results[0].Name != "a" {
		t.Fatalf("first: %+v", sum.Results[0])
	}
	if sum.Results[1].Passed || sum.Results[1].Error != "nope" {
		t.Fatalf("second: %+v", sum.Results[1])
	}
	d0 := sum.Results[0].Duration
	if d0 < 499*time.Millisecond || d0 > 501*time.Millisecond {
		t.Fatalf("duration got %v want ~500ms", d0)
	}
}

func TestParseResults_rejectsNonTable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	L.PushNumber(42)
	st := &stubTB{TB: t}
	func() {
		defer func() { recover() }()
		run := &Runner{t: st}
		run.parseResults(L)
	}()
	if st.fatal == "" {
		t.Fatal("expected Fatal for non-table stack value")
	}
}

type stubTB struct {
	testing.TB
	fatal string
}

func (s *stubTB) Helper() {}

func (s *stubTB) Fatal(args ...interface{}) {
	s.fatal = fmt.Sprint(args...)
	panic("fatal")
}

func (s *stubTB) Fatalf(format string, args ...interface{}) {
	s.fatal = fmt.Sprintf(format, args...)
	panic("fatal")
}
