package monitor

import (
	"context"
	"testing"
	"time"

	"easy_proxies/internal/config"
)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// The boot sweep and the upstream loop start together. The upstream probe must
// not run until the sweep is done, or it measures our own startup burst.
func TestUpstreamLoopDefersFirstCheckUntilBootSweepFinishes(t *testing.T) {
	m, err := NewManager(Config{ProbeTarget: "http://example.com/generate_204"})
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	h := m.Register(NodeInfo{Tag: "a", Name: "a", URI: "vmess://x@h:443"})
	h.SetProbe(func(ctx context.Context) (time.Duration, error) {
		<-release
		return time.Millisecond, nil
	})

	// Empty upstream_proxy keeps checkUpstreamOnce off the network while still
	// recording a check, which is what we are asserting on.
	s := &Server{mgr: m, cfgSrc: &config.Config{}}
	defer s.StopUpstreamHealthLoop()

	go m.probeAllNodes(time.Second)
	waitFor(t, "boot sweep to start", m.SweepInProgress)

	s.StartUpstreamHealthLoop(time.Hour) // long interval: only the first check matters

	// Give the loop room to misbehave before the sweep releases.
	time.Sleep(50 * time.Millisecond)
	if got := s.upstreamHealthSnapshot().ConsecutiveOK; got != 0 {
		t.Fatalf("upstream was probed during the boot sweep (consecutive_ok=%d)", got)
	}

	close(release)
	waitFor(t, "upstream check after sweep", func() bool {
		return s.upstreamHealthSnapshot().ConsecutiveOK > 0
	})
	if m.SweepInProgress() {
		t.Fatal("sweep still running when the upstream check landed")
	}
}

// Stopping the loop while it waits for a sweep must not leave it parked for the
// full grace period.
func TestUpstreamLoopWaitAbortsOnStop(t *testing.T) {
	m, err := NewManager(Config{ProbeTarget: "http://example.com/generate_204"})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{mgr: m, cfgSrc: &config.Config{}}
	s.StartUpstreamHealthLoop(time.Hour) // no sweep will ever run
	s.StopUpstreamHealthLoop()

	time.Sleep(50 * time.Millisecond)
	if got := s.upstreamHealthSnapshot().ConsecutiveOK; got != 0 {
		t.Fatalf("stopped loop still probed upstream (consecutive_ok=%d)", got)
	}
	if initialUpstreamCheckGrace <= 0 {
		t.Fatal("grace must be a positive bound so a disabled health check cannot park the loop forever")
	}
}
