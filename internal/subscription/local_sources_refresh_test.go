//go:build with_clash_api

package subscription

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"easy_proxies/internal/boxmgr"
	"easy_proxies/internal/config"
	"easy_proxies/internal/monitor"
)

func TestRefreshCommitsLatestLocalStateAndReportsErrors(t *testing.T) {
	addr := func() string {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer l.Close()
		return l.Addr().String()
	}
	_, port, _ := net.SplitHostPort(addr())
	p, _ := strconv.Atoi(port)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("socks5://127.0.0.1:2#sub\n")) }))
	defer source.Close()
	path := filepath.Join(t.TempDir(), "config.yaml")
	body, _ := json.Marshal(map[string]any{"mode": "pool", "listener": map[string]any{"address": "127.0.0.1", "port": p}, "management": map[string]any{"enabled": false, "clash_api_listen": addr(), "probe_target": "http://127.0.0.1:1"}, "geoip": map[string]any{"enabled": false}, "free_proxy_cache": map[string]any{"enabled": false}, "subscription_refresh": map[string]any{"health_check_timeout": "50ms"}, "nodes": []map[string]string{{"uri": "http://127.0.0.1:1", "name": "inline"}}})
	if e := os.WriteFile(path, body, 0600); e != nil {
		t.Fatal(e)
	}
	cfg, e := config.Load(path)
	if e != nil {
		t.Fatal(e)
	}
	cfg.Subscriptions = []string{source.URL}
	if e = cfg.SaveSubscriptionSettings(); e != nil {
		t.Fatal(e)
	}
	bm := boxmgr.New(cfg, monitor.Config{}, boxmgr.WithPortReleaseTimeout(time.Millisecond))
	if e = bm.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	defer bm.Close()
	if _, e = bm.SaveLocalSource(context.Background(), "", 0, "draft", true, "http://127.0.0.1:3"); e != nil {
		t.Fatal(e)
	}
	sm := New(cfg, bm)
	defer sm.Stop()
	sm.httpClient = source.Client()
	sm.doRefresh()
	if got := sm.Status(); got.LastError != "" || got.NodeCount != 1 || got.RefreshCount != 1 || got.IsRefreshing {
		t.Fatalf("successful refresh: %+v", got)
	}
	current := bm.ConfigSnapshot()
	if len(current.Nodes) != 2 || len(current.LocalSources) != 0 {
		t.Fatal("refresh lost inline or applied draft")
	}
	state, e := bm.LocalSources(context.Background())
	if e != nil || !state.Pending {
		t.Fatal("refresh discarded draft")
	}
	if sm.lastSubHash == "" || sm.lastNodesModTime.IsZero() {
		t.Fatal("successful cache commit not tracked")
	}
	// A stale fetch must fail before replacing runtime, even though data fetched.
	changed := *cfg
	changed.Subscriptions = []string{source.URL + "/stale"}
	sm.baseCfg = &changed
	sm.doRefresh()
	if sm.Status().LastError == "" {
		t.Fatal("stale source commit reported success")
	}
	if len(bm.ConfigSnapshot().Nodes) != 2 {
		t.Fatal("failed commit changed runtime")
	}
	// No cache masks an empty response or failed HTTP fetch.
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(503)
		}
	}))
	defer empty.Close()
	changed.NodesFile = filepath.Join(t.TempDir(), "absent.txt")
	changed.Subscriptions = []string{empty.URL}
	sm.baseCfg = &changed
	sm.httpClient = empty.Client()
	sm.doRefresh()
	if sm.Status().LastError != "no nodes fetched" {
		t.Fatalf("empty refresh: %s", sm.Status().LastError)
	}
	changed.Subscriptions = []string{empty.URL + "/fail"}
	sm.doRefresh()
	if sm.Status().LastError == "" {
		t.Fatal("fetch error suppressed")
	}
	sm.refreshMu.Lock()
	before := sm.Status().RefreshCount
	sm.doRefresh()
	sm.refreshMu.Unlock()
	if sm.Status().RefreshCount != before {
		t.Fatal("overlapping refresh ran")
	}
}
