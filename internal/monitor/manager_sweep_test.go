package monitor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestProbeWorkerLimitStaysFlatOnBigHosts(t *testing.T) {
	cases := []struct {
		cpus int
		want int
	}{
		{1, minProbeWorkers},
		{4, minProbeWorkers},
		{16, 8},
		{32, maxProbeWorkers}, // was NumCPU()*2 = 64 before the cap
		{128, maxProbeWorkers},
	}
	for _, c := range cases {
		if got := probeWorkerLimit(c.cpus); got != c.want {
			t.Fatalf("probeWorkerLimit(%d) = %d, want %d", c.cpus, got, c.want)
		}
	}
}

func TestSweepConcurrencyRespectsConfiguredCap(t *testing.T) {
	m, err := NewManager(Config{ProbeTarget: "http://example.com/generate_204", ProbeConcurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	var inFlight, peak atomic.Int32
	for i := 0; i < 12; i++ {
		h := m.Register(NodeInfo{Tag: string(rune('a' + i)), Name: "n", URI: "vmess://x@h:443"})
		h.SetProbe(func(ctx context.Context) (time.Duration, error) {
			cur := inFlight.Add(1)
			for {
				old := peak.Load()
				if cur <= old || peak.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			inFlight.Add(-1)
			return time.Millisecond, nil
		})
	}
	m.probeAllNodes(time.Second)
	if got := peak.Load(); got > 2 {
		t.Fatalf("peak concurrent probes = %d, want <= 2", got)
	}
}

// The cap is a property of the manager, not of one sweep: a reload-triggered
// ProbeAllNow overlapping the periodic tick must not double the dial count.
func TestOverlappingSweepsShareOneConcurrencyCap(t *testing.T) {
	m, err := NewManager(Config{ProbeTarget: "http://example.com/generate_204", ProbeConcurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	var inFlight, peak atomic.Int32
	for i := 0; i < 10; i++ {
		h := m.Register(NodeInfo{Tag: string(rune('a' + i)), Name: "n", URI: "vmess://x@h:443"})
		h.SetProbe(func(ctx context.Context) (time.Duration, error) {
			cur := inFlight.Add(1)
			for {
				old := peak.Load()
				if cur <= old || peak.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			inFlight.Add(-1)
			return time.Millisecond, nil
		})
	}

	done := make(chan struct{}, 3)
	for i := 0; i < 3; i++ {
		go func() {
			m.probeAllNodes(time.Second)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 3; i++ {
		<-done
	}

	if got := peak.Load(); got > 2 {
		t.Fatalf("peak concurrent probes across 3 overlapping sweeps = %d, want <= 2", got)
	}
}

func TestSweepSkipsNodesWithFreshLiveTraffic(t *testing.T) {
	m, err := NewManager(Config{ProbeTarget: "http://example.com/generate_204", ProbeConcurrency: 4})
	if err != nil {
		t.Fatal(err)
	}
	m.probeInterval = time.Minute

	var freshProbes, staleProbes atomic.Int32
	fresh := m.Register(NodeInfo{Tag: "fresh", Name: "fresh", URI: "vmess://x@h:443"})
	fresh.SetProbe(func(ctx context.Context) (time.Duration, error) {
		freshProbes.Add(1)
		return time.Millisecond, nil
	})
	stale := m.Register(NodeInfo{Tag: "stale", Name: "stale", URI: "vmess://x@h:443"})
	stale.SetProbe(func(ctx context.Context) (time.Duration, error) {
		staleProbes.Add(1)
		return time.Millisecond, nil
	})

	// Live traffic just succeeded on "fresh"; "stale" has nothing recent.
	fresh.RecordSuccessWithLatency(5 * time.Millisecond)

	m.sweep(time.Second, true)
	if freshProbes.Load() != 0 {
		t.Fatalf("fresh node was probed %d times, want 0", freshProbes.Load())
	}
	if staleProbes.Load() != 1 {
		t.Fatalf("stale node probed %d times, want 1", staleProbes.Load())
	}

	// A manual/full sweep must never skip: the operator asked for real numbers.
	m.probeAllNodes(time.Second)
	if freshProbes.Load() != 1 {
		t.Fatalf("full sweep skipped fresh node, probes=%d", freshProbes.Load())
	}
}

func TestSweepInProgressGuardsUpstreamProbe(t *testing.T) {
	m, err := NewManager(Config{ProbeTarget: "http://example.com/generate_204", ProbeConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	if m.SweepInProgress() {
		t.Fatal("idle manager reported a sweep in progress")
	}
	seen := make(chan bool, 1)
	h := m.Register(NodeInfo{Tag: "a", Name: "a", URI: "vmess://x@h:443"})
	h.SetProbe(func(ctx context.Context) (time.Duration, error) {
		seen <- m.SweepInProgress()
		return time.Millisecond, nil
	})
	m.probeAllNodes(time.Second)
	if !<-seen {
		t.Fatal("SweepInProgress was false while probing")
	}
	if m.SweepInProgress() {
		t.Fatal("SweepInProgress stayed true after the sweep")
	}

	var nilMgr *Manager
	if nilMgr.SweepInProgress() {
		t.Fatal("nil manager must report no sweep")
	}
}

// A reload-triggered ProbeAllNow can overlap the periodic tick. The first sweep
// to finish must not report the pool idle while the other is still dialing.
func TestSweepInProgressSurvivesOverlappingSweeps(t *testing.T) {
	// Cap of 2: the dial budget is now global, so both sweeps need a slot each
	// to be in flight at the same time.
	m, err := NewManager(Config{ProbeTarget: "http://example.com/generate_204", ProbeConcurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	// One token per probe, so we can let exactly one sweep finish at a time.
	release := make(chan struct{}, 2)
	entered := make(chan struct{}, 2)
	h := m.Register(NodeInfo{Tag: "a", Name: "a", URI: "vmess://x@h:443"})
	h.SetProbe(func(ctx context.Context) (time.Duration, error) {
		entered <- struct{}{}
		<-release
		return time.Millisecond, nil
	})

	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			m.probeAllNodes(time.Second)
			done <- struct{}{}
		}()
	}
	<-entered
	<-entered // both sweeps are dialing

	release <- struct{}{}
	<-done // exactly one sweep has finished; the other is still blocked
	if !m.SweepInProgress() {
		t.Fatal("SweepInProgress went false while a second sweep was still dialing")
	}

	release <- struct{}{}
	<-done
	if m.SweepInProgress() {
		t.Fatal("SweepInProgress stayed true after both sweeps finished")
	}
}
