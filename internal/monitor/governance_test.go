package monitor

import (
	"errors"
	"testing"
	"time"
)

func TestClassifyStructuralWhenConfigured(t *testing.T) {
	g := DefaultGovernance()
	// Recovery defaults: no permanent structural isolation.
	if g.ClassifyStructural("hysteria2", "hysteria2://x@h:443") != "" {
		t.Fatal("default recovery mode should not isolate hy2")
	}
	// Explicit isolation still works when enabled by operator.
	g.IsolateProtocols = []string{"hysteria2", "hy2", "anytls"}
	g.IsolateVlessReality = true
	g.HostQuarantine = true
	if g.ClassifyStructural("hysteria2", "hysteria2://x@h:443") == "" {
		t.Fatal("expected hy2 isolate when configured")
	}
	if g.ClassifyStructural("anytls", "anytls://x@h:443") == "" {
		t.Fatal("expected anytls isolate when configured")
	}
	uri := "vless://00000000-0000-0000-0000-000000000001@aws.example:443?security=reality&type=tcp&flow=xtls-rprx-vision&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if g.ClassifyStructural("vless", uri) == "" {
		t.Fatal("expected reality isolate when configured")
	}
	ws := "vless://00000000-0000-0000-0000-000000000001@h:443?security=tls&type=ws&path=/x"
	if g.ClassifyStructural("vless", ws) != "" {
		t.Fatal("tls-ws should not be structural")
	}
	if g.ClassifyStructural("vmess", "vmess://xx") != "" {
		t.Fatal("vmess never structural")
	}
}

func TestHostQuarantineWhenConfigured(t *testing.T) {
	g := DefaultGovernance()
	g.HostQuarantine = true
	uri := "vless://00000000-0000-0000-0000-000000000001@cloudflare.182682.xyz:443?security=tls&type=ws&path=/a"
	if g.ClassifyHostQuarantine("vless", uri) == "" {
		t.Fatal("expected host quarantine when enabled")
	}
	uri2 := "vless://00000000-0000-0000-0000-000000000001@unamecf.example:443?security=tls&type=ws&path=/a"
	if g.ClassifyHostQuarantine("vless", uri2) != "" {
		t.Fatal("unknown host should not quarantine")
	}
}

func TestZombieSkipVmessAndFlakyVlessWS(t *testing.T) {
	g := DefaultGovernance()
	m, err := NewManager(Config{Governance: g})
	if err != nil {
		t.Fatal(err)
	}
	h := m.Register(NodeInfo{Tag: "v1", Name: "v1", URI: "vmess://xx", Protocol: "vmess"})
	h.ref.mu.Lock()
	h.ref.blacklist = false
	h.ref.quarantineReason = ""
	h.ref.mu.Unlock()
	bl := 0
	h.SetBlacklistFn(func(d time.Duration) { bl++ })
	for i := 0; i < 20; i++ {
		h.RecordFailureStage(errors.New("x"), ProbeStageDial)
	}
	if h.Snapshot().Blacklisted || bl != 0 {
		t.Fatalf("vmess should skip zombie, bl=%v calls=%d", h.Snapshot().Blacklisted, bl)
	}

	h2 := m.Register(NodeInfo{Tag: "w1", Name: "w1", URI: "vless://u@h:443?security=tls&type=ws", Protocol: "vless"})
	h2.ref.mu.Lock()
	h2.ref.blacklist = false
	h2.ref.quarantineReason = ""
	h2.ref.success = 6
	h2.ref.mu.Unlock()
	bl = 0
	h2.SetBlacklistFn(func(d time.Duration) { bl++ })
	for i := 0; i < 20; i++ {
		h2.RecordFailureStage(errors.New("x"), ProbeStageDial)
	}
	if bl != 0 {
		t.Fatalf("flaky vless-ws should skip zombie auto, blCalls=%d", bl)
	}
}

func TestZombieBansNeverSuccessNonVmess(t *testing.T) {
	g := DefaultGovernance()
	m, err := NewManager(Config{Governance: g})
	if err != nil {
		t.Fatal(err)
	}
	h := m.Register(NodeInfo{Tag: "hy", Name: "hy", URI: "hysteria2://p@h:443", Protocol: "hysteria2"})
	// no structural quarantine in recovery defaults
	if h.Snapshot().QuarantineReason != "" {
		t.Fatalf("recovery defaults should not structural-quarantine hy2, got %q", h.Snapshot().QuarantineReason)
	}
	bl := 0
	h.SetBlacklistFn(func(d time.Duration) { bl++ })
	for i := 0; i < g.ZombieZeroSuccessFails; i++ {
		h.RecordFailureStage(errors.New("timeout"), ProbeStageDial)
	}
	snap := h.Snapshot()
	if !snap.Blacklisted || bl != 1 {
		t.Fatalf("expected zombie ban after %d fails, bl=%v calls=%d fail=%d", g.ZombieZeroSuccessFails, snap.Blacklisted, bl, snap.FailureCount)
	}
}

func TestStructuralQuarantineOnRegisterWhenConfigured(t *testing.T) {
	g := DefaultGovernance()
	g.IsolateProtocols = []string{"hysteria2"}
	m, err := NewManager(Config{Governance: g})
	if err != nil {
		t.Fatal(err)
	}
	h := m.Register(NodeInfo{Tag: "hy", Name: "hy", URI: "hysteria2://p@h:443", Protocol: "hysteria2"})
	snap := h.Snapshot()
	if snap.QuarantineReason == "" || snap.Available {
		t.Fatalf("expected structural quarantine unavailable, got bl=%v avail=%v reason=%q", snap.Blacklisted, snap.Available, snap.QuarantineReason)
	}
	if snap.Blacklisted {
		t.Fatalf("structural quarantine must not set blacklisted=true, reason=%q", snap.QuarantineReason)
	}
}
