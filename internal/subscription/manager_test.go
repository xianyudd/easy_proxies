package subscription

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easy_proxies/internal/config"
)

func TestCreateNewConfigMarksSubscriptionNodes(t *testing.T) {
	mgr := &Manager{baseCfg: &config.Config{
		Mode:          "hybrid",
		MultiPort:     config.MultiPortConfig{BasePort: 30000, Username: "user", Password: "pass"},
		Subscriptions: []string{"http://127.0.0.1/sub"},
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
		Subscriptions: []string{"http://127.0.0.1/old"},
		SubscriptionRefresh: config.SubscriptionRefreshConfig{
			Enabled:  true,
			Interval: time.Hour,
		},
	}, nil)
	mgr.UpdateConfig([]string{"http://127.0.0.1/new"}, true, 2*time.Hour)
	defer mgr.Stop()

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

	nodes, stats := config.FetchSubscriptionNodes(context.Background(), []string{server.URL}, config.SubscriptionFetchOptions{
		Timeout:     time.Second,
		Concurrency: 1,
		Client:      server.Client(),
	})
	if len(nodes) != 0 {
		t.Fatalf("expected 0 nodes for oversized body, got %d", len(nodes))
	}
	if stats.Failed != 1 || stats.LastError == nil || !strings.Contains(stats.LastError.Error(), "exceeds") {
		t.Fatalf("stats=%+v, lastErr=%v; want a body-size failure", stats, stats.LastError)
	}
}

func TestFetchAllSubscriptionsFallsBackToCache(t *testing.T) {
	const cachedOne = "trojan://pw@cached1.example.com:443#cached1"
	const cachedTwo = "trojan://pw@cached2.example.com:443#cached2"

	emptyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(""))
	}))
	defer emptyServer.Close()

	partialServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/broken" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("trojan://pw@fresh.example.com:443#fresh\n"))
	}))
	defer partialServer.Close()

	cases := []struct {
		name  string
		urls  []string
		nodes int // 期望回退后拿到的缓存节点数
	}{
		{"all empty uses cache", []string{emptyServer.URL + "/empty"}, 2},
		{"partial failure regression uses cache", []string{partialServer.URL + "/ok", partialServer.URL + "/broken"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			nodesFile := filepath.Join(dir, "nodes.txt")
			if err := os.WriteFile(nodesFile, []byte(cachedOne+"\n"+cachedTwo+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			mgr := New(&config.Config{
				NodesFile:     nodesFile,
				Subscriptions: tc.urls,
				SubscriptionRefresh: config.SubscriptionRefreshConfig{
					Timeout: 5 * time.Second,
				},
			}, nil)
			defer mgr.Stop()
			mgr.httpClient = emptyServer.Client()

			nodes, err := mgr.fetchAllSubscriptions()
			if err != nil {
				t.Fatalf("fetchAllSubscriptions error: %v", err)
			}
			if len(nodes) != tc.nodes {
				t.Fatalf("nodes=%d, want %d cached nodes: %#v", len(nodes), tc.nodes, nodes)
			}
		})
	}
}

func TestRefreshLoopUsesGenerationScopedContextAndManualChannel(t *testing.T) {
	source, err := os.ReadFile("manager.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "func (m *Manager) refreshLoop(ctx context.Context, manualRefresh <-chan struct{}, interval time.Duration)") {
		t.Fatalf("refreshLoop must accept generation-scoped ctx and manualRefresh channel")
	}
	start := strings.Index(text, "func (m *Manager) refreshLoop(")
	end := strings.Index(text[start:], "// doRefresh performs")
	if start < 0 || end < 0 {
		t.Fatalf("refreshLoop body not found")
	}
	body := text[start : start+end]
	if strings.Contains(body, "<-m.ctx.Done()") || strings.Contains(body, "<-m.manualRefresh") {
		t.Fatalf("refreshLoop should not read mutable manager ctx/manualRefresh fields directly")
	}
}
