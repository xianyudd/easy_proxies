//go:build with_clash_api

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"easy_proxies/internal/boxmgr"
	"easy_proxies/internal/config"
	"easy_proxies/internal/monitor"
	"easy_proxies/internal/nodesource"
)

// Exercise the actual startup background wiring, without replacing timers or
// manager interfaces. Synthetic proxy accepts only a loopback HTTP probe.
func TestStartupFreeRefreshPreservesDraft(t *testing.T) {
	for _, filter := range []bool{false, true} {
		t.Run(strconv.FormatBool(filter), func(t *testing.T) {
			cfg, bm := freeRefreshFixture(t, filter)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if e := bm.Start(ctx); e != nil {
				t.Fatal(e)
			}
			defer bm.Close()
			if _, e := bm.SaveLocalSource(ctx, "", 0, "draft", true, "http://127.0.0.1:3"); e != nil {
				t.Fatal(e)
			}
			startFreeProxyCacheRefresh(ctx, cfg, bm)
			deadline := time.Now().Add(8 * time.Second)
			for time.Now().Before(deadline) {
				current := bm.ConfigSnapshot()
				if len(current.Nodes) == 2 {
					if len(current.LocalSources) != 0 {
						t.Fatal("background refresh applied draft")
					}
					state, e := bm.LocalSources(ctx)
					if e != nil || !state.Pending {
						t.Fatal("background refresh lost draft")
					}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("startup free source not committed")
		})
	}
}

func TestStartupFreeRefreshReportsReloadFailure(t *testing.T) {
	for _, filter := range []bool{false, true} {
		t.Run(strconv.FormatBool(filter), func(t *testing.T) {
			cfg, bm := freeRefreshFixture(t, filter)
			// Manager is deliberately not started; the real source fetch succeeds but
			// commit must reject and the background caller must report the error.
			read, write, e := os.Pipe()
			if e != nil {
				t.Fatal(e)
			}
			old := os.Stderr
			os.Stderr = write
			defer func() { os.Stderr = old; write.Close(); read.Close() }()
			lines := make(chan string, 8)
			go func() {
				scanner := bufio.NewScanner(read)
				for scanner.Scan() {
					lines <- scanner.Text()
				}
				close(lines)
			}()
			startFreeProxyCacheRefresh(context.Background(), cfg, bm)
			select {
			case line := <-lines:
				if !strings.Contains(line, "reload failed") {
					t.Fatalf("unexpected error: %s", line)
				}
			case <-time.After(8 * time.Second):
				t.Fatal("reload failure not reported")
			}
		})
	}
}

func freeRefreshFixture(t *testing.T, filter bool) (*config.Config, *boxmgr.Manager) {
	t.Helper()
	addr := func() string {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		defer l.Close()
		return l.Addr().String()
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	t.Cleanup(proxy.Close)
	_, port, _ := net.SplitHostPort(addr())
	p, _ := strconv.Atoi(port)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body, _ := json.Marshal(map[string]any{"mode": "pool", "listener": map[string]any{"address": "127.0.0.1", "port": p}, "management": map[string]any{"enabled": false, "clash_api_listen": addr(), "probe_target": "http://127.0.0.1:1"}, "geoip": map[string]any{"enabled": false}, "free_proxy_cache": map[string]any{"enabled": false}, "subscription_refresh": map[string]any{"health_check_timeout": "50ms"}, "nodes": []map[string]string{{"uri": "http://127.0.0.1:1", "name": "inline"}}})
	if e := os.WriteFile(path, body, 0600); e != nil {
		t.Fatal(e)
	}
	cfg, e := config.Load(path)
	if e != nil {
		t.Fatal(e)
	}
	yes, no := true, false
	cfg.FreeProxySources = []nodesource.SourceConfig{{Name: "synthetic", File: filepath.Join(dir, "source.txt"), Format: "txt"}}
	if e = os.WriteFile(cfg.FreeProxySources[0].File, []byte(proxy.URL+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	cfg.FreeProxyFilter = nodesource.FilterConfig{Enabled: filter, Timeout: time.Second, Workers: 1, Probes: nodesource.FilterProbes{HTTP: "http://127.0.0.1:1", HTTPS: "http://127.0.0.1:1"}}
	cfg.FreeProxyCache = config.FreeProxyCacheConfig{Enabled: &yes, RefreshOnStart: &yes, AutoReload: &yes, Path: filepath.Join(dir, "cache.txt")}
	if filter {
		// Only the startup filter-reload goroutine is scheduled in this branch.
		cfg.FreeProxyCache.RefreshOnStart = &no
		if e = os.WriteFile(cfg.FreeProxyCache.Path, []byte(proxy.URL+"\n"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	return cfg, boxmgr.New(cfg, monitor.Config{}, boxmgr.WithPortReleaseTimeout(time.Millisecond))
}
