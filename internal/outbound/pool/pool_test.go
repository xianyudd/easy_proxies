package pool

import (
	"context"
	"net"
	"errors"
	"testing"
	"time"

	"easy_proxies/internal/monitor"

	"github.com/sagernet/sing-box/adapter/outbound"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type testOutbound struct {
	outbound.Adapter
}

func newTestOutbound(tag string, networks []string) *testOutbound {
	return &testOutbound{
		Adapter: outbound.NewAdapter("test", tag, networks, nil),
	}
}

func (t testOutbound) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	return nil, nil
}

func (t testOutbound) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, nil
}

func TestAvailableMembersLockedExcludesCheckedUnavailableNodes(t *testing.T) {
	mgr, err := monitor.NewManager(monitor.Config{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	available := mgr.Register(monitor.NodeInfo{Tag: "available"})
	available.MarkInitialCheckDone(true)

	unavailable := mgr.Register(monitor.NodeInfo{Tag: "unavailable"})
	unavailable.MarkInitialCheckDone(false)

	unchecked := mgr.Register(monitor.NodeInfo{Tag: "unchecked"})

	p := &poolOutbound{
		members: []*memberState{
			{tag: "available", outbound: newTestOutbound("available", []string{N.NetworkTCP}), entry: available, shared: acquireSharedState("available")},
			{tag: "unavailable", outbound: newTestOutbound("unavailable", []string{N.NetworkTCP}), entry: unavailable, shared: acquireSharedState("unavailable")},
			{tag: "unchecked", outbound: newTestOutbound("unchecked", []string{N.NetworkTCP}), entry: unchecked, shared: acquireSharedState("unchecked")},
		},
	}

	candidates, fallback := p.availableMembersLocked(time.Now(), N.NetworkTCP, nil, nil)

	if len(candidates) != 2 {
		t.Fatalf("expected 2 primary candidates, got %d", len(candidates))
	}
	for _, member := range candidates {
		if member.tag == "unavailable" {
			t.Fatalf("checked-unavailable member was included in primary candidates")
		}
	}
	if len(fallback) != 1 || fallback[0].tag != "unavailable" {
		t.Fatalf("expected unavailable fallback candidate, got %#v", fallback)
	}
}

func TestAvailableMembersLockedKeepsUnavailableNodesAsFallback(t *testing.T) {
	mgr, err := monitor.NewManager(monitor.Config{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	first := mgr.Register(monitor.NodeInfo{Tag: "first"})
	first.MarkInitialCheckDone(false)
	second := mgr.Register(monitor.NodeInfo{Tag: "second"})
	second.MarkInitialCheckDone(false)

	p := &poolOutbound{
		members: []*memberState{
			{tag: "first", outbound: newTestOutbound("first", []string{N.NetworkTCP}), entry: first, shared: acquireSharedState("first")},
			{tag: "second", outbound: newTestOutbound("second", []string{N.NetworkTCP}), entry: second, shared: acquireSharedState("second")},
		},
	}

	candidates, fallback := p.availableMembersLocked(time.Now(), N.NetworkTCP, nil, nil)

	if len(candidates) != 0 {
		t.Fatalf("expected no primary candidates, got %d", len(candidates))
	}
	if len(fallback) != 2 {
		t.Fatalf("expected 2 fallback candidates, got %d", len(fallback))
	}
}

func TestSelectMemberLatencyPrefersFasterNode(t *testing.T) {
	ResetSharedStateStore()
	mgr, err := monitor.NewManager(monitor.Config{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	fast := mgr.Register(monitor.NodeInfo{Tag: "fast"})
	fast.MarkInitialCheckDone(true)
	fast.RecordSuccessWithLatency(100 * time.Millisecond)

	slow := mgr.Register(monitor.NodeInfo{Tag: "slow"})
	slow.MarkInitialCheckDone(true)
	slow.RecordSuccessWithLatency(800 * time.Millisecond)

	fastShared := acquireSharedState("fast")
	slowShared := acquireSharedState("slow")
	fastShared.attachEntry(fast)
	slowShared.attachEntry(slow)

	p := &poolOutbound{mode: modeLatency}
	candidates := []*memberState{
		{tag: "slow", outbound: newTestOutbound("slow", []string{N.NetworkTCP}), entry: slow, shared: slowShared},
		{tag: "fast", outbound: newTestOutbound("fast", []string{N.NetworkTCP}), entry: fast, shared: fastShared},
	}
	got := p.selectMember(candidates)
	if got == nil || got.tag != "fast" {
		t.Fatalf("latency mode selected %#v, want fast", got)
	}
}

func TestSelectMemberBalancePrefersFasterWhenIdle(t *testing.T) {
	ResetSharedStateStore()
	mgr, err := monitor.NewManager(monitor.Config{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	fast := mgr.Register(monitor.NodeInfo{Tag: "fast"})
	fast.MarkInitialCheckDone(true)
	fast.RecordSuccessWithLatency(200 * time.Millisecond)

	slow := mgr.Register(monitor.NodeInfo{Tag: "slow"})
	slow.MarkInitialCheckDone(true)
	slow.RecordSuccessWithLatency(900 * time.Millisecond)

	fastShared := acquireSharedState("fast")
	slowShared := acquireSharedState("slow")
	fastShared.attachEntry(fast)
	slowShared.attachEntry(slow)

	p := &poolOutbound{mode: modeBalance}
	candidates := []*memberState{
		{tag: "slow", outbound: newTestOutbound("slow", []string{N.NetworkTCP}), entry: slow, shared: slowShared},
		{tag: "fast", outbound: newTestOutbound("fast", []string{N.NetworkTCP}), entry: fast, shared: fastShared},
	}
	got := p.selectMember(candidates)
	if got == nil || got.tag != "fast" {
		t.Fatalf("balance mode selected %#v, want fast when both idle", got)
	}
}

func TestSelectMemberBalanceActivePenaltyCanPreferIdleSlowerNode(t *testing.T) {
	ResetSharedStateStore()
	mgr, err := monitor.NewManager(monitor.Config{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// fast=200ms with 5 active → score 200+500=700
	// slow=300ms with 0 active → score 300 → slow wins
	fast := mgr.Register(monitor.NodeInfo{Tag: "fast"})
	fast.MarkInitialCheckDone(true)
	fast.RecordSuccessWithLatency(200 * time.Millisecond)

	slow := mgr.Register(monitor.NodeInfo{Tag: "slow"})
	slow.MarkInitialCheckDone(true)
	slow.RecordSuccessWithLatency(300 * time.Millisecond)

	fastShared := acquireSharedState("fast")
	slowShared := acquireSharedState("slow")
	fastShared.attachEntry(fast)
	slowShared.attachEntry(slow)
	for i := 0; i < 5; i++ {
		fastShared.incActive()
	}

	p := &poolOutbound{mode: modeBalance}
	candidates := []*memberState{
		{tag: "fast", outbound: newTestOutbound("fast", []string{N.NetworkTCP}), entry: fast, shared: fastShared},
		{tag: "slow", outbound: newTestOutbound("slow", []string{N.NetworkTCP}), entry: slow, shared: slowShared},
	}
	got := p.selectMember(candidates)
	if got == nil || got.tag != "slow" {
		t.Fatalf("balance mode selected %#v, want idle slower node under active penalty", got)
	}
}

func TestSelectMemberLatencyPrefersProbedOverUnknown(t *testing.T) {
	ResetSharedStateStore()
	mgr, err := monitor.NewManager(monitor.Config{})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	probed := mgr.Register(monitor.NodeInfo{Tag: "probed"})
	probed.MarkInitialCheckDone(true)
	probed.RecordSuccessWithLatency(1500 * time.Millisecond)

	unknown := mgr.Register(monitor.NodeInfo{Tag: "unknown"})
	unknown.MarkInitialCheckDone(true) // available but no latency sample yet

	probedShared := acquireSharedState("probed")
	unknownShared := acquireSharedState("unknown")
	probedShared.attachEntry(probed)
	unknownShared.attachEntry(unknown)

	p := &poolOutbound{mode: modeLatency}
	candidates := []*memberState{
		{tag: "unknown", outbound: newTestOutbound("unknown", []string{N.NetworkTCP}), entry: unknown, shared: unknownShared},
		{tag: "probed", outbound: newTestOutbound("probed", []string{N.NetworkTCP}), entry: probed, shared: probedShared},
	}
	got := p.selectMember(candidates)
	if got == nil || got.tag != "probed" {
		t.Fatalf("latency mode selected %#v, want probed over unknown", got)
	}
}

func TestNormalizeOptionsAcceptsLatencyMode(t *testing.T) {
	got := normalizeOptions(Options{Mode: "LATENCY", Members: []string{"a"}})
	if got.Mode != modeLatency {
		t.Fatalf("mode=%q, want %q", got.Mode, modeLatency)
	}
}


func TestRecordFailureUsesCallerBlacklistDuration(t *testing.T) {
	ResetSharedStateStore()
	tag := "mp-node"
	state := acquireSharedState(tag)
	_, bl1, _ := state.recordFailure(errors.New("e1"), 2, 2*time.Second)
	if bl1 {
		t.Fatalf("should not blacklist on first failure")
	}
	_, bl2, until := state.recordFailure(errors.New("e2"), 2, 2*time.Second)
	if !bl2 {
		t.Fatalf("expected blacklist on second failure")
	}
	remain := time.Until(until)
	if remain > 5*time.Second || remain < 0 {
		t.Fatalf("expected ~2s blacklist window, remain=%v until=%v", remain, until)
	}
	if !state.isBlacklisted(time.Now()) {
		t.Fatalf("expected blacklisted now")
	}
}

func TestEffectiveMultiportDefaults(t *testing.T) {
	// config helper is in config package; keep a smoke via Options normalize thresholds only.
	opts := normalizeOptions(Options{Mode: "balance", FailureThreshold: 3, BlacklistDuration: 24 * time.Hour, Members: []string{"a"}})
	if opts.FailureThreshold != 3 {
		t.Fatalf("threshold=%d", opts.FailureThreshold)
	}
}
