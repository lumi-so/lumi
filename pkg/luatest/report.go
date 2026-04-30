package luatest

import (
	"fmt"
	"testing"
	"time"
)

// ReportOptions configures how a Reporter emits output.
type ReportOptions struct {
	Verbose  bool
	Subtests bool // if true and TB is *testing.T, one Go subtest per Lua case
}

// Reporter handles reporting for one Lua run (file or string).
type Reporter interface {
	Report(tb testing.TB, source string, summary *RunSummary, opt ReportOptions)
}

// TBReporter is the default reporter: errors on failures, optional verbose logs.
// When ReportOptions.Subtests is true and tb is *testing.T, each Lua case becomes
// a Go subtest (supports go test -run).
type TBReporter struct{}

// Report implements Reporter.
func (TBReporter) Report(tb testing.TB, source string, summary *RunSummary, opt ReportOptions) {
	if summary == nil {
		return
	}
	tb.Helper()
	if opt.Subtests {
		if tt, ok := tb.(*testing.T); ok {
			reportSubtests(tt, source, summary, opt.Verbose)
			return
		}
	}
	reportFlat(tb, source, summary, opt.Verbose)
}

func reportSubtests(t *testing.T, source string, summary *RunSummary, verbose bool) {
	t.Helper()
	for _, result := range summary.Results {
		result := result
		name := subtestName(source, result.Suite, result.Name)
		t.Run(name, func(t *testing.T) {
			t.Helper()
			if result.Passed {
				if verbose {
					t.Logf("ok (%.1fms)", float64(result.Duration)/float64(time.Millisecond))
				}
				return
			}
			t.Errorf("%s", result.Error)
		})
	}
	if summary.Failed > 0 || summary.Errored > 0 {
		t.Errorf("%s: %d passed, %d failed, %d errors (total %d)",
			source, summary.Passed, summary.Failed, summary.Errored, summary.Total)
	} else if verbose && summary.Total > 0 {
		t.Logf("%s: all %d tests passed", source, summary.Total)
	}
}

func subtestName(source, suite, name string) string {
	if source != "" && source != "<string>" {
		return fmt.Sprintf("%s/%s/%s", source, suite, name)
	}
	return fmt.Sprintf("%s/%s", suite, name)
}

func reportFlat(tb testing.TB, source string, summary *RunSummary, verbose bool) {
	tb.Helper()
	for _, result := range summary.Results {
		testName := fmt.Sprintf("%s/%s", result.Suite, result.Name)
		if result.Passed {
			if verbose {
				tb.Logf("  ✓ %s (%.1fms)", testName, float64(result.Duration)/float64(time.Millisecond))
			}
		} else {
			tb.Errorf("  ✗ %s\n    %s", testName, result.Error)
		}
	}
	if summary.Failed > 0 || summary.Errored > 0 {
		tb.Errorf("%s: %d passed, %d failed, %d errors (total %d)",
			source, summary.Passed, summary.Failed, summary.Errored, summary.Total)
	} else if verbose {
		tb.Logf("%s: all %d tests passed", source, summary.Total)
	}
}

// MultiReporter runs several reporters in order (e.g. TB + JUnit).
func MultiReporter(reps ...Reporter) Reporter {
	return multiReporter(reps)
}

type multiReporter []Reporter

func (m multiReporter) Report(tb testing.TB, source string, summary *RunSummary, opt ReportOptions) {
	for _, r := range m {
		if r != nil {
			r.Report(tb, source, summary, opt)
		}
	}
}
