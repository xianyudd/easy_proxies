package subscription

import (
	"testing"

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
