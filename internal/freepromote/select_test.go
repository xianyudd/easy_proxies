package freepromote

import (
	"testing"
	"time"

	"easy_proxies/internal/config"
)

func TestSelectCandidatesFiltersAndOrders(t *testing.T) {
	cfg := config.FreeProxyPromoteConfig{
		Enabled:             true,
		BatchSize:           2,
		MaxPromoted:         10,
		MaxLatencyMS:        500,
		MinSuccessCount:     1,
		MaxFailureCount:     -1,
		RecentSuccessWithin: -1,
		NamePrefix:          "free-promoted-",
	}
	nodes := []config.NodeConfig{
		{Name: "free-promoted-aaaa", URI: "http://1.1.1.1:80", Source: config.NodeSourceFile},
	}
	snaps := []Snapshot{
		{Name: "a", URI: "http://1.1.1.1:80", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 5, LastLatencyMs: 100},
		{Name: "b", URI: "http://2.2.2.2:80", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 3, LastLatencyMs: 200},
		{Name: "c", URI: "http://3.3.3.3:80", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 9, LastLatencyMs: 50},
		{Name: "d", URI: "http://4.4.4.4:80", Source: "free_proxy", Available: false, InitialCheckDone: true, SuccessCount: 9, LastLatencyMs: 10},
		{Name: "e", URI: "http://5.5.5.5:80", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 9, LastLatencyMs: 900},
		{Name: "f", URI: "http://6.6.6.6:80", Source: "subscription", Available: true, InitialCheckDone: true, SuccessCount: 9, LastLatencyMs: 10},
		{Name: "g", URI: "http://7.7.7.7:80", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 0, LastLatencyMs: 20},
	}
	got := SelectCandidates(snaps, nodes, cfg)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %#v", len(got), got)
	}
	if got[0].URI != "http://3.3.3.3:80" {
		t.Fatalf("first=%s want lowest latency non-dup", got[0].URI)
	}
	if got[1].URI != "http://2.2.2.2:80" {
		t.Fatalf("second=%s", got[1].URI)
	}
}

func TestSelectCandidatesRespectsMaxPromoted(t *testing.T) {
	cfg := config.FreeProxyPromoteConfig{
		Enabled: true, BatchSize: 5, MaxPromoted: 2, MaxLatencyMS: 1000, MinSuccessCount: 1, MaxFailureCount: -1, RecentSuccessWithin: -1, NamePrefix: "free-promoted-",
	}
	nodes := []config.NodeConfig{
		{Name: "free-promoted-1", URI: "http://9.9.9.9:80", Source: config.NodeSourceFile},
		{Name: "free-promoted-2", URI: "http://8.8.8.8:80", Source: config.NodeSourceFile},
	}
	snaps := []Snapshot{
		{Name: "a", URI: "http://1.1.1.1:80", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 2, LastLatencyMs: 10},
	}
	got := SelectCandidates(snaps, nodes, cfg)
	if len(got) != 0 {
		t.Fatalf("expected empty at cap, got %#v", got)
	}
}

func TestSelectCandidatesDedupAlreadyNonFree(t *testing.T) {
	cfg := config.FreeProxyPromoteConfig{
		Enabled: true, BatchSize: 5, MaxPromoted: 10, MaxLatencyMS: 1000, MinSuccessCount: 1, MaxFailureCount: -1, RecentSuccessWithin: -1, NamePrefix: "free-promoted-",
	}
	nodes := []config.NodeConfig{
		{Name: "manual", URI: "http://1.1.1.1:80", Source: config.NodeSourceInline},
	}
	snaps := []Snapshot{
		{Name: "a", URI: "http://1.1.1.1:80", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 2, LastLatencyMs: 10},
		{Name: "b", URI: "http://2.2.2.2:80", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 2, LastLatencyMs: 20},
	}
	got := SelectCandidates(snaps, nodes, cfg)
	if len(got) != 1 || got[0].URI != "http://2.2.2.2:80" {
		t.Fatalf("got %#v", got)
	}
}

func TestSelectCandidatesFiltersFailureAndRecentSuccess(t *testing.T) {
	now := time.Now()
	cfg := config.FreeProxyPromoteConfig{
		Enabled: true, BatchSize: 5, MaxPromoted: 10, MaxLatencyMS: 1000, MinSuccessCount: 1,
		MaxFailureCount: 1, RecentSuccessWithin: 30 * time.Minute, NamePrefix: "free-promoted-",
	}
	snaps := []Snapshot{
		{Name: "good", URI: "http://1.1.1.1:80", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 3, FailureCount: 1, LastLatencyMs: 10, LastSuccess: now.Add(-5 * time.Minute)},
		{Name: "failed", URI: "http://2.2.2.2:80", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 3, FailureCount: 2, LastLatencyMs: 20, LastSuccess: now.Add(-5 * time.Minute)},
		{Name: "stale", URI: "http://3.3.3.3:80", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 3, FailureCount: 0, LastLatencyMs: 30, LastSuccess: now.Add(-2 * time.Hour)},
		{Name: "empty", URI: "http://4.4.4.4:80", Source: "free_proxy", Available: true, InitialCheckDone: true, SuccessCount: 3, FailureCount: 0, LastLatencyMs: 40},
	}
	got := SelectCandidates(snaps, nil, cfg)
	if len(got) != 1 || got[0].Name != "good" {
		t.Fatalf("got %#v", got)
	}
}

func TestPromotedNodeNameStable(t *testing.T) {
	a := PromotedNodeName("free-promoted-", "http://1.2.3.4:8080")
	b := PromotedNodeName("free-promoted-", "HTTP://1.2.3.4:8080")
	if a != b {
		t.Fatalf("unstable names %s vs %s", a, b)
	}
	if a[:len("free-promoted-")] != "free-promoted-" {
		t.Fatalf("prefix missing: %s", a)
	}
	if CountPromoted([]config.NodeConfig{{Name: a}}, "free-promoted-") != 1 {
		t.Fatal("count promoted")
	}
}
