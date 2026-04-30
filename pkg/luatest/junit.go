package luatest

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// JUnitReporter accumulates one <testsuite> per Report call and can write a
// valid JUnit XML document with a single <testsuites> root.
type JUnitReporter struct {
	suites []junitSuiteXML
}

// NewJUnitReporter creates an empty JUnit reporter.
func NewJUnitReporter() *JUnitReporter {
	return &JUnitReporter{}
}

// Add appends results for one Lua source (file path or "<string>").
func (j *JUnitReporter) Add(source string, summary *RunSummary) {
	if summary == nil {
		return
	}
	suite := junitSuiteXML{
		Name:     source,
		Tests:    summary.Total,
		Failures: summary.Failed,
		Errors:   summary.Errored,
		Time:     suiteSeconds(summary),
	}
	for _, r := range summary.Results {
		tc := junitCaseXML{
			Classname: r.Suite,
			Name:      r.Name,
			Time:      caseSeconds(r.Duration),
		}
		if !r.Passed {
			tc.Failure = &junitFailureXML{
				Message: firstLine(r.Error),
				Body:    r.Error,
			}
		}
		suite.Cases = append(suite.Cases, tc)
	}
	j.suites = append(j.suites, suite)
}

// Report implements Reporter; tb and opt are ignored.
func (j *JUnitReporter) Report(_ testing.TB, source string, summary *RunSummary, _ ReportOptions) {
	j.Add(source, summary)
}

// WriteJUnit writes <?xml ...><testsuites>...</testsuites>.
func (j *JUnitReporter) WriteJUnit(w io.Writer) error {
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		return err
	}
	root := junitRootXML{Suites: j.suites}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(root); err != nil {
		return err
	}
	return nil
}

func suiteSeconds(s *RunSummary) string {
	var d time.Duration
	for _, r := range s.Results {
		d += r.Duration
	}
	return fmtDurationSeconds(d)
}

func caseSeconds(d time.Duration) string {
	return fmtDurationSeconds(d)
}

func fmtDurationSeconds(d time.Duration) string {
	if d <= 0 {
		return "0"
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", d.Seconds()), "0"), ".")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

type junitRootXML struct {
	XMLName xml.Name        `xml:"testsuites"`
	Suites  []junitSuiteXML `xml:"testsuite"`
}

type junitSuiteXML struct {
	XMLName  xml.Name       `xml:"testsuite"`
	Name     string         `xml:"name,attr"`
	Tests    int            `xml:"tests,attr"`
	Failures int            `xml:"failures,attr"`
	Errors   int            `xml:"errors,attr"`
	Time     string         `xml:"time,attr"`
	Cases    []junitCaseXML `xml:"testcase"`
}

type junitCaseXML struct {
	XMLName   xml.Name         `xml:"testcase"`
	Classname string           `xml:"classname,attr"`
	Name      string           `xml:"name,attr"`
	Time      string           `xml:"time,attr"`
	Failure   *junitFailureXML `xml:"failure,omitempty"`
}

type junitFailureXML struct {
	XMLName xml.Name `xml:"failure"`
	Message string   `xml:"message,attr"`
	Body    string   `xml:",chardata"`
}
