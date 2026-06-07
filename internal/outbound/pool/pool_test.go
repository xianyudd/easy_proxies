package pool

import (
	"context"
	"net"
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
