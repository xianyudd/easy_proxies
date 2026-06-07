package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"easy_proxies/internal/config"
)

type fakeNodeManager struct {
	delay       time.Duration
	err         error
	reloadCalls int
	done        chan struct{}
}

func (f *fakeNodeManager) ListConfigNodes(ctx context.Context) ([]config.NodeConfig, error) {
	return nil, nil
}

func (f *fakeNodeManager) CreateNode(ctx context.Context, node config.NodeConfig) (config.NodeConfig, error) {
	return node, nil
}

func (f *fakeNodeManager) UpdateNode(ctx context.Context, name string, node config.NodeConfig) (config.NodeConfig, error) {
	return node, nil
}

func (f *fakeNodeManager) DeleteNode(ctx context.Context, name string) error {
	return nil
}

func (f *fakeNodeManager) TriggerReload(ctx context.Context) error {
	f.reloadCalls++
	if f.done != nil {
		defer close(f.done)
	}
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

func TestHandleSettingsPersistsFreeProxyConfig(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
free_proxy_max_nodes: 10
free_proxy_filter:
  enabled: false
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg}

	body := []byte(`{
		"free_proxy_sources": [
			{"name":"github-http","url":"https://example.test/proxies.txt","default_scheme":"http","format":"text","enabled":true,"max_nodes":321}
		],
		"free_proxy_max_nodes": 123,
		"free_proxy_filter": {
			"enabled": true,
			"min_tier": "simple_web",
			"workers": 222,
			"timeout": "1500ms",
			"max_candidates": 4567,
			"probes": {"http":"http://cp.cloudflare.com/generate_204","https":"https://example.com/"}
		}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		NeedReload bool `json:"need_reload"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.NeedReload {
		t.Fatalf("expected need_reload response, got %s", rec.Body.String())
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.FreeProxyMaxNodes != 123 || len(reloaded.FreeProxySources) != 1 {
		t.Fatalf("free proxy config not persisted: %#v", reloaded)
	}
	source := reloaded.FreeProxySources[0]
	if source.Name != "github-http" || source.URL != "https://example.test/proxies.txt" || source.DefaultScheme != "http" || source.MaxNodes != 321 || !source.EnabledValue() {
		t.Fatalf("unexpected source: %#v", source)
	}
	filter := reloaded.FreeProxyFilter
	if !filter.Enabled || filter.MinTier != "simple_web" || filter.Workers != 222 || filter.Timeout.String() != "1.5s" || filter.MaxCandidates != 4567 {
		t.Fatalf("unexpected filter: %#v", filter)
	}
}

func TestHandleSettingsReturnsFreeProxyConfig(t *testing.T) {
	cfg := &config.Config{FreeProxyMaxNodes: 88}
	cfg.FreeProxyFilter.Enabled = true
	cfg.FreeProxyFilter.MinTier = "simple_web"
	cfg.FreeProxyFilter.Workers = 120
	cfg.FreeProxyFilter.MaxCandidates = 3000
	cfg.FreeProxyFilter.Probes.HTTP = "http://cp.cloudflare.com/generate_204"
	cfg.FreeProxyFilter.Probes.HTTPS = "https://example.com/"
	server := &Server{cfgSrc: cfg}

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec := httptest.NewRecorder()

	server.handleSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		FreeProxyMaxNodes int `json:"free_proxy_max_nodes"`
		FreeProxyFilter   struct {
			Enabled       bool   `json:"enabled"`
			MinTier       string `json:"min_tier"`
			Workers       int    `json:"workers"`
			MaxCandidates int    `json:"max_candidates"`
		} `json:"free_proxy_filter"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.FreeProxyMaxNodes != 88 || !resp.FreeProxyFilter.Enabled || resp.FreeProxyFilter.MinTier != "simple_web" || resp.FreeProxyFilter.Workers != 120 || resp.FreeProxyFilter.MaxCandidates != 3000 {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestHandleSettingsAcceptsRoundTripFreeProxyDurations(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
free_proxy_sources:
  - name: local-free
    file: /tmp/free.txt
    timeout: 30s
    enabled: true
free_proxy_cache:
  enabled: true
  path: .cache/free-proxies.txt
  refresh_on_start: false
  auto_reload: true
  workers: 4
  max_age: 6h
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg}

	getReq := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	getRec := httptest.NewRecorder()
	server.handleSettings(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", getRec.Code, getRec.Body.String())
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(getRec.Body.Bytes()))
	putRec := httptest.NewRecorder()
	server.handleSettings(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("round-trip PUT status = %d, body = %s", putRec.Code, putRec.Body.String())
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.FreeProxySources) != 1 {
		t.Fatalf("expected one free proxy source, got %#v", reloaded.FreeProxySources)
	}
	if got := reloaded.FreeProxySources[0].Timeout.String(); got != "30s" {
		t.Fatalf("timeout = %s, want 30s", got)
	}
}

func TestHandleSettingsSkipsReloadForControlPlaneOnlyChanges(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
management:
  listen: 127.0.0.1:9091
  password: ""
  probe_target: http://cp.cloudflare.com/generate_204
log:
  output: stdout
  max_size: 100
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeNodeManager{}
	server := &Server{cfgSrc: cfg, nodeMgr: fake, reloadState: "idle"}

	body := []byte(`{
		"external_ip": "1.2.3.4",
		"probe_target": "http://example.test/generate_204",
		"management": {"listen":"127.0.0.1:9091","password":"secret"},
		"log": {"output":"stdout","max_size":200,"max_backups":2,"max_age":3,"compress":false}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		NeedReload    bool `json:"need_reload"`
		ReloadStarted bool `json:"reload_started"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.NeedReload || resp.ReloadStarted || fake.reloadCalls != 0 {
		t.Fatalf("control-plane-only settings should not reload: resp=%#v calls=%d", resp, fake.reloadCalls)
	}
}

func TestHandleSettingsStartsCoreReloadAsynchronously(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`mode: hybrid
listener:
  address: 127.0.0.1
  port: 18080
multi_port:
  address: 127.0.0.1
  base_port: 25000
nodes:
  - name: base
    uri: http://127.0.0.1:18080
management:
  listen: 127.0.0.1:9091
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeNodeManager{delay: 250 * time.Millisecond, done: make(chan struct{})}
	server := &Server{cfgSrc: cfg, nodeMgr: fake, reloadState: "idle"}

	body := []byte(`{
		"mode": "hybrid",
		"listener": {"address":"127.0.0.1","port":18081,"username":"","password":""},
		"multi_port": {"address":"127.0.0.1","base_port":25000,"username":"","password":""},
		"management": {"listen":"127.0.0.1:9091","password":""}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	start := time.Now()
	server.handleSettings(rec, req)
	elapsed := time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if elapsed >= fake.delay {
		t.Fatalf("settings save waited for reload: elapsed=%s delay=%s", elapsed, fake.delay)
	}
	var resp struct {
		NeedReload    bool `json:"need_reload"`
		ReloadStarted bool `json:"reload_started"`
		ReloadStatus  struct {
			State string `json:"state"`
		} `json:"reload_status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.NeedReload || !resp.ReloadStarted || resp.ReloadStatus.State != "running" {
		t.Fatalf("expected async reload response, got %#v body=%s", resp, rec.Body.String())
	}
	deadline := time.After(2 * time.Second)
	for {
		status := server.currentReloadStatus()
		if status.State == "succeeded" {
			break
		}
		if status.State == "failed" {
			t.Fatalf("reload failed: %#v", status)
		}
		select {
		case <-deadline:
			t.Fatalf("async reload did not finish, status=%#v", status)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if fake.reloadCalls != 1 {
		t.Fatalf("reloadCalls = %d, want 1", fake.reloadCalls)
	}
}

func TestHandleSettingsStartsFreeProxyRefreshAsynchronously(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	sourcePath := filepath.Join(tmp, "free.txt")
	cachePath := filepath.Join(tmp, "cache.txt")
	if err := os.WriteFile(sourcePath, []byte("http://127.0.0.1:18080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
free_proxy_cache:
  enabled: true
  path: ` + cachePath + `
  auto_reload: true
free_proxy_filter:
  enabled: false
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeNodeManager{done: make(chan struct{})}
	server := &Server{cfgSrc: cfg, nodeMgr: fake, reloadState: "idle"}

	body := []byte(`{
		"free_proxy_sources": [
			{"name":"local-test","file":"` + sourcePath + `","default_scheme":"http","format":"txt","enabled":true,"max_nodes":0}
		],
		"free_proxy_cache": {"enabled":true,"path":"` + cachePath + `","auto_reload":true,"workers":1,"max_age":"6h"},
		"free_proxy_filter": {"enabled":false,"min_tier":"http_basic","workers":1,"timeout":"100ms","max_candidates":0}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		NeedReload              bool `json:"need_reload"`
		ReloadStarted           bool `json:"reload_started"`
		FreeProxyRefreshNeeded  bool `json:"free_proxy_refresh_needed"`
		FreeProxyRefreshStarted bool `json:"free_proxy_refresh_started"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.NeedReload || resp.ReloadStarted || !resp.FreeProxyRefreshNeeded || !resp.FreeProxyRefreshStarted {
		t.Fatalf("unexpected response: %#v body=%s", resp, rec.Body.String())
	}

	deadline := time.After(2 * time.Second)
	for {
		status := server.currentFreeProxyRefreshStatus()
		if status.State == "succeeded" {
			if status.Accepted != 1 || !status.ReloadStarted {
				t.Fatalf("unexpected refresh status: %#v", status)
			}
			break
		}
		if status.State == "failed" {
			t.Fatalf("refresh failed: %#v", status)
		}
		select {
		case <-deadline:
			t.Fatalf("refresh did not finish, status=%#v", status)
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case <-fake.done:
	case <-time.After(2 * time.Second):
		t.Fatal("auto reload did not run after free proxy refresh")
	}
	if fake.reloadCalls != 1 {
		t.Fatalf("reloadCalls = %d, want 1", fake.reloadCalls)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "http://127.0.0.1:18080\n" {
		t.Fatalf("cache content = %q", string(data))
	}
}

func TestHandleReloadStatusReportsAsyncFailure(t *testing.T) {
	fake := &fakeNodeManager{err: errors.New("boom"), done: make(chan struct{})}
	server := &Server{nodeMgr: fake, reloadState: "idle"}
	if _, started, err := server.startAsyncReload("test"); err != nil || !started {
		t.Fatalf("startAsyncReload started=%v err=%v", started, err)
	}
	select {
	case <-fake.done:
	case <-time.After(2 * time.Second):
		t.Fatal("async reload did not finish")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/reload/status", nil)
	rec := httptest.NewRecorder()
	server.handleReloadStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp reloadStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.State != "failed" || resp.Error != "boom" {
		t.Fatalf("unexpected status: %#v", resp)
	}
}

func TestHandleSettingsHotRebindsManagementListen(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	if srv == nil {
		t.Fatal("server is nil")
	}
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(configPath, []byte("nodes:\n  - name: base\n    uri: http://127.0.0.1:18080\nmanagement:\n  listen: 127.0.0.1:0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetConfig(cfg)
	if err := srv.startHTTPServer("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	oldListen := srv.cfg.Listen
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	body := []byte(`{"management":{"listen":"127.0.0.1:0","password":""}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	req.Host = oldListen
	rec := httptest.NewRecorder()
	srv.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ManagementRebound bool   `json:"management_rebound"`
		ManagementListen  string `json:"management_listen"`
		ManagementURLHint string `json:"management_url_hint"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.ManagementRebound || resp.ManagementListen == "" || resp.ManagementListen == oldListen || resp.ManagementURLHint == "" {
		t.Fatalf("unexpected rebind response: %#v old=%s", resp, oldListen)
	}

	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Get("http://" + resp.ManagementListen + "/api/settings")
	if err != nil {
		t.Fatalf("new listen not reachable: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("new listen status = %d", res.StatusCode)
	}
}
