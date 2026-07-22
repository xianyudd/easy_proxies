package monitor

import (
	"testing"
	"time"
)

func TestLatencyBreakdownFields(t *testing.T) {
	mgr, err := NewManager(Config{})
	if err != nil {
		t.Fatal(err)
	}
	h := mgr.Register(NodeInfo{Tag: "n1"})
	h.RecordSuccessWithLatencyBreakdown(1500*time.Millisecond, 400*time.Millisecond, 1100*time.Millisecond)
	snap := h.Snapshot()
	if snap.LastLatencyMs < 1400 || snap.LastLatencyMs > 1600 {
		t.Fatalf("total=%d", snap.LastLatencyMs)
	}
	if snap.LastDialMs < 300 || snap.LastDialMs > 500 {
		t.Fatalf("dial=%d", snap.LastDialMs)
	}
	if snap.LastHTTPMs < 1000 || snap.LastHTTPMs > 1200 {
		t.Fatalf("http=%d", snap.LastHTTPMs)
	}
}
