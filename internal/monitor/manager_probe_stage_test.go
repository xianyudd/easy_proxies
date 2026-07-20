package monitor

import (
	"errors"
	"testing"
	"time"
)

func TestRecordFailureStageAndZombieBlacklist(t *testing.T) {
	m, err := NewManager(Config{ProbeTarget: "http://example.com/generate_204"})
	if err != nil {
		t.Fatal(err)
	}
	h := m.Register(NodeInfo{Tag: "z1", Name: "z1", URI: "hysteria2://x@h:443", Protocol: "hysteria2"})
	if h == nil {
		t.Fatal("register failed")
	}
	blacklisted := 0
	h.SetBlacklistFn(func(d time.Duration) {
		if d <= 0 {
			t.Fatalf("expected positive duration, got %v", d)
		}
		blacklisted++
	})
	for i := 0; i < 14; i++ {
		h.RecordFailureStage(errors.New("timeout"), ProbeStageDial)
	}
	snap := h.Snapshot()
	if snap.FailureCount != 14 || snap.Blacklisted {
		t.Fatalf("before threshold: fail=%d bl=%v stage=%q err=%q", snap.FailureCount, snap.Blacklisted, snap.LastErrorStage, snap.LastError)
	}
	if snap.LastErrorStage != ProbeStageDial || snap.LastError == "" || snap.LastError[0] != '[' {
		t.Fatalf("expected staged error, got stage=%q err=%q", snap.LastErrorStage, snap.LastError)
	}
	h.RecordFailureStage(errors.New("timeout"), ProbeStageDial)
	snap = h.Snapshot()
	if !snap.Blacklisted || blacklisted != 1 {
		t.Fatalf("expected zombie blacklist, bl=%v calls=%d fail=%d", snap.Blacklisted, blacklisted, snap.FailureCount)
	}
	if snap.Protocol != "hysteria2" {
		t.Fatalf("protocol=%q", snap.Protocol)
	}
}

func TestRegisterFillsProtocolFromURI(t *testing.T) {
	m, err := NewManager(Config{})
	if err != nil {
		t.Fatal(err)
	}
	h := m.Register(NodeInfo{Tag: "a", Name: "a", URI: "anytls://pw@h:443?sni=x"})
	if h.Snapshot().Protocol != "anytls" {
		t.Fatalf("got %q", h.Snapshot().Protocol)
	}
}
