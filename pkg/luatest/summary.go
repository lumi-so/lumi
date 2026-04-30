package luatest

// MergeSummaries combines two summaries (e.g. multiple RunDir calls).
// Nil arguments are treated as empty summaries.
func MergeSummaries(a, b *RunSummary) *RunSummary {
	out := &RunSummary{}
	if a != nil {
		out.Results = append(out.Results, a.Results...)
		out.Passed += a.Passed
		out.Failed += a.Failed
		out.Errored += a.Errored
		out.Total += a.Total
	}
	if b != nil {
		out.Results = append(out.Results, b.Results...)
		out.Passed += b.Passed
		out.Failed += b.Failed
		out.Errored += b.Errored
		out.Total += b.Total
	}
	return out
}
