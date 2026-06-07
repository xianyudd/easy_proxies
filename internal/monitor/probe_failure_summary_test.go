package monitor

import (
	"errors"
	"strings"
	"testing"
)

func TestProbeFailureSummaryAggregatesReasons(t *testing.T) {
	t.Parallel()

	summary := newProbeFailureSummary(2)
	summary.Add("node-a", errors.New("unexpected status: 400 Bad Request"))
	summary.Add("node-b", errors.New("unexpected status: 400 Bad Request"))
	summary.Add("node-c", errors.New("connection reset by peer"))
	summary.Add("node-d", errors.New("timeout"))

	got := summary.String()
	checks := []string{
		"probe failures aggregated: total=4 reasons=3",
		"2x unexpected status: 400 Bad Request (sample=node-a)",
		"1x connection reset by peer (sample=node-c)",
		"+1 more reasons",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q does not contain %q", got, want)
		}
	}
	if strings.Contains(got, "node-b") {
		t.Fatalf("summary should not list every failed node, got %q", got)
	}
}
