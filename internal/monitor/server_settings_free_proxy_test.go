package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"easy_proxies/internal/config"
)

type fakeNodeManager struct {
	mu          sync.Mutex
	delay       time.Duration
	err         error
	reloadCalls int
	done        chan struct{}
	doneOnce    sync.Once
	reloadCh    chan int
	nodes       []config.NodeConfig
	created     []config.NodeConfig
	updated     []config.NodeConfig
	updatedName string
	deleted     []string
}

func freeLocalListen(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate local listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close local listen: %v", err)
	}
	return addr
}

func (f *fakeNodeManager) ListConfigNodes(ctx context.Context) ([]config.NodeConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]config.NodeConfig, len(f.nodes))
	copy(out, f.nodes)
	return out, nil
}

func (f *fakeNodeManager) CreateNode(ctx context.Context, node config.NodeConfig) (config.NodeConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, node)
	f.nodes = append(f.nodes, node)
	return node, nil
}

func (f *fakeNodeManager) UpdateNode(ctx context.Context, name string, node config.NodeConfig) (config.NodeConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updatedName = name
	f.updated = append(f.updated, node)
	for i, existing := range f.nodes {
		if existing.Name == name {
			f.nodes[i] = node
			return node, nil
		}
	}
	f.nodes = append(f.nodes, node)
	return node, nil
}

func (f *fakeNodeManager) DeleteNode(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, name)
	filtered := f.nodes[:0]
	for _, node := range f.nodes {
		if node.Name != name {
			filtered = append(filtered, node)
		}
	}
	f.nodes = filtered
	return nil
}

func (f *fakeNodeManager) TriggerReload(ctx context.Context) error {
	f.mu.Lock()
	f.reloadCalls++
	call := f.reloadCalls
	ch := f.reloadCh
	f.mu.Unlock()
	if ch != nil {
		select {
		case ch <- call:
		default:
		}
	}
	if f.done != nil {
		defer f.doneOnce.Do(func() { close(f.done) })
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

func (f *fakeNodeManager) ReloadCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reloadCalls
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
			"max_probe_candidates": 12000,
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
		NeedReload             bool `json:"need_reload"`
		FreeProxyRefreshNeeded bool `json:"free_proxy_refresh_needed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.NeedReload || !resp.FreeProxyRefreshNeeded {
		t.Fatalf("free proxy config should request refresh instead of immediate reload, got %#v body=%s", resp, rec.Body.String())
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
	if !filter.Enabled || filter.MinTier != "simple_web" || filter.Workers != 222 || filter.Timeout.String() != "1.5s" || filter.MaxCandidates != 4567 || filter.MaxProbeCandidates != 12000 {
		t.Fatalf("unexpected filter: %#v", filter)
	}
}

func TestHandleSettingsUpdatesCloudflareRuntimeConfig(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	listen := freeLocalListen(t)
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
management:
  listen: ` + listen + `
quality_check:
  enabled: false
  interval: 1h
  region: all
  count: 500
  include_unavailable: true
  retry_failed: false
  cloudflare_timeout: 5s
  cloudflare_concurrency: 24
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	server := NewServer(Config{Enabled: true, Listen: listen}, mgr, nil)
	server.SetConfig(cfg)

	body := []byte(`{
		"management": {"listen":"` + listen + `","password":""},
		"quality_check": {
			"enabled": false,
			"interval": "1h",
			"region": "all",
			"count": 500,
			"include_unavailable": true,
			"retry_failed": false,
			"cloudflare_timeout": "3s",
			"cloudflare_concurrency": 32
		}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	timeout, concurrency := server.cfChecker.Settings()
	if timeout != 3*time.Second || concurrency != 32 {
		t.Fatalf("cloudflare runtime settings = %s/%d, want 3s/32", timeout, concurrency)
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	q := reloaded.QualityCheck.Normalized()
	if q.CloudflareTimeout != 3*time.Second || q.CloudflareConcurrency != 32 {
		t.Fatalf("persisted cloudflare settings = %s/%d", q.CloudflareTimeout, q.CloudflareConcurrency)
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
			Enabled            bool   `json:"enabled"`
			MinTier            string `json:"min_tier"`
			Workers            int    `json:"workers"`
			MaxCandidates      int    `json:"max_candidates"`
			MaxProbeCandidates int    `json:"max_probe_candidates"`
		} `json:"free_proxy_filter"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.FreeProxyMaxNodes != 88 || !resp.FreeProxyFilter.Enabled || resp.FreeProxyFilter.MinTier != "simple_web" || resp.FreeProxyFilter.Workers != 120 || resp.FreeProxyFilter.MaxCandidates != 3000 || resp.FreeProxyFilter.MaxProbeCandidates != 0 {
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
	listen := freeLocalListen(t)
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
management:
  listen: ` + listen + `
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
		"management": {"listen":"` + listen + `","password":"secret"},
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
	if resp.NeedReload || resp.ReloadStarted || fake.ReloadCalls() != 0 {
		t.Fatalf("control-plane-only settings should not reload: resp=%#v calls=%d", resp, fake.ReloadCalls())
	}
}

func TestHandleSettingsStartsCoreReloadAsynchronously(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	listen := freeLocalListen(t)
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
  listen: ` + listen + `
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeNodeManager{delay: 2 * time.Second, done: make(chan struct{})}
	server := &Server{cfgSrc: cfg, nodeMgr: fake, reloadState: "idle"}

	body := []byte(`{
		"mode": "hybrid",
		"listener": {"address":"127.0.0.1","port":18081,"username":"","password":""},
		"multi_port": {"address":"127.0.0.1","base_port":25000,"username":"","password":""},
		"management": {"listen":"` + listen + `","password":""}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	start := time.Now()
	server.handleSettings(rec, req)
	elapsed := time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if elapsed >= 500*time.Millisecond {
		t.Fatalf("settings save waited too long for async reload start: elapsed=%s delay=%s", elapsed, fake.delay)
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
	deadline := time.After(4 * time.Second)
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
	if fake.ReloadCalls() != 1 {
		t.Fatalf("reloadCalls = %d, want 1", fake.ReloadCalls())
	}
}

func TestCurrentReloadStatusReportsElapsedWhileRunning(t *testing.T) {
	started := time.Now().Add(-150 * time.Millisecond)
	server := &Server{
		reloadState: "running",
		reloadStatus: reloadStatus{
			State:     "running",
			StartedAt: started,
		},
	}

	status := server.currentReloadStatus()
	if status.State != "running" {
		t.Fatalf("state=%q, want running", status.State)
	}
	if status.ElapsedMS <= 0 {
		t.Fatalf("elapsed_ms=%d, want positive elapsed duration", status.ElapsedMS)
	}
	if status.DurationMS != 0 {
		t.Fatalf("duration_ms=%d, want 0 while running", status.DurationMS)
	}
}

func TestHandleSettingsQueuesReloadWhenSaveArrivesDuringReload(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	listen := freeLocalListen(t)
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
mode: hybrid
listener:
  address: 127.0.0.1
  port: 18080
multi_port:
  address: 127.0.0.1
  base_port: 25000
management:
  listen: ` + listen + `
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeNodeManager{delay: 120 * time.Millisecond, reloadCh: make(chan int, 4)}
	server := &Server{cfgSrc: cfg, nodeMgr: fake, reloadState: "idle"}

	body1 := []byte(`{
		"mode": "hybrid",
		"listener": {"address":"127.0.0.1","port":18081,"username":"u1","password":""},
		"multi_port": {"address":"127.0.0.1","base_port":25000,"username":"","password":""},
		"management": {"listen":"` + listen + `","password":""}
	}`)
	req1 := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body1))
	rec1 := httptest.NewRecorder()
	server.handleSettings(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first status = %d, body = %s", rec1.Code, rec1.Body.String())
	}
	select {
	case <-fake.reloadCh:
	case <-time.After(time.Second):
		t.Fatal("first reload did not start")
	}

	body2 := []byte(`{
		"mode": "hybrid",
		"listener": {"address":"127.0.0.1","port":18082,"username":"u2","password":""},
		"multi_port": {"address":"127.0.0.1","base_port":25000,"username":"","password":""},
		"management": {"listen":"` + listen + `","password":""}
	}`)
	req2 := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body2))
	rec2 := httptest.NewRecorder()
	server.handleSettings(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second status = %d, body = %s", rec2.Code, rec2.Body.String())
	}
	var resp struct {
		NeedReload    bool `json:"need_reload"`
		ReloadStarted bool `json:"reload_started"`
		ReloadStatus  struct {
			State   string `json:"state"`
			Pending bool   `json:"reload_pending"`
		} `json:"reload_status"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.NeedReload || resp.ReloadStarted || resp.ReloadStatus.State != "running" || !resp.ReloadStatus.Pending {
		t.Fatalf("expected queued reload response, got %#v body=%s", resp, rec2.Body.String())
	}
	select {
	case <-fake.reloadCh:
	case <-time.After(2 * time.Second):
		t.Fatal("queued reload did not start")
	}
	deadline := time.After(3 * time.Second)
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
			t.Fatalf("queued reload did not finish, status=%#v calls=%d", status, fake.ReloadCalls())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if fake.ReloadCalls() != 2 {
		t.Fatalf("reloadCalls = %d, want 2", fake.ReloadCalls())
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Listener.Port != 18082 || reloaded.Listener.Username != "u2" {
		t.Fatalf("latest settings not persisted: %#v", reloaded.Listener)
	}
}

func TestHandleSettingsReportsReloadErrorWhenNodeManagerMissing(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	listen := freeLocalListen(t)
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
management:
  listen: ` + listen + `
listener:
  address: 127.0.0.1
  port: 18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg, reloadState: "idle"}

	body := []byte(`{
		"listener": {"address":"127.0.0.1","port":18081,"username":"","password":""},
		"management": {"listen":"` + listen + `","password":""}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		NeedReload    bool   `json:"need_reload"`
		ReloadStarted bool   `json:"reload_started"`
		ReloadError   string `json:"reload_error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.NeedReload || resp.ReloadStarted || resp.ReloadError == "" {
		t.Fatalf("expected explicit reload error, got %#v body=%s", resp, rec.Body.String())
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
	if resp.NeedReload || resp.ReloadStarted || !resp.FreeProxyRefreshNeeded || !resp.FreeProxyRefreshStarted {
		t.Fatalf("free proxy settings should be handled by refresh instead of immediate reload: %#v body=%s", resp, rec.Body.String())
	}

	deadline := time.After(2 * time.Second)
	for {
		status := server.currentFreeProxyRefreshStatus()
		if status.State == "succeeded" {
			if status.Accepted != 1 || !status.ReloadStarted || status.ReloadStatus == nil || status.ReloadStatus.State == "" {
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
	if fake.ReloadCalls() != 1 {
		t.Fatalf("reloadCalls = %d, want 1", fake.ReloadCalls())
	}
	status := server.currentFreeProxyRefreshStatus()
	if status.ReloadStatus == nil || status.ReloadStatus.State != "succeeded" {
		t.Fatalf("refresh status should include completed reload status, got %#v", status)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "http://127.0.0.1:18080\n" {
		t.Fatalf("cache content = %q", string(data))
	}
}

func TestFreeProxyRefreshStatusIncludesPerSourceFailureDetails(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	cachePath := filepath.Join(tmp, "cache.txt")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
free_proxy_cache:
  enabled: true
  path: ` + cachePath + `
  auto_reload: true
free_proxy_filter:
  enabled: false
free_proxy_sources:
  - name: missing-source
    file: ` + filepath.Join(tmp, "missing.txt") + `
    format: txt
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
	if _, started, err := server.startFreeProxyRefresh("test"); err != nil || !started {
		t.Fatalf("startFreeProxyRefresh started=%v err=%v", started, err)
	}
	deadline := time.After(2 * time.Second)
	for {
		status := server.currentFreeProxyRefreshStatus()
		if status.State == "failed" {
			if status.Accepted != 0 || len(status.Sources) != 1 || status.ReloadStarted {
				t.Fatalf("unexpected failed status: %#v", status)
			}
			if status.Sources[0].Name != "missing-source" || status.Sources[0].Error == "" {
				t.Fatalf("missing source failure details: %#v", status.Sources)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("refresh did not fail, status=%#v", status)
		case <-time.After(10 * time.Millisecond):
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/free-proxy/refresh/status", nil)
	rec := httptest.NewRecorder()
	server.handleFreeProxyRefreshStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp freeProxyRefreshStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Sources) != 1 || resp.Sources[0].Error == "" {
		t.Fatalf("API did not include source error details: %#v", resp)
	}
	select {
	case <-fake.done:
		t.Fatal("auto reload should not run after failed refresh; stale cache must be preserved")
	case <-time.After(100 * time.Millisecond):
	}
	if fake.ReloadCalls() != 0 {
		t.Fatalf("reloadCalls = %d, want 0", fake.ReloadCalls())
	}
}

func TestFreeProxyRefreshUsesConfigSnapshotFromStart(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	cachePath := filepath.Join(tmp, "cache.txt")
	requested := make(chan struct{})
	release := make(chan struct{})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requested)
		<-release
		_, _ = w.Write([]byte("http://127.0.0.1:18080\nhttp://127.0.0.1:18081\n"))
	}))
	defer source.Close()
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
free_proxy_cache:
  enabled: true
  path: ` + cachePath + `
  auto_reload: false
free_proxy_filter:
  enabled: false
free_proxy_max_nodes: 2
free_proxy_sources:
  - name: slow-source
    url: ` + source.URL + `
    format: txt
    timeout: 2s
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg, nodeMgr: &fakeNodeManager{done: make(chan struct{})}, reloadState: "idle"}
	if _, started, err := server.startFreeProxyRefresh("test"); err != nil || !started {
		t.Fatalf("startFreeProxyRefresh started=%v err=%v", started, err)
	}
	select {
	case <-requested:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not request slow source")
	}
	server.cfgMu.Lock()
	server.cfgSrc.FreeProxyMaxNodes = 1
	server.cfgMu.Unlock()
	close(release)

	deadline := time.After(2 * time.Second)
	for {
		status := server.currentFreeProxyRefreshStatus()
		if status.State == "succeeded" {
			if status.Accepted != 2 {
				t.Fatalf("refresh should use start-time config snapshot and accept 2 nodes, got %#v", status)
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
}

func TestHandleSettingsReportsNoImmediateReloadWhenFreshFreeProxyCacheIsReused(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	cachePath := filepath.Join(tmp, "cache.txt")
	if err := os.WriteFile(cachePath, []byte("http://9.9.9.9:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
free_proxy_cache:
  enabled: true
  path: ` + cachePath + `
  auto_reload: true
  max_age: 1h
free_proxy_filter:
  enabled: false
  min_tier: http_basic
  workers: 1
  timeout: 100ms
free_proxy_sources:
  - name: remote
    url: http://127.0.0.1:1/missing.txt
    format: txt
    timeout: 1s
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
			{"name":"remote","url":"http://127.0.0.1:1/missing.txt","format":"txt","enabled":true,"timeout":"1s"}
		],
		"free_proxy_cache": {"enabled":true,"path":"` + cachePath + `","auto_reload":true,"workers":1,"max_age":"1h"},
		"free_proxy_filter": {"enabled":false,"min_tier":"http_basic","workers":1,"timeout":"200ms","max_candidates":0}
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
	if resp.NeedReload || resp.ReloadStarted || !resp.FreeProxyRefreshNeeded || !resp.FreeProxyRefreshStarted {
		t.Fatalf("unexpected response: %#v body=%s", resp, rec.Body.String())
	}
	deadline := time.After(500 * time.Millisecond)
	for {
		status := server.currentFreeProxyRefreshStatus()
		if status.State == "succeeded" {
			if status.Accepted != 1 || status.CacheUpdated || status.ReloadStarted {
				t.Fatalf("fresh cache reuse should not reload: %#v", status)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("refresh did not finish quickly, status=%#v", status)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestHandleFreeProxyRefreshStatusSerializesCacheReuseFields(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	cachePath := filepath.Join(tmp, "cache.txt")
	if err := os.WriteFile(cachePath, []byte("http://9.9.9.9:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("fresh cache refresh should not fetch remote source")
	}))
	defer slow.Close()
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
free_proxy_cache:
  enabled: true
  path: ` + cachePath + `
  auto_reload: true
  max_age: 1h
free_proxy_filter:
  enabled: false
free_proxy_sources:
  - name: remote-source
    url: ` + slow.URL + `
    format: txt
    timeout: 1s
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg, nodeMgr: &fakeNodeManager{done: make(chan struct{})}, reloadState: "idle"}
	if _, started, err := server.startFreeProxyRefresh("test"); err != nil || !started {
		t.Fatalf("startFreeProxyRefresh started=%v err=%v", started, err)
	}
	deadline := time.After(500 * time.Millisecond)
	for {
		if status := server.currentFreeProxyRefreshStatus(); status.State == "succeeded" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("refresh did not finish quickly")
		case <-time.After(10 * time.Millisecond):
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/free-proxy/refresh/status", nil)
	server.handleFreeProxyRefreshStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status code=%d body=%s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"duration_ms", "cache_updated", "reload_started", "accepted"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("expected %s to be serialized for cache reuse status, body=%s", key, rec.Body.String())
		}
	}
	var resp struct {
		State             string `json:"state"`
		Accepted          int    `json:"accepted"`
		CacheUpdated      bool   `json:"cache_updated"`
		ReloadStarted     bool   `json:"reload_started"`
		CachePath         string `json:"cache_path"`
		CacheNodeCount    int    `json:"cache_node_count"`
		CacheFresh        bool   `json:"cache_fresh"`
		CacheEnabled      bool   `json:"cache_enabled"`
		AutoReload        bool   `json:"auto_reload"`
		TotalSources      int    `json:"total_sources"`
		EnabledSources    int    `json:"enabled_sources"`
		FilterMinTier     string `json:"filter_min_tier"`
		FilterProbeBudget int    `json:"filter_probe_budget"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.State != "succeeded" || resp.Accepted != 1 || resp.CacheUpdated || resp.ReloadStarted {
		t.Fatalf("unexpected status payload: %#v body=%s", resp, rec.Body.String())
	}
	if resp.CachePath != cachePath || resp.CacheNodeCount != 1 || !resp.CacheFresh || !resp.CacheEnabled || !resp.AutoReload {
		t.Fatalf("missing cache context in status payload: %#v body=%s", resp, rec.Body.String())
	}
	if resp.TotalSources != 1 || resp.EnabledSources != 1 || resp.FilterMinTier == "" {
		t.Fatalf("missing source/filter context in status payload: %#v body=%s", resp, rec.Body.String())
	}
}

func TestFreeProxyRefreshUsesFreshCacheWithoutRemoteFetchOrReload(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	cachePath := filepath.Join(tmp, "cache.txt")
	if err := os.WriteFile(cachePath, []byte("http://9.9.9.9:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("fresh cache refresh should not fetch remote source")
	}))
	defer slow.Close()
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
free_proxy_cache:
  enabled: true
  path: ` + cachePath + `
  auto_reload: true
  max_age: 1h
free_proxy_filter:
  enabled: false
free_proxy_sources:
  - name: remote-source
    url: ` + slow.URL + `
    format: txt
    timeout: 1s
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
	if _, started, err := server.startFreeProxyRefresh("test"); err != nil || !started {
		t.Fatalf("startFreeProxyRefresh started=%v err=%v", started, err)
	}
	deadline := time.After(500 * time.Millisecond)
	for {
		status := server.currentFreeProxyRefreshStatus()
		if status.State == "succeeded" {
			if status.Accepted != 1 || status.CacheUpdated || status.ReloadStarted {
				t.Fatalf("fresh cache refresh should succeed without update/reload, got %#v", status)
			}
			if status.DurationMS > 200 {
				t.Fatalf("fresh cache refresh too slow: %#v", status)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("refresh did not finish quickly, status=%#v", status)
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case <-fake.done:
		t.Fatal("auto reload should not run when fresh cache was reused")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestFreeProxyRefreshReusesStaleCacheWithoutReload(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	cachePath := filepath.Join(tmp, "cache.txt")
	if err := os.WriteFile(cachePath, []byte("http://9.9.9.9:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(cachePath, old, old); err != nil {
		t.Fatal(err)
	}
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
free_proxy_cache:
  enabled: true
  path: ` + cachePath + `
  auto_reload: true
  max_age: 1h
free_proxy_filter:
  enabled: false
free_proxy_sources:
  - name: missing-source
    file: ` + filepath.Join(tmp, "missing.txt") + `
    format: txt
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
	if _, started, err := server.startFreeProxyRefresh("test"); err != nil || !started {
		t.Fatalf("startFreeProxyRefresh started=%v err=%v", started, err)
	}
	deadline := time.After(2 * time.Second)
	for {
		status := server.currentFreeProxyRefreshStatus()
		if status.State == "succeeded" {
			if status.Accepted != 1 || status.CacheUpdated || status.ReloadStarted {
				t.Fatalf("stale cache refresh should succeed without reload, got %#v", status)
			}
			if len(status.Sources) != 1 || status.Sources[0].Error == "" {
				t.Fatalf("source failure telemetry missing: %#v", status.Sources)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("refresh did not finish, status=%#v", status)
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case <-fake.done:
		t.Fatal("auto reload should not run when only stale cache was reused")
	case <-time.After(100 * time.Millisecond):
	}
	if fake.ReloadCalls() != 0 {
		t.Fatalf("reloadCalls = %d, want 0", fake.ReloadCalls())
	}
}

func TestHandleFreeProxyRefreshStartsManualBackgroundRefresh(t *testing.T) {
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
  auto_reload: false
free_proxy_filter:
  enabled: false
free_proxy_sources:
  - name: local
    file: ` + sourcePath + `
    format: txt
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg, reloadState: "idle"}
	req := httptest.NewRequest(http.MethodPost, "/api/free-proxy/refresh", nil)
	rec := httptest.NewRecorder()
	server.handleFreeProxyRefresh(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Started bool                   `json:"started"`
		Status  freeProxyRefreshStatus `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Started || resp.Status.State != "running" || resp.Status.RequestedBy != "manual" {
		t.Fatalf("unexpected manual refresh response: %#v body=%s", resp, rec.Body.String())
	}
	deadline := time.After(2 * time.Second)
	for {
		status := server.currentFreeProxyRefreshStatus()
		if status.State == "succeeded" {
			if status.Accepted != 1 {
				t.Fatalf("accepted=%d, want 1: %#v", status.Accepted, status)
			}
			break
		}
		if status.State == "failed" {
			t.Fatalf("manual refresh failed: %#v", status)
		}
		select {
		case <-deadline:
			t.Fatalf("manual refresh did not finish, status=%#v", status)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestHandleConfigNodesCRUDReportsNeedReloadWithoutReloading(t *testing.T) {
	fake := &fakeNodeManager{nodes: []config.NodeConfig{{Name: "old", URI: "http://127.0.0.1:18080", Port: 18080}}}
	server := &Server{nodeMgr: fake, reloadState: "idle"}

	listReq := httptest.NewRequest(http.MethodGet, "/api/nodes/config", nil)
	listRec := httptest.NewRecorder()
	server.handleConfigNodes(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Nodes []config.NodeConfig `json:"nodes"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Nodes) != 1 || listResp.Nodes[0].Name != "old" {
		t.Fatalf("unexpected list response: %#v body=%s", listResp, listRec.Body.String())
	}

	createBody := []byte(`{"name":"new","uri":"http://127.0.0.1:18081","port":18081}`)
	createRec := httptest.NewRecorder()
	server.handleConfigNodes(createRec, httptest.NewRequest(http.MethodPost, "/api/nodes/config", bytes.NewReader(createBody)))
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	var createResp struct {
		Node       config.NodeConfig `json:"node"`
		NeedReload bool              `json:"need_reload"`
		Message    string            `json:"message"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatal(err)
	}
	if createResp.Node.Name != "new" || !createResp.NeedReload || createResp.Message == "" {
		t.Fatalf("unexpected create response: %#v body=%s", createResp, createRec.Body.String())
	}

	updateBody := []byte(`{"name":"newer","uri":"http://127.0.0.1:18082","port":18082}`)
	updateRec := httptest.NewRecorder()
	server.handleConfigNodeItem(updateRec, httptest.NewRequest(http.MethodPut, "/api/nodes/config/new", bytes.NewReader(updateBody)))
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updateRec.Code, updateRec.Body.String())
	}
	var updateResp struct {
		Node       config.NodeConfig `json:"node"`
		NeedReload bool              `json:"need_reload"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updateResp); err != nil {
		t.Fatal(err)
	}
	if updateResp.Node.Name != "newer" || !updateResp.NeedReload || fake.updatedName != "new" {
		t.Fatalf("unexpected update response: %#v updatedName=%q body=%s", updateResp, fake.updatedName, updateRec.Body.String())
	}

	deleteRec := httptest.NewRecorder()
	server.handleConfigNodeItem(deleteRec, httptest.NewRequest(http.MethodDelete, "/api/nodes/config/newer", nil))
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleteRec.Code, deleteRec.Body.String())
	}
	var deleteResp struct {
		NeedReload bool   `json:"need_reload"`
		Message    string `json:"message"`
	}
	if err := json.Unmarshal(deleteRec.Body.Bytes(), &deleteResp); err != nil {
		t.Fatal(err)
	}
	if !deleteResp.NeedReload || deleteResp.Message == "" || len(fake.deleted) != 1 || fake.deleted[0] != "newer" {
		t.Fatalf("unexpected delete response: %#v deleted=%#v body=%s", deleteResp, fake.deleted, deleteRec.Body.String())
	}
	if fake.ReloadCalls() != 0 {
		t.Fatalf("CRUD should not reload automatically, reloadCalls=%d", fake.ReloadCalls())
	}
}

func TestHandleReloadStartsAsyncReload(t *testing.T) {
	fake := &fakeNodeManager{delay: 200 * time.Millisecond, done: make(chan struct{})}
	server := &Server{nodeMgr: fake, reloadState: "idle"}
	req := httptest.NewRequest(http.MethodPost, "/api/reload", nil)
	rec := httptest.NewRecorder()
	start := time.Now()
	server.handleReload(rec, req)
	elapsed := time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if elapsed >= fake.delay {
		t.Fatalf("manual reload waited for core reload: elapsed=%s delay=%s", elapsed, fake.delay)
	}
	var resp struct {
		Started      bool         `json:"started"`
		ReloadStatus reloadStatus `json:"reload_status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Started || resp.ReloadStatus.State != "running" || resp.ReloadStatus.RequestedBy != "manual" {
		t.Fatalf("unexpected reload response: %#v body=%s", resp, rec.Body.String())
	}
	select {
	case <-fake.done:
	case <-time.After(2 * time.Second):
		t.Fatal("async reload did not finish")
	}
	if fake.ReloadCalls() != 1 {
		t.Fatalf("reloadCalls = %d, want 1", fake.ReloadCalls())
	}
}

func TestHandleReloadQueuesWhenReloadAlreadyRunning(t *testing.T) {
	fake := &fakeNodeManager{delay: 120 * time.Millisecond, reloadCh: make(chan int, 4)}
	server := &Server{nodeMgr: fake, reloadState: "idle"}
	first := httptest.NewRecorder()
	server.handleReload(first, httptest.NewRequest(http.MethodPost, "/api/reload", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, body = %s", first.Code, first.Body.String())
	}
	select {
	case <-fake.reloadCh:
	case <-time.After(time.Second):
		t.Fatal("first reload did not start")
	}
	second := httptest.NewRecorder()
	server.handleReload(second, httptest.NewRequest(http.MethodPost, "/api/reload", nil))
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, body = %s", second.Code, second.Body.String())
	}
	var resp struct {
		Started      bool         `json:"started"`
		ReloadStatus reloadStatus `json:"reload_status"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Started || resp.ReloadStatus.State != "running" || !resp.ReloadStatus.Pending {
		t.Fatalf("expected queued reload response, got %#v body=%s", resp, second.Body.String())
	}
	select {
	case <-fake.reloadCh:
	case <-time.After(2 * time.Second):
		t.Fatal("queued reload did not start")
	}
	deadline := time.After(3 * time.Second)
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
			t.Fatalf("queued reload did not finish, status=%#v calls=%d", status, fake.ReloadCalls())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if fake.ReloadCalls() != 2 {
		t.Fatalf("reloadCalls = %d, want 2", fake.ReloadCalls())
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

type fakeSubscriptionRefresher struct {
	status SubscriptionStatus
}

func (f fakeSubscriptionRefresher) RefreshNow() error          { return nil }
func (f fakeSubscriptionRefresher) Status() SubscriptionStatus { return f.status }
func (f fakeSubscriptionRefresher) UpdateConfig(urls []string, enabled bool, interval time.Duration) {
}
func (f fakeSubscriptionRefresher) UpdateConfigAndRefresh(urls []string, enabled bool, interval time.Duration) error {
	return nil
}

type recordingSubscriptionRefresher struct {
	status               SubscriptionStatus
	refreshCalls         int
	refreshStarted       chan struct{}
	refreshBlock         chan struct{}
	updateCalls          int
	updateRefreshCalls   int
	updateRefreshStarted chan struct{}
	updateRefreshBlock   chan struct{}
	lastURLs             []string
	lastEnabled          bool
	lastInterval         time.Duration
}

func (f *recordingSubscriptionRefresher) RefreshNow() error {
	f.refreshCalls++
	if f.refreshStarted != nil {
		f.refreshStarted <- struct{}{}
	}
	if f.refreshBlock != nil {
		<-f.refreshBlock
	}
	return nil
}
func (f *recordingSubscriptionRefresher) Status() SubscriptionStatus { return f.status }
func (f *recordingSubscriptionRefresher) UpdateConfig(urls []string, enabled bool, interval time.Duration) {
	f.updateCalls++
	f.lastURLs = append([]string(nil), urls...)
	f.lastEnabled = enabled
	f.lastInterval = interval
}
func (f *recordingSubscriptionRefresher) UpdateConfigAndRefresh(urls []string, enabled bool, interval time.Duration) error {
	f.updateRefreshCalls++
	if f.updateRefreshStarted != nil {
		f.updateRefreshStarted <- struct{}{}
	}
	if f.updateRefreshBlock != nil {
		<-f.updateRefreshBlock
	}
	f.lastURLs = append([]string(nil), urls...)
	f.lastEnabled = enabled
	f.lastInterval = interval
	return nil
}

func TestHandleSubscriptionRefreshStartsInBackground(t *testing.T) {
	refresher := &recordingSubscriptionRefresher{
		status:         SubscriptionStatus{NodeCount: 7},
		refreshStarted: make(chan struct{}, 1),
		refreshBlock:   make(chan struct{}),
	}
	server := &Server{}
	server.SetSubscriptionRefresher(refresher)

	req := httptest.NewRequest(http.MethodPost, "/api/subscription/refresh", nil)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		server.handleSubscriptionRefresh(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		close(refresher.refreshBlock)
		t.Fatal("subscription refresh endpoint blocked waiting for refresh completion")
	}
	close(refresher.refreshBlock)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Started bool `json:"started"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Started {
		t.Fatalf("unexpected response started=%v body=%s", resp.Started, rec.Body.String())
	}

	select {
	case <-refresher.refreshStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("background refresh was not triggered")
	}
	if refresher.refreshCalls != 1 {
		t.Fatalf("refreshCalls=%d, want 1", refresher.refreshCalls)
	}
}

func TestHandleSubscriptionRefreshReportsRuntimeSubscriptionNodeCount(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	mgr.Register(NodeInfo{Tag: "sub-a", Name: "Sub A", Source: "subscription"})
	mgr.Register(NodeInfo{Tag: "sub-b", Name: "Sub B", Source: "subscription"})
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	srv.SetSubscriptionRefresher(&recordingSubscriptionRefresher{status: SubscriptionStatus{NodeCount: 0}})

	req := httptest.NewRequest(http.MethodPost, "/api/subscription/refresh", nil)
	rec := httptest.NewRecorder()
	srv.handleSubscriptionRefresh(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		NodeCount int `json:"node_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.NodeCount != 2 {
		t.Fatalf("node_count=%d, want runtime subscription count 2; body=%s", resp.NodeCount, rec.Body.String())
	}
}

func TestHandleSubscriptionConfigSkipsRefreshForUnchangedConfig(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
subscriptions:
  - https://example.test/sub-a
subscription_refresh:
  enabled: true
  interval: 1h
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	refresher := &recordingSubscriptionRefresher{status: SubscriptionStatus{NodeCount: 7}}
	server := &Server{cfgSrc: cfg}
	server.SetSubscriptionRefresher(refresher)

	body := []byte(`{"subscriptions":["https://example.test/sub-a"],"enabled":true,"interval":"1h"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/subscription/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSubscriptionConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if refresher.updateCalls != 0 || refresher.updateRefreshCalls != 0 {
		t.Fatalf("unchanged config should not update refresher: update=%d refresh=%d", refresher.updateCalls, refresher.updateRefreshCalls)
	}
	var resp struct {
		ConfigChanged    bool `json:"config_changed"`
		RefreshTriggered bool `json:"refresh_triggered"`
		NodeCount        int  `json:"node_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ConfigChanged || resp.RefreshTriggered || resp.NodeCount != 7 {
		t.Fatalf("unexpected response: %#v body=%s", resp, rec.Body.String())
	}
}

func TestHandleSubscriptionConfigUpdatesIntervalWithoutRefresh(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
subscriptions:
  - https://example.test/sub-a
subscription_refresh:
  enabled: true
  interval: 1h
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	refresher := &recordingSubscriptionRefresher{status: SubscriptionStatus{NodeCount: 7}}
	server := &Server{cfgSrc: cfg}
	server.SetSubscriptionRefresher(refresher)

	body := []byte(`{"subscriptions":["https://example.test/sub-a"],"enabled":true,"interval":"61m"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/subscription/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSubscriptionConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if refresher.updateCalls != 1 || refresher.updateRefreshCalls != 0 {
		t.Fatalf("interval-only change should update scheduler without refresh: update=%d refresh=%d", refresher.updateCalls, refresher.updateRefreshCalls)
	}
	if refresher.lastInterval != 61*time.Minute {
		t.Fatalf("lastInterval=%s, want 61m", refresher.lastInterval)
	}
	var resp struct {
		ConfigChanged    bool   `json:"config_changed"`
		RefreshTriggered bool   `json:"refresh_triggered"`
		Interval         string `json:"interval"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.ConfigChanged || resp.RefreshTriggered || resp.Interval != "1h1m0s" {
		t.Fatalf("unexpected response: %#v body=%s", resp, rec.Body.String())
	}
}

func TestHandleSubscriptionConfigURLChangeRefreshesInBackground(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
subscriptions:
  - https://example.test/sub-a
subscription_refresh:
  enabled: true
  interval: 1h
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	refresher := &recordingSubscriptionRefresher{
		status:               SubscriptionStatus{NodeCount: 9},
		updateRefreshStarted: make(chan struct{}, 1),
		updateRefreshBlock:   make(chan struct{}),
	}
	server := &Server{cfgSrc: cfg}
	server.SetSubscriptionRefresher(refresher)

	body := []byte(`{"subscriptions":["https://example.test/sub-b"],"enabled":true,"interval":"1h"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/subscription/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		server.handleSubscriptionConfig(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		close(refresher.updateRefreshBlock)
		t.Fatal("subscription config save blocked waiting for refresh completion")
	}
	close(refresher.updateRefreshBlock)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ConfigChanged    bool `json:"config_changed"`
		RefreshTriggered bool `json:"refresh_triggered"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.ConfigChanged || !resp.RefreshTriggered {
		t.Fatalf("unexpected response: %#v body=%s", resp, rec.Body.String())
	}

	select {
	case <-refresher.updateRefreshStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("background config refresh was not triggered")
	}
	if refresher.updateRefreshCalls != 1 {
		t.Fatalf("updateRefreshCalls=%d, want 1", refresher.updateRefreshCalls)
	}
}

func TestHandleSubscriptionConfigRefreshesWhenURLsChange(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
subscriptions:
  - https://example.test/sub-a
subscription_refresh:
  enabled: true
  interval: 1h
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	refresher := &recordingSubscriptionRefresher{status: SubscriptionStatus{NodeCount: 9}, updateRefreshStarted: make(chan struct{}, 1)}
	server := &Server{cfgSrc: cfg}
	server.SetSubscriptionRefresher(refresher)

	body := []byte(`{"subscriptions":["https://example.test/sub-b"],"enabled":true,"interval":"1h"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/subscription/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSubscriptionConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-refresher.updateRefreshStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("background config refresh was not triggered")
	}
	if refresher.updateCalls != 0 || refresher.updateRefreshCalls != 1 {
		t.Fatalf("url change should refresh: update=%d refresh=%d", refresher.updateCalls, refresher.updateRefreshCalls)
	}
	var resp struct {
		ConfigChanged    bool `json:"config_changed"`
		RefreshTriggered bool `json:"refresh_triggered"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.ConfigChanged || !resp.RefreshTriggered {
		t.Fatalf("unexpected response: %#v body=%s", resp, rec.Body.String())
	}
}

func TestHandleSubscriptionConfigDisableDoesNotRefresh(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
subscriptions:
  - https://example.test/sub-a
subscription_refresh:
  enabled: true
  interval: 1h
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	refresher := &recordingSubscriptionRefresher{status: SubscriptionStatus{NodeCount: 9}}
	server := &Server{cfgSrc: cfg}
	server.SetSubscriptionRefresher(refresher)

	body := []byte(`{"subscriptions":["https://example.test/sub-a"],"enabled":false,"interval":"1h"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/subscription/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSubscriptionConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if refresher.updateCalls != 1 || refresher.updateRefreshCalls != 0 {
		t.Fatalf("disable should update scheduler without refresh: update=%d refresh=%d", refresher.updateCalls, refresher.updateRefreshCalls)
	}
	if refresher.lastEnabled {
		t.Fatalf("lastEnabled=true, want false")
	}
	var resp struct {
		ConfigChanged    bool `json:"config_changed"`
		RefreshTriggered bool `json:"refresh_triggered"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.ConfigChanged || resp.RefreshTriggered {
		t.Fatalf("unexpected response: %#v body=%s", resp, rec.Body.String())
	}
}

func TestHandleSubscriptionConfigEnableRefreshesExistingURLs(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
subscriptions:
  - https://example.test/sub-a
subscription_refresh:
  enabled: false
  interval: 1h
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	refresher := &recordingSubscriptionRefresher{status: SubscriptionStatus{NodeCount: 9}, updateRefreshStarted: make(chan struct{}, 1)}
	server := &Server{cfgSrc: cfg}
	server.SetSubscriptionRefresher(refresher)

	body := []byte(`{"subscriptions":["https://example.test/sub-a"],"enabled":true,"interval":"1h"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/subscription/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSubscriptionConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-refresher.updateRefreshStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("background config refresh was not triggered")
	}
	if refresher.updateCalls != 0 || refresher.updateRefreshCalls != 1 {
		t.Fatalf("enable should refresh existing URLs: update=%d refresh=%d", refresher.updateCalls, refresher.updateRefreshCalls)
	}
	if !refresher.lastEnabled {
		t.Fatalf("lastEnabled=false, want true")
	}
	var resp struct {
		ConfigChanged    bool `json:"config_changed"`
		RefreshTriggered bool `json:"refresh_triggered"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.ConfigChanged || !resp.RefreshTriggered {
		t.Fatalf("unexpected response: %#v body=%s", resp, rec.Body.String())
	}
}

func TestHandleSubscriptionStatusFallsBackToRuntimeSubscriptionNodes(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	mgr.Register(NodeInfo{Tag: "sub-a", Name: "Sub A", Source: "subscription"})
	mgr.Register(NodeInfo{Tag: "sub-b", Name: "Sub B", Source: "subscription"})
	mgr.Register(NodeInfo{Tag: "free-a", Name: "Free A", Source: "free_proxy"})
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	srv.SetSubscriptionRefresher(fakeSubscriptionRefresher{status: SubscriptionStatus{NodeCount: 0}})

	req := httptest.NewRequest(http.MethodGet, "/api/subscription/status", nil)
	rec := httptest.NewRecorder()
	srv.handleSubscriptionStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		NodeCount int `json:"node_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.NodeCount != 2 {
		t.Fatalf("node_count=%d, want runtime subscription count 2; body=%s", resp.NodeCount, rec.Body.String())
	}
}

func TestHandleSubscriptionStatusWithoutRefresherReportsRuntimeSubscriptionNodes(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	mgr.Register(NodeInfo{Tag: "sub-a", Name: "Sub A", Source: "subscription"})
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/subscription/status", nil)
	rec := httptest.NewRecorder()
	srv.handleSubscriptionStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Enabled   bool `json:"enabled"`
		NodeCount int  `json:"node_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Enabled || resp.NodeCount != 1 {
		t.Fatalf("unexpected response: %#v body=%s", resp, rec.Body.String())
	}
}
