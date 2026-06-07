package subscription

import (
	"testing"
	"time"

	"easy_proxies/internal/config"
)

func TestCreateNewConfigMarksSubscriptionNodes(t *testing.T) {
	mgr := &Manager{baseCfg: &config.Config{
		Mode:          "hybrid",
		MultiPort:     config.MultiPortConfig{BasePort: 30000, Username: "user", Password: "pass"},
		Subscriptions: []string{"https://example.test/sub"},
	}}

	cfg := mgr.createNewConfig([]config.NodeConfig{{URI: "http://127.0.0.1:8080"}})
	if len(cfg.Nodes) != 1 {
		t.Fatalf("nodes len=%d, want 1", len(cfg.Nodes))
	}
	if cfg.Nodes[0].Source != config.NodeSourceSubscription {
		t.Fatalf("source=%q, want %q", cfg.Nodes[0].Source, config.NodeSourceSubscription)
	}
}

func TestUpdateConfigDoesNotTriggerImmediateRefresh(t *testing.T) {
	mgr := New(&config.Config{
		Subscriptions: []string{"https://example.test/old"},
		SubscriptionRefresh: config.SubscriptionRefreshConfig{
			Enabled:  true,
			Interval: time.Hour,
		},
	}, nil)
	mgr.UpdateConfig([]string{"https://example.test/new"}, true, 2*time.Hour)
	defer mgr.Stop()

	time.Sleep(50 * time.Millisecond)
	status := mgr.Status()
	if status.RefreshCount != 0 || status.IsRefreshing {
		t.Fatalf("UpdateConfig should not trigger immediate refresh, got %#v", status)
	}
	if mgr.baseCfg.SubscriptionRefresh.Interval != 2*time.Hour {
		t.Fatalf("interval=%s, want 2h", mgr.baseCfg.SubscriptionRefresh.Interval)
	}
}
