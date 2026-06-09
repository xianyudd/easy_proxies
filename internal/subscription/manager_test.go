package subscription

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestFetchSubscriptionRejectsBodyLargerThanLimit(t *testing.T) {
	const validPrefix = "socks5://127.0.0.1:1080#valid\n"
	const maxBodySize = 10 * 1024 * 1024
	oversized := append([]byte(validPrefix), bytes.Repeat([]byte("x"), maxBodySize-len(validPrefix)+1)...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(oversized)
	}))
	defer server.Close()

	mgr := New(&config.Config{}, nil)
	defer mgr.Stop()

	nodes, err := mgr.fetchSubscription(server.URL, time.Second)
	if err == nil {
		t.Fatalf("fetchSubscription returned nil error for oversized body, nodes=%d", len(nodes))
	}
	if !strings.Contains(err.Error(), "body too large") {
		t.Fatalf("error=%v, want body too large", err)
	}
}
