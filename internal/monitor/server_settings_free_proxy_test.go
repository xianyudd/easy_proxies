package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/nodesource"
)

type fakeNodeManager struct {
	mu          sync.Mutex
	delay       time.Duration
	err         error
	listErr     error
	createErr   error
	updateErr   error
	deleteErr   error
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

func TestHandleSettingsReturnsStructuredErrorCodes(t *testing.T) {
	cases := []struct {
		name   string
		server *Server
		body   string
		status int
		code   string
	}{
		{
			name:   "bad json",
			server: &Server{},
			body:   `{`,
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name:   "trailing json",
			server: &Server{},
			body:   `{"external_ip":"1.2.3.4"}{"extra":true}`,
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name:   "missing config store",
			server: &Server{},
			body:   `{"external_ip":"1.2.3.4"}`,
			status: http.StatusInternalServerError,
			code:   "config_store_uninitialized",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader([]byte(tc.body)))
			rec := httptest.NewRecorder()

			tc.server.handleSettings(rec, req)

			assertSettingsErrorCode(t, rec, tc.status, tc.code)
		})
	}
}

func assertSettingsErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status=%d, want %d body=%s", rec.Code, status, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error == "" || body.Code != code {
		t.Fatalf("unexpected body: %#v raw=%s", body, rec.Body.String())
	}
}

func (f *fakeNodeManager) ListConfigNodes(ctx context.Context) ([]config.NodeConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]config.NodeConfig, len(f.nodes))
	copy(out, f.nodes)
	return out, nil
}

func (f *fakeNodeManager) CreateNode(ctx context.Context, node config.NodeConfig) (config.NodeConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return config.NodeConfig{}, f.createErr
	}
	f.created = append(f.created, node)
	f.nodes = append(f.nodes, node)
	return node, nil
}

func (f *fakeNodeManager) UpdateNode(ctx context.Context, name string, node config.NodeConfig) (config.NodeConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return config.NodeConfig{}, f.updateErr
	}
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
	if f.deleteErr != nil {
		return f.deleteErr
	}
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

func TestManagementHandlersRejectMethodsWithStructuredCode(t *testing.T) {
	server := &Server{}
	cases := []struct {
		name string
		req  *http.Request
		call func(http.ResponseWriter, *http.Request)
	}{
		{name: "settings", req: httptest.NewRequest(http.MethodPost, "/api/settings", nil), call: server.handleSettings},
		{name: "subscription status", req: httptest.NewRequest(http.MethodPost, "/api/subscription/status", nil), call: server.handleSubscriptionStatus},
		{name: "subscription refresh", req: httptest.NewRequest(http.MethodGet, "/api/subscription/refresh", nil), call: server.handleSubscriptionRefresh},
		{name: "subscription config", req: httptest.NewRequest(http.MethodPost, "/api/subscription/config", nil), call: server.handleSubscriptionConfig},
		{name: "reload", req: httptest.NewRequest(http.MethodGet, "/api/reload", nil), call: server.handleReload},
		{name: "reload status", req: httptest.NewRequest(http.MethodPost, "/api/reload/status", nil), call: server.handleReloadStatus},
		{name: "free proxy refresh status", req: httptest.NewRequest(http.MethodPost, "/api/free-proxy/refresh/status", nil), call: server.handleFreeProxyRefreshStatus},
		{name: "free proxy refresh", req: httptest.NewRequest(http.MethodGet, "/api/free-proxy/refresh", nil), call: server.handleFreeProxyRefresh},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			tc.call(rec, tc.req)

			assertSettingsErrorCode(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
		})
	}
}

func TestConfigNodeHandlersRejectMethodsWithStructuredCode(t *testing.T) {
	server := &Server{nodeMgr: &fakeNodeManager{}}
	cases := []struct {
		name string
		req  *http.Request
		call func(http.ResponseWriter, *http.Request)
	}{
		{name: "config nodes", req: httptest.NewRequest(http.MethodPatch, "/api/nodes/config", nil), call: server.handleConfigNodes},
		{name: "config node item", req: httptest.NewRequest(http.MethodPost, "/api/nodes/config/node-a", nil), call: server.handleConfigNodeItem},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			tc.call(rec, tc.req)

			assertSettingsErrorCode(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
		})
	}
}

func TestConfigNodeHandlersRejectTrailingJSON(t *testing.T) {
	server := &Server{nodeMgr: &fakeNodeManager{}}
	cases := []struct {
		name string
		req  *http.Request
		call func(http.ResponseWriter, *http.Request)
	}{
		{name: "create", req: httptest.NewRequest(http.MethodPost, "/api/nodes/config", bytes.NewReader([]byte(`{"name":"node-a","uri":"http://127.0.0.1:1"}{"extra":true}`))), call: server.handleConfigNodes},
		{name: "update", req: httptest.NewRequest(http.MethodPut, "/api/nodes/config/node-a", bytes.NewReader([]byte(`{"name":"node-a","uri":"http://127.0.0.1:1"}{"extra":true}`))), call: server.handleConfigNodeItem},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			tc.call(rec, tc.req)

			assertSettingsErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
		})
	}
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

func TestHandleSettingsPreservesFreeProxyFilterFieldsWhenPartiallyUpdated(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
free_proxy_filter:
  enabled: true
  min_tier: simple_web
  workers: 123
  timeout: 1500ms
  max_candidates: 4567
  max_probe_candidates: 89
  probes:
    http: http://cp.cloudflare.com/generate_204
    https: https://example.com/
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg, reloadState: "idle"}

	body := []byte(`{"free_proxy_filter":{"enabled":false}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	f := reloaded.FreeProxyFilter
	if f.Enabled || f.MinTier != "simple_web" || f.Workers != 123 || f.Timeout != 1500*time.Millisecond || f.MaxCandidates != 4567 || f.MaxProbeCandidates != 89 || f.Probes.HTTP != "http://cp.cloudflare.com/generate_204" || f.Probes.HTTPS != "https://example.com/" {
		t.Fatalf("free_proxy_filter partial update corrupted fields: %#v", f)
	}
}

func TestHandleSettingsRejectsInvalidPoolDurationWithoutMutatingMemory(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
external_ip: 1.1.1.1
pool:
  mode: round_robin
  failure_threshold: 3
  blacklist_duration: 5m
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
		"external_ip": "2.2.2.2",
		"pool": {"mode":"least_failures","failure_threshold":9,"blacklist_duration":"bad-duration"}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body = %s", rec.Code, rec.Body.String())
	}
	if server.cfgSrc.ExternalIP != "1.1.1.1" || server.cfgSrc.Pool.Mode != "round_robin" || server.cfgSrc.Pool.FailureThreshold != 3 || server.cfgSrc.Pool.BlacklistDuration != 5*time.Minute {
		t.Fatalf("invalid settings should not mutate memory: external_ip=%q pool=%#v", server.cfgSrc.ExternalIP, server.cfgSrc.Pool)
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ExternalIP != "1.1.1.1" || reloaded.Pool.Mode != "round_robin" || reloaded.Pool.FailureThreshold != 3 || reloaded.Pool.BlacklistDuration != 5*time.Minute {
		t.Fatalf("invalid settings should not be persisted: external_ip=%q pool=%#v", reloaded.ExternalIP, reloaded.Pool)
	}
}

func TestHandleSettingsRejectsInvalidGeoIPIntervalWithoutMutatingMemory(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
external_ip: 1.1.1.1
geoip:
  enabled: true
  database_path: /tmp/GeoLite2.mmdb
  listen: 127.0.0.1
  port: 1221
  auto_update_enabled: true
  auto_update_interval: 24h
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
		"external_ip": "2.2.2.2",
		"geoip": {
			"enabled": true,
			"database_path": "/tmp/other.mmdb",
			"listen": "127.0.0.1",
			"port": 1222,
			"auto_update_enabled": false,
			"auto_update_interval": "bad-duration"
		}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body = %s", rec.Code, rec.Body.String())
	}
	if server.cfgSrc.ExternalIP != "1.1.1.1" || server.cfgSrc.GeoIP.DatabasePath != "/tmp/GeoLite2.mmdb" || server.cfgSrc.GeoIP.Port != 1221 || server.cfgSrc.GeoIP.AutoUpdateInterval != 24*time.Hour {
		t.Fatalf("invalid geoip settings should not mutate memory: external_ip=%q geoip=%#v", server.cfgSrc.ExternalIP, server.cfgSrc.GeoIP)
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ExternalIP != "1.1.1.1" || reloaded.GeoIP.DatabasePath != "/tmp/GeoLite2.mmdb" || reloaded.GeoIP.Port != 1221 || reloaded.GeoIP.AutoUpdateInterval != 24*time.Hour {
		t.Fatalf("invalid geoip settings should not be persisted: external_ip=%q geoip=%#v", reloaded.ExternalIP, reloaded.GeoIP)
	}
}

func TestHandleSettingsRejectsInvalidFreeProxyFilterTimeoutWithoutMutatingMemory(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
external_ip: 1.1.1.1
free_proxy_filter:
  enabled: true
  min_tier: simple_web
  workers: 12
  timeout: 1500ms
  max_candidates: 100
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
		"external_ip": "2.2.2.2",
		"free_proxy_filter": {
			"enabled": true,
			"min_tier": "simple_web",
			"workers": 22,
			"timeout": "bad-duration",
			"max_candidates": 200,
			"probes": {"http":"http://cp.cloudflare.com/generate_204","https":"https://example.com/"}
		}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body = %s", rec.Code, rec.Body.String())
	}
	if server.cfgSrc.ExternalIP != "1.1.1.1" || server.cfgSrc.FreeProxyFilter.Workers != 12 || server.cfgSrc.FreeProxyFilter.Timeout != 1500*time.Millisecond || server.cfgSrc.FreeProxyFilter.MaxCandidates != 100 {
		t.Fatalf("invalid free proxy filter should not mutate memory: external_ip=%q filter=%#v", server.cfgSrc.ExternalIP, server.cfgSrc.FreeProxyFilter)
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ExternalIP != "1.1.1.1" || reloaded.FreeProxyFilter.Workers != 12 || reloaded.FreeProxyFilter.Timeout != 1500*time.Millisecond || reloaded.FreeProxyFilter.MaxCandidates != 100 {
		t.Fatalf("invalid free proxy filter should not be persisted: external_ip=%q filter=%#v", reloaded.ExternalIP, reloaded.FreeProxyFilter)
	}
}

func TestHandleSettingsRejectsInvalidFreeProxyFilterNumbersWithoutMutatingMemory(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
external_ip: 1.1.1.1
free_proxy_filter:
  enabled: true
  min_tier: simple_web
  workers: 12
  timeout: 1500ms
  max_candidates: 100
  max_probe_candidates: 50
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg}

	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{name: "zero workers", body: `{"external_ip":"2.2.2.2","free_proxy_filter":{"enabled":true,"min_tier":"simple_web","workers":0,"timeout":"1500ms","max_candidates":100,"max_probe_candidates":50,"probes":{"http":"http://cp.cloudflare.com/generate_204","https":"https://example.com/"}}}`, code: "invalid_free_proxy_filter_workers"},
		{name: "negative workers", body: `{"external_ip":"2.2.2.2","free_proxy_filter":{"enabled":true,"min_tier":"simple_web","workers":-1,"timeout":"1500ms","max_candidates":100,"max_probe_candidates":50,"probes":{"http":"http://cp.cloudflare.com/generate_204","https":"https://example.com/"}}}`, code: "invalid_free_proxy_filter_workers"},
		{name: "negative max candidates", body: `{"external_ip":"2.2.2.2","free_proxy_filter":{"enabled":true,"min_tier":"simple_web","workers":12,"timeout":"1500ms","max_candidates":-1,"max_probe_candidates":50,"probes":{"http":"http://cp.cloudflare.com/generate_204","https":"https://example.com/"}}}`, code: "invalid_free_proxy_filter_max_candidates"},
		{name: "negative max probe candidates", body: `{"external_ip":"2.2.2.2","free_proxy_filter":{"enabled":true,"min_tier":"simple_web","workers":12,"timeout":"1500ms","max_candidates":100,"max_probe_candidates":-1,"probes":{"http":"http://cp.cloudflare.com/generate_204","https":"https://example.com/"}}}`, code: "invalid_free_proxy_filter_max_probe_candidates"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			server.handleSettings(rec, req)

			assertSettingsErrorCode(t, rec, http.StatusBadRequest, tc.code)
			if server.cfgSrc.ExternalIP != "1.1.1.1" || server.cfgSrc.FreeProxyFilter.Workers != 12 || server.cfgSrc.FreeProxyFilter.MaxCandidates != 100 || server.cfgSrc.FreeProxyFilter.MaxProbeCandidates != 50 {
				t.Fatalf("invalid free proxy filter should not mutate memory: external_ip=%q filter=%#v", server.cfgSrc.ExternalIP, server.cfgSrc.FreeProxyFilter)
			}
			reloaded, err := config.Load(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if reloaded.ExternalIP != "1.1.1.1" || reloaded.FreeProxyFilter.Workers != 12 || reloaded.FreeProxyFilter.MaxCandidates != 100 || reloaded.FreeProxyFilter.MaxProbeCandidates != 50 {
				t.Fatalf("invalid free proxy filter should not be persisted: external_ip=%q filter=%#v", reloaded.ExternalIP, reloaded.FreeProxyFilter)
			}
		})
	}
}

func TestHandleSettingsRejectsNonPositiveFreeProxyDurationsWithoutMutatingMemory(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
external_ip: 1.1.1.1
free_proxy_filter:
  enabled: true
  min_tier: simple_web
  workers: 12
  timeout: 1500ms
  max_candidates: 100
free_proxy_cache:
  enabled: true
  path: .cache/free-proxies.txt
  refresh_on_start: true
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
	originalFilter := cfg.FreeProxyFilter
	originalCache := cfg.FreeProxyCache
	server := &Server{cfgSrc: cfg}

	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{name: "zero filter timeout", body: `{"external_ip":"2.2.2.2","free_proxy_filter":{"enabled":true,"min_tier":"simple_web","workers":12,"timeout":"0s","max_candidates":100,"max_probe_candidates":0,"probes":{"http":"http://cp.cloudflare.com/generate_204","https":"https://example.com/"}}}`, code: "invalid_free_proxy_filter_timeout"},
		{name: "negative filter timeout", body: `{"external_ip":"2.2.2.2","free_proxy_filter":{"enabled":true,"min_tier":"simple_web","workers":12,"timeout":"-1s","max_candidates":100,"max_probe_candidates":0,"probes":{"http":"http://cp.cloudflare.com/generate_204","https":"https://example.com/"}}}`, code: "invalid_free_proxy_filter_timeout"},
		{name: "zero cache max age", body: `{"external_ip":"2.2.2.2","free_proxy_cache":{"enabled":true,"path":".cache/other.txt","refresh_on_start":false,"auto_reload":false,"workers":4,"max_age":"0s"}}`, code: "invalid_free_proxy_cache_max_age"},
		{name: "negative cache max age", body: `{"external_ip":"2.2.2.2","free_proxy_cache":{"enabled":true,"path":".cache/other.txt","refresh_on_start":false,"auto_reload":false,"workers":4,"max_age":"-1s"}}`, code: "invalid_free_proxy_cache_max_age"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			server.handleSettings(rec, req)

			assertSettingsErrorCode(t, rec, http.StatusBadRequest, tc.code)
			if server.cfgSrc.ExternalIP != "1.1.1.1" || server.cfgSrc.FreeProxyFilter.Timeout != originalFilter.Timeout || server.cfgSrc.FreeProxyCache.MaxAge != originalCache.MaxAge {
				t.Fatalf("invalid durations should not mutate memory: external_ip=%q filter=%#v cache=%#v", server.cfgSrc.ExternalIP, server.cfgSrc.FreeProxyFilter, server.cfgSrc.FreeProxyCache)
			}
			reloaded, err := config.Load(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if reloaded.ExternalIP != "1.1.1.1" || reloaded.FreeProxyFilter.Timeout != originalFilter.Timeout || reloaded.FreeProxyCache.MaxAge != originalCache.MaxAge {
				t.Fatalf("invalid durations should not be persisted: external_ip=%q filter=%#v cache=%#v", reloaded.ExternalIP, reloaded.FreeProxyFilter, reloaded.FreeProxyCache)
			}
		})
	}
}

func TestHandleSettingsRejectsInvalidFreeProxyCacheMaxAgeWithoutMutatingMemory(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
external_ip: 1.1.1.1
free_proxy_cache:
  enabled: true
  path: .cache/free-proxies.txt
  refresh_on_start: true
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
	originalCache := cfg.FreeProxyCache
	server := &Server{cfgSrc: cfg}

	body := []byte(`{
		"external_ip": "2.2.2.2",
		"free_proxy_cache": {
			"enabled": true,
			"path": ".cache/other.txt",
			"refresh_on_start": false,
			"auto_reload": false,
			"workers": 9,
			"max_age": "bad-duration"
		}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body = %s", rec.Code, rec.Body.String())
	}
	if server.cfgSrc.ExternalIP != "1.1.1.1" || server.cfgSrc.FreeProxyCache.Path != originalCache.Path || server.cfgSrc.FreeProxyCache.Workers != originalCache.Workers || server.cfgSrc.FreeProxyCache.MaxAge != originalCache.MaxAge {
		t.Fatalf("invalid free proxy cache should not mutate memory: external_ip=%q cache=%#v original=%#v", server.cfgSrc.ExternalIP, server.cfgSrc.FreeProxyCache, originalCache)
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ExternalIP != "1.1.1.1" || reloaded.FreeProxyCache.Path != originalCache.Path || reloaded.FreeProxyCache.Workers != originalCache.Workers || reloaded.FreeProxyCache.MaxAge != originalCache.MaxAge {
		t.Fatalf("invalid free proxy cache should not be persisted: external_ip=%q cache=%#v original=%#v", reloaded.ExternalIP, reloaded.FreeProxyCache, originalCache)
	}
}

func TestHandleSettingsPreservesFreeProxyCacheFieldsWhenPartiallyUpdated(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
free_proxy_cache:
  enabled: true
  path: .cache/free-proxies.txt
  refresh_on_start: true
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
	server := &Server{cfgSrc: cfg, reloadState: "idle"}

	body := []byte(`{"free_proxy_cache":{"enabled":false}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	c := reloaded.FreeProxyCache
	if c.EnabledValue() || c.Path != filepath.Join(tmp, ".cache", "free-proxies.txt") || !c.RefreshOnStartValue() || !c.AutoReloadValue() || c.Workers != 4 || c.MaxAge != 6*time.Hour {
		t.Fatalf("free_proxy_cache partial update corrupted fields: %#v", c)
	}
}

func TestHandleSettingsRejectsInvalidFreeProxyCacheWorkersWithoutMutatingMemory(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
external_ip: 1.1.1.1
free_proxy_cache:
  enabled: true
  path: .cache/free-proxies.txt
  refresh_on_start: true
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
	originalCache := cfg.FreeProxyCache
	server := &Server{cfgSrc: cfg}

	for _, tc := range []struct {
		name    string
		workers int
	}{
		{name: "zero", workers: 0},
		{name: "negative", workers: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"external_ip":"2.2.2.2","free_proxy_cache":{"enabled":true,"path":".cache/other.txt","refresh_on_start":false,"auto_reload":false,"workers":` + strconv.Itoa(tc.workers) + `,"max_age":"6h"}}`
			req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
			rec := httptest.NewRecorder()

			server.handleSettings(rec, req)

			assertSettingsErrorCode(t, rec, http.StatusBadRequest, "invalid_free_proxy_cache_workers")
			if server.cfgSrc.ExternalIP != "1.1.1.1" || server.cfgSrc.FreeProxyCache.Path != originalCache.Path || server.cfgSrc.FreeProxyCache.Workers != originalCache.Workers || server.cfgSrc.FreeProxyCache.MaxAge != originalCache.MaxAge {
				t.Fatalf("invalid free proxy cache should not mutate memory: external_ip=%q cache=%#v original=%#v", server.cfgSrc.ExternalIP, server.cfgSrc.FreeProxyCache, originalCache)
			}
			reloaded, err := config.Load(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if reloaded.ExternalIP != "1.1.1.1" || reloaded.FreeProxyCache.Path != originalCache.Path || reloaded.FreeProxyCache.Workers != originalCache.Workers || reloaded.FreeProxyCache.MaxAge != originalCache.MaxAge {
				t.Fatalf("invalid free proxy cache should not be persisted: external_ip=%q cache=%#v original=%#v", reloaded.ExternalIP, reloaded.FreeProxyCache, originalCache)
			}
		})
	}
}

func TestHandleSettingsRejectsNegativeFreeProxyMaxNodesBeforePersisting(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
external_ip: 1.1.1.1
free_proxy_max_nodes: 10
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg}

	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"external_ip":"2.2.2.2","free_proxy_max_nodes":-1}`))
	rec := httptest.NewRecorder()

	server.handleSettings(rec, req)

	assertSettingsErrorCode(t, rec, http.StatusBadRequest, "invalid_free_proxy_max_nodes")
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ExternalIP != "1.1.1.1" || reloaded.FreeProxyMaxNodes != 10 {
		t.Fatalf("invalid free proxy max nodes should not be persisted: external_ip=%q max=%d", reloaded.ExternalIP, reloaded.FreeProxyMaxNodes)
	}
}

func TestHandleSettingsRejectsInvalidFreeProxySourceNumbersBeforePersisting(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
external_ip: 1.1.1.1
free_proxy_max_nodes: 10
free_proxy_sources:
  - name: good-source
    url: https://example.test/proxies.txt
    format: txt
    timeout: 5s
    max_nodes: 100
    max_bytes: 1024
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg}

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "zero timeout", body: `{"external_ip":"2.2.2.2","free_proxy_sources":[{"name":"bad-source","url":"https://example.test/proxies.txt","format":"txt","timeout":"0s","max_nodes":100,"max_bytes":1024}]}`},
		{name: "negative timeout", body: `{"external_ip":"2.2.2.2","free_proxy_sources":[{"name":"bad-source","url":"https://example.test/proxies.txt","format":"txt","timeout":"-1s","max_nodes":100,"max_bytes":1024}]}`},
		{name: "negative max nodes", body: `{"external_ip":"2.2.2.2","free_proxy_sources":[{"name":"bad-source","url":"https://example.test/proxies.txt","format":"txt","timeout":"5s","max_nodes":-1,"max_bytes":1024}]}`},
		{name: "negative max bytes", body: `{"external_ip":"2.2.2.2","free_proxy_sources":[{"name":"bad-source","url":"https://example.test/proxies.txt","format":"txt","timeout":"5s","max_nodes":100,"max_bytes":-1}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			server.handleSettings(rec, req)

			assertSettingsErrorCode(t, rec, http.StatusBadRequest, "invalid_free_proxy_source")
			reloaded, err := config.Load(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if reloaded.ExternalIP != "1.1.1.1" || reloaded.FreeProxyMaxNodes != 10 || len(reloaded.FreeProxySources) != 1 || reloaded.FreeProxySources[0].Name != "good-source" {
				t.Fatalf("invalid free proxy source numbers should not be persisted: external_ip=%q max=%d sources=%#v", reloaded.ExternalIP, reloaded.FreeProxyMaxNodes, reloaded.FreeProxySources)
			}
		})
	}
}

func TestHandleSettingsRejectsInvalidFreeProxySourceURLBeforePersisting(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
free_proxy_sources:
  - name: old
    url: https://example.test/proxies.txt
    enabled: true
free_proxy_max_nodes: 10
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
			{"name":"bad","url":"not-a-url","default_scheme":"http","format":"text","enabled":true}
		],
		"free_proxy_max_nodes": 123
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body = %s", rec.Code, rec.Body.String())
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.FreeProxyMaxNodes != 10 || len(reloaded.FreeProxySources) != 1 || reloaded.FreeProxySources[0].URL != "https://example.test/proxies.txt" {
		t.Fatalf("invalid free proxy source should not be persisted: max=%d sources=%#v", reloaded.FreeProxyMaxNodes, reloaded.FreeProxySources)
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

func TestHandleSettingsPreservesQualityCheckFieldsWhenPartiallyUpdated(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
quality_check:
  enabled: true
  interval: 2h
  region: us
  count: 321
  include_unavailable: true
  retry_failed: true
  cloudflare_timeout: 6s
  cloudflare_concurrency: 32
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg, reloadState: "idle"}

	body := []byte(`{"quality_check":{"enabled":false}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	q := reloaded.QualityCheck
	if q.Enabled || q.Interval != 2*time.Hour || q.Region != "us" || q.Count != 321 || !q.IncludeUnavailable || !q.RetryFailed || q.CloudflareTimeout != 6*time.Second || q.CloudflareConcurrency != 32 {
		t.Fatalf("quality_check partial update corrupted fields: %#v", q)
	}
}

func TestHandleSettingsRejectsInvalidQualityRegionBeforePersisting(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	listen := freeLocalListen(t)
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
external_ip: 1.1.1.1
management:
  listen: ` + listen + `
quality_check:
  enabled: true
  interval: 1h
  region: all
  count: 321
  include_unavailable: true
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

	body := `{"external_ip":"2.2.2.2","management":{"listen":"` + listen + `","password":""},"quality_check":{"enabled":true,"interval":"1h","region":"mars","count":321,"include_unavailable":true,"cloudflare_timeout":"5s","cloudflare_concurrency":24}}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleSettings(rec, req)

	assertSettingsErrorCode(t, rec, http.StatusBadRequest, "invalid_quality_region")
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	q := reloaded.QualityCheck.Normalized()
	if reloaded.ExternalIP != "1.1.1.1" || q.Region != "all" {
		t.Fatalf("invalid quality region should not be persisted: external_ip=%q quality=%#v", reloaded.ExternalIP, q)
	}
}

func TestHandleSettingsRejectsInvalidQualityNumbersBeforePersisting(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	listen := freeLocalListen(t)
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
management:
  listen: ` + listen + `
quality_check:
  enabled: true
  interval: 1h
  region: all
  count: 321
  include_unavailable: true
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

	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{name: "zero count", body: `{"management":{"listen":"` + listen + `","password":""},"quality_check":{"enabled":true,"interval":"1h","region":"all","count":0,"include_unavailable":true,"cloudflare_timeout":"5s","cloudflare_concurrency":24}}`, code: "invalid_quality_count"},
		{name: "negative count", body: `{"management":{"listen":"` + listen + `","password":""},"quality_check":{"enabled":true,"interval":"1h","region":"all","count":-1,"include_unavailable":true,"cloudflare_timeout":"5s","cloudflare_concurrency":24}}`, code: "invalid_quality_count"},
		{name: "zero concurrency", body: `{"management":{"listen":"` + listen + `","password":""},"quality_check":{"enabled":true,"interval":"1h","region":"all","count":321,"include_unavailable":true,"cloudflare_timeout":"5s","cloudflare_concurrency":0}}`, code: "invalid_cloudflare_concurrency"},
		{name: "negative concurrency", body: `{"management":{"listen":"` + listen + `","password":""},"quality_check":{"enabled":true,"interval":"1h","region":"all","count":321,"include_unavailable":true,"cloudflare_timeout":"5s","cloudflare_concurrency":-1}}`, code: "invalid_cloudflare_concurrency"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			server.handleSettings(rec, req)

			assertSettingsErrorCode(t, rec, http.StatusBadRequest, tc.code)
			reloaded, err := config.Load(configPath)
			if err != nil {
				t.Fatal(err)
			}
			q := reloaded.QualityCheck.Normalized()
			if q.Count != 321 || q.CloudflareConcurrency != 24 {
				t.Fatalf("invalid quality numbers should not be persisted: count=%d concurrency=%d", q.Count, q.CloudflareConcurrency)
			}
		})
	}
}

func TestHandleSettingsRejectsNonPositiveCoreDurationsBeforePersisting(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	listen := freeLocalListen(t)
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
external_ip: 1.1.1.1
management:
  listen: ` + listen + `
pool:
  mode: round_robin
  failure_threshold: 3
  blacklist_duration: 5m
geoip:
  enabled: true
  database_path: /tmp/GeoLite2.mmdb
  listen: 127.0.0.1
  port: 1221
  auto_update_enabled: true
  auto_update_interval: 24h
quality_check:
  enabled: true
  interval: 1h
  region: all
  count: 321
  include_unavailable: true
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

	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{name: "zero pool blacklist", body: `{"external_ip":"2.2.2.2","pool":{"mode":"least_failures","failure_threshold":9,"blacklist_duration":"0s"}}`, code: "invalid_pool_blacklist_duration"},
		{name: "negative pool blacklist", body: `{"external_ip":"2.2.2.2","pool":{"mode":"least_failures","failure_threshold":9,"blacklist_duration":"-1s"}}`, code: "invalid_pool_blacklist_duration"},
		{name: "zero geoip interval", body: `{"external_ip":"2.2.2.2","geoip":{"enabled":true,"database_path":"/tmp/other.mmdb","listen":"127.0.0.1","port":1222,"auto_update_enabled":false,"auto_update_interval":"0s"}}`, code: "invalid_geoip_auto_update_interval"},
		{name: "negative geoip interval", body: `{"external_ip":"2.2.2.2","geoip":{"enabled":true,"database_path":"/tmp/other.mmdb","listen":"127.0.0.1","port":1222,"auto_update_enabled":false,"auto_update_interval":"-1s"}}`, code: "invalid_geoip_auto_update_interval"},
		{name: "zero quality interval", body: `{"management":{"listen":"` + listen + `","password":""},"quality_check":{"enabled":true,"interval":"0s","region":"all","count":321,"include_unavailable":true,"cloudflare_timeout":"5s","cloudflare_concurrency":24}}`, code: "invalid_quality_interval"},
		{name: "negative quality interval", body: `{"management":{"listen":"` + listen + `","password":""},"quality_check":{"enabled":true,"interval":"-1s","region":"all","count":321,"include_unavailable":true,"cloudflare_timeout":"5s","cloudflare_concurrency":24}}`, code: "invalid_quality_interval"},
		{name: "zero cloudflare timeout", body: `{"management":{"listen":"` + listen + `","password":""},"quality_check":{"enabled":true,"interval":"1h","region":"all","count":321,"include_unavailable":true,"cloudflare_timeout":"0s","cloudflare_concurrency":24}}`, code: "invalid_cloudflare_timeout"},
		{name: "negative cloudflare timeout", body: `{"management":{"listen":"` + listen + `","password":""},"quality_check":{"enabled":true,"interval":"1h","region":"all","count":321,"include_unavailable":true,"cloudflare_timeout":"-1s","cloudflare_concurrency":24}}`, code: "invalid_cloudflare_timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			server.handleSettings(rec, req)

			assertSettingsErrorCode(t, rec, http.StatusBadRequest, tc.code)
			reloaded, err := config.Load(configPath)
			if err != nil {
				t.Fatal(err)
			}
			q := reloaded.QualityCheck.Normalized()
			if reloaded.ExternalIP != "1.1.1.1" || reloaded.Pool.BlacklistDuration != 5*time.Minute || reloaded.GeoIP.AutoUpdateInterval != 24*time.Hour || q.Interval != time.Hour || q.CloudflareTimeout != 5*time.Second {
				t.Fatalf("invalid duration should not be persisted: external_ip=%q pool=%#v geoip=%#v quality=%#v", reloaded.ExternalIP, reloaded.Pool, reloaded.GeoIP, q)
			}
		})
	}
}

func TestHandleSettingsRejectsInvalidQualityDurationBeforePersisting(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	listen := freeLocalListen(t)
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
management:
  listen: ` + listen + `
quality_check:
  enabled: true
  interval: 1h
  region: all
  count: 500
  include_unavailable: true
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
			"enabled": true,
			"interval": "1h",
			"region": "all",
			"count": 500,
			"include_unavailable": true,
			"cloudflare_timeout": "bad-duration",
			"cloudflare_concurrency": 32
		}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.handleSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body = %s", rec.Code, rec.Body.String())
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	q := reloaded.QualityCheck.Normalized()
	if q.CloudflareTimeout != 5*time.Second || q.CloudflareConcurrency != 24 {
		t.Fatalf("invalid quality settings should not be persisted: timeout=%s concurrency=%d", q.CloudflareTimeout, q.CloudflareConcurrency)
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

func TestHandleSettingsAcceptsRoundTripZeroDurationDefaults(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.SetFilePath(configPath)
	cfg.Log.Output = "stdout"
	cfg.Log.MaxSize = 50
	cfg.Log.MaxBackups = 3
	cfg.Log.MaxAge = 7
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
}

func TestHandleSettingsAcceptsRoundTripFreeProxySourceDefaultTimeout(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	freeSourcePath := filepath.Join(tmp, "free.txt")
	if err := os.WriteFile(freeSourcePath, []byte("127.0.0.1:18081\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.SetFilePath(configPath)
	cfg.Mode = "pool"
	cfg.Log.Output = "stdout"
	cfg.Log.MaxSize = 50
	cfg.Log.MaxBackups = 3
	cfg.Log.MaxAge = 7
	cfg.Pool.Mode = "sequential"
	cfg.Pool.FailureThreshold = 3
	cfg.Pool.BlacklistDuration = time.Hour
	cfg.FreeProxyFilter = cfg.FreeProxyFilter.Normalized()
	cfg.FreeProxySources = []nodesource.SourceConfig{{
		Name:    "local-free",
		File:    freeSourcePath,
		Format:  "txt",
		Timeout: 0,
	}}
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

func TestHandleSettingsRejectsInvalidCoreEnumsAndNumbersBeforePersisting(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`mode: hybrid
nodes:
  - name: base
    uri: http://127.0.0.1:18080
external_ip: 1.1.1.1
pool:
  mode: round_robin
  failure_threshold: 3
  blacklist_duration: 5m
log:
  output: stdout
  max_size: 100
  max_backups: 3
  max_age: 7
  compress: false
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg}

	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{name: "bad mode", body: `{"mode":"bad","external_ip":"2.2.2.2"}`, code: "invalid_mode"},
		{name: "bad pool mode", body: `{"external_ip":"2.2.2.2","pool":{"mode":"bad","failure_threshold":3,"blacklist_duration":"5m"}}`, code: "invalid_pool_mode"},
		{name: "zero pool threshold", body: `{"external_ip":"2.2.2.2","pool":{"mode":"round_robin","failure_threshold":0,"blacklist_duration":"5m"}}`, code: "invalid_pool_failure_threshold"},
		{name: "negative pool threshold", body: `{"external_ip":"2.2.2.2","pool":{"mode":"round_robin","failure_threshold":-1,"blacklist_duration":"5m"}}`, code: "invalid_pool_failure_threshold"},
		{name: "zero log max size", body: `{"external_ip":"2.2.2.2","log":{"output":"stdout","max_size":0,"max_backups":3,"max_age":7,"compress":false}}`, code: "invalid_log_max_size"},
		{name: "zero log max backups", body: `{"external_ip":"2.2.2.2","log":{"output":"stdout","max_size":100,"max_backups":0,"max_age":7,"compress":false}}`, code: "invalid_log_max_backups"},
		{name: "zero log max age", body: `{"external_ip":"2.2.2.2","log":{"output":"stdout","max_size":100,"max_backups":3,"max_age":0,"compress":false}}`, code: "invalid_log_max_age"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			server.handleSettings(rec, req)

			assertSettingsErrorCode(t, rec, http.StatusBadRequest, tc.code)
			reloaded, err := config.Load(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if reloaded.Mode != "hybrid" || reloaded.ExternalIP != "1.1.1.1" || reloaded.Pool.Mode != "round_robin" || reloaded.Pool.FailureThreshold != 3 || reloaded.Log.MaxSize != 100 || reloaded.Log.MaxBackups != 3 || reloaded.Log.MaxAge != 7 {
				t.Fatalf("invalid core setting should not be persisted: mode=%q external_ip=%q pool=%#v log=%#v", reloaded.Mode, reloaded.ExternalIP, reloaded.Pool, reloaded.Log)
			}
		})
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

func TestHandleSettingsPartialPutPreservesBooleanAndNestedConfig(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	listen := freeLocalListen(t)
	initial := []byte(`skip_cert_verify: true
external_ip: 9.9.9.9
management:
  listen: ` + listen + `
  probe_target: http://cp.cloudflare.com/generate_204
geoip:
  enabled: true
  database_path: ./GeoLite2-Country.mmdb
  listen: 127.0.0.1
  port: 1221
  auto_update_enabled: true
  auto_update_interval: 24h
nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg, reloadState: "idle"}

	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader([]byte(`{"external_ip":"1.2.3.4"}`)))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ExternalIP != "1.2.3.4" {
		t.Fatalf("external_ip = %q, want updated", reloaded.ExternalIP)
	}
	if !reloaded.SkipCertVerify {
		t.Fatalf("partial PUT cleared skip_cert_verify")
	}
	if !reloaded.GeoIP.Enabled || reloaded.GeoIP.DatabasePath == "" || reloaded.GeoIP.Port != 1221 {
		t.Fatalf("partial PUT corrupted geoip config: %#v", reloaded.GeoIP)
	}
}

func TestHandleSettingsPreservesPoolFieldsWhenPartiallyUpdated(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`pool:
  mode: round_robin
  failure_threshold: 3
  blacklist_duration: 5m
nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg, reloadState: "idle"}

	body := []byte(`{"pool":{"mode":"least_failures"}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Pool.Mode != "least_failures" || reloaded.Pool.FailureThreshold != 3 || reloaded.Pool.BlacklistDuration != 5*time.Minute {
		t.Fatalf("pool partial update corrupted fields: %#v", reloaded.Pool)
	}
}

func TestHandleSettingsPreservesLogFieldsWhenPartiallyUpdated(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`log:
  output: file
  max_size: 100
  max_backups: 3
  max_age: 7
  compress: true
nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg, reloadState: "idle"}

	body := []byte(`{"log":{"output":"stdout"}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Log.Output != "stdout" || reloaded.Log.MaxSize != 100 || reloaded.Log.MaxBackups != 3 || reloaded.Log.MaxAge != 7 || !reloaded.Log.Compress {
		t.Fatalf("log partial update corrupted fields: %#v", reloaded.Log)
	}
}

func TestHandleSettingsPreservesGeoIPFieldsWhenPartiallyUpdated(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`geoip:
  enabled: true
  database_path: ./GeoLite2-Country.mmdb
  listen: 127.0.0.1
  port: 1221
  auto_update_enabled: true
  auto_update_interval: 24h
nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg, reloadState: "idle"}

	body := []byte(`{"geoip":{"enabled":false,"database_path":"./GeoLite2-Updated.mmdb"}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.GeoIP.Enabled || reloaded.GeoIP.DatabasePath != "./GeoLite2-Updated.mmdb" || reloaded.GeoIP.Listen != "127.0.0.1" || reloaded.GeoIP.Port != 1221 || !reloaded.GeoIP.AutoUpdateEnabled || reloaded.GeoIP.AutoUpdateInterval != 24*time.Hour {
		t.Fatalf("geoip partial update corrupted fields: %#v", reloaded.GeoIP)
	}
}

func TestHandleSettingsPreservesGeoIPEnabledWhenOmitted(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`geoip:
  enabled: true
  database_path: ./GeoLite2-Country.mmdb
  listen: 127.0.0.1
  port: 1221
  auto_update_enabled: true
  auto_update_interval: 24h
nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg, reloadState: "idle"}

	body := []byte(`{"geoip":{"database_path":"./GeoLite2-Updated.mmdb"}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.GeoIP.Enabled || reloaded.GeoIP.DatabasePath != "./GeoLite2-Updated.mmdb" || reloaded.GeoIP.Listen != "127.0.0.1" || reloaded.GeoIP.Port != 1221 || !reloaded.GeoIP.AutoUpdateEnabled || reloaded.GeoIP.AutoUpdateInterval != 24*time.Hour {
		t.Fatalf("geoip partial update should preserve enabled: %#v", reloaded.GeoIP)
	}
}

func TestHandleSettingsPreservesListenerPortsWhenCredentialsOnly(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	listen := freeLocalListen(t)
	initial := []byte(`mode: hybrid
listener:
  address: 127.0.0.1
  port: 18080
  username: old-listener-user
  password: old-listener-pass
multi_port:
  address: 127.0.0.1
  base_port: 35000
  username: old-multi-user
  password: old-multi-pass
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
	server := &Server{cfgSrc: cfg}

	body := []byte(`{
		"listener": {"username":"new-listener-user","password":"new-listener-pass"},
		"multi_port": {"username":"new-multi-user","password":"new-multi-pass"}
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Listener.Address != "127.0.0.1" || reloaded.Listener.Port != 18080 || reloaded.Listener.Username != "new-listener-user" || reloaded.Listener.Password != "new-listener-pass" {
		t.Fatalf("listener credential-only update corrupted listener: %#v", reloaded.Listener)
	}
	if reloaded.MultiPort.Address != "127.0.0.1" || reloaded.MultiPort.BasePort != 35000 || reloaded.MultiPort.Username != "new-multi-user" || reloaded.MultiPort.Password != "new-multi-pass" {
		t.Fatalf("multi_port credential-only update corrupted multi_port: %#v", reloaded.MultiPort)
	}
}

func TestHandleSettingsPersistsAndroidProxyFields(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`android_proxy:
  enabled: true
  listen: 127.0.0.1
  base_port: 13001
  region_ports:
    US: 13010
nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg, reloadState: "idle"}

	body := []byte(`{"android_proxy":{"listen":"0.0.0.0","base_port":14001}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.AndroidProxy.Enabled || reloaded.AndroidProxy.Listen != "0.0.0.0" || reloaded.AndroidProxy.BasePort != 14001 || reloaded.AndroidProxy.RegionPorts["US"] != 13010 {
		t.Fatalf("android proxy update not persisted or corrupted fields: %#v", reloaded.AndroidProxy)
	}
}

func TestHandleSettingsPersistsSubscriptionsFields(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`subscriptions:
  - https://example.test/sub-a
subscription_refresh:
  enabled: true
  interval: 1h
nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	refresher := &recordingSubscriptionRefresher{status: SubscriptionStatus{NodeCount: 1}, updateRefreshStarted: make(chan struct{}, 1)}
	server := &Server{cfgSrc: cfg, reloadState: "idle"}
	server.SetSubscriptionRefresher(refresher)

	body := []byte(`{"subscriptions":["https://example.test/sub-b","  ","https://example.test/sub-c"],"subscription_refresh":{"enabled":true,"interval":"1h"}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		NeedReload                 bool `json:"need_reload"`
		SubscriptionRefreshStarted bool `json:"subscription_refresh_started"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.NeedReload {
		t.Fatalf("subscription URL settings should not require core reload: body=%s", rec.Body.String())
	}
	if !resp.SubscriptionRefreshStarted {
		t.Fatalf("subscription URL settings should report background refresh start: body=%s", rec.Body.String())
	}
	select {
	case <-refresher.updateRefreshStarted:
	case <-time.After(time.Second):
		t.Fatalf("settings save should start background subscription refresh")
	}
	if refresher.updateCalls != 0 || refresher.updateRefreshCalls != 1 {
		t.Fatalf("settings save should refresh changed subscription URLs in background: update=%d refresh=%d", refresher.updateCalls, refresher.updateRefreshCalls)
	}
	if got := strings.Join(refresher.lastURLs, ","); got != "https://example.test/sub-b,https://example.test/sub-c" {
		t.Fatalf("refresher URLs = %q", got)
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Subscriptions) != 2 || reloaded.Subscriptions[0] != "https://example.test/sub-b" || reloaded.Subscriptions[1] != "https://example.test/sub-c" {
		t.Fatalf("subscriptions update not persisted: %#v", reloaded.Subscriptions)
	}
}

func TestHandleSettingsPersistsSubscriptionRefreshFields(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`subscription_refresh:
  enabled: true
  interval: 1h
nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Subscriptions = []string{"https://example.test/sub-a"}
	server := &Server{cfgSrc: cfg, reloadState: "idle"}

	body := []byte(`{"subscription_refresh":{"enabled":false,"interval":"2h"}}`)
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
	if resp.NeedReload {
		t.Fatalf("subscription refresh settings should not require core reload: body=%s", rec.Body.String())
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.SubscriptionRefresh.Enabled || reloaded.SubscriptionRefresh.Interval != 2*time.Hour || len(reloaded.Subscriptions) != 1 || reloaded.Subscriptions[0] != "https://example.test/sub-a" {
		t.Fatalf("subscription refresh update not persisted or corrupted fields: refresh=%#v subscriptions=%#v", reloaded.SubscriptionRefresh, reloaded.Subscriptions)
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

func TestHandleFreeProxyRefreshReportsDisabledState(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	cachePath := filepath.Join(tmp, "cache.txt")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
free_proxy_cache:
  enabled: false
  path: ` + cachePath + `
  auto_reload: true
free_proxy_sources:
  - name: disabled-source
    url: http://127.0.0.1:1/free.txt
    enabled: false
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
		Message string                 `json:"message"`
		Started bool                   `json:"started"`
		Status  freeProxyRefreshStatus `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Started || resp.Status.State != "disabled" {
		t.Fatalf("expected disabled refresh response, got %#v body=%s", resp, rec.Body.String())
	}
	if strings.Contains(resp.Message, "运行") {
		t.Fatalf("disabled refresh should not claim a job is running: %#v body=%s", resp, rec.Body.String())
	}
	if resp.Status.TotalSources != 1 || resp.Status.EnabledSources != 0 || resp.Status.CacheEnabled {
		t.Fatalf("disabled refresh response should include config context: %#v body=%s", resp.Status, rec.Body.String())
	}
}

func TestHandleFreeProxyRefreshReturnsStructuredError(t *testing.T) {
	server := &Server{}

	req := httptest.NewRequest(http.MethodPost, "/api/free-proxy/refresh", nil)
	rec := httptest.NewRecorder()
	server.handleFreeProxyRefresh(rec, req)

	assertSettingsErrorCode(t, rec, http.StatusServiceUnavailable, "free_proxy_refresh_unavailable")
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

func TestHandleConfigNodesRejectsInvalidNodeWithStructuredCode(t *testing.T) {
	fake := &fakeNodeManager{createErr: fmt.Errorf("%w: URI 格式无效", ErrInvalidNode)}
	server := &Server{nodeMgr: fake}

	body := []byte(`{"name":"bad","uri":"not-a-uri","port":18080}`)
	rec := httptest.NewRecorder()
	server.handleConfigNodes(rec, httptest.NewRequest(http.MethodPost, "/api/nodes/config", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=400 body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Code != "invalid_node" || resp.Error == "" {
		t.Fatalf("unexpected response: %#v body=%s", resp, rec.Body.String())
	}
	if len(fake.created) != 0 || len(fake.nodes) != 0 {
		t.Fatalf("invalid node should not be added: created=%#v nodes=%#v", fake.created, fake.nodes)
	}
}

func TestHandleConfigNodesReturnsStructuredErrorCodes(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "invalid", err: ErrInvalidNode, status: http.StatusBadRequest, code: "invalid_node"},
		{name: "conflict", err: ErrNodeConflict, status: http.StatusBadRequest, code: "node_conflict"},
		{name: "missing", err: ErrNodeNotFound, status: http.StatusNotFound, code: "node_not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := &Server{}
			rec := httptest.NewRecorder()
			server.respondNodeError(rec, tc.err)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
			}
			var body struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error == "" || body.Code != tc.code {
				t.Fatalf("unexpected body: %#v raw=%s", body, rec.Body.String())
			}
		})
	}
}

func TestHandleConfigNodeHandlersReturnStructuredRequestErrors(t *testing.T) {
	cases := []struct {
		name    string
		server  *Server
		handler func(*Server, *httptest.ResponseRecorder)
		status  int
		code    string
	}{
		{
			name:   "manager disabled",
			server: &Server{},
			handler: func(s *Server, rec *httptest.ResponseRecorder) {
				s.handleConfigNodes(rec, httptest.NewRequest(http.MethodGet, "/api/nodes/config", nil))
			},
			status: http.StatusServiceUnavailable,
			code:   "node_manager_disabled",
		},
		{
			name:   "create bad json",
			server: &Server{nodeMgr: &fakeNodeManager{}},
			handler: func(s *Server, rec *httptest.ResponseRecorder) {
				s.handleConfigNodes(rec, httptest.NewRequest(http.MethodPost, "/api/nodes/config", bytes.NewReader([]byte(`{`))))
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		{
			name:   "invalid node name",
			server: &Server{nodeMgr: &fakeNodeManager{}},
			handler: func(s *Server, rec *httptest.ResponseRecorder) {
				s.handleConfigNodeItem(rec, httptest.NewRequest(http.MethodPut, "/api/nodes/config/", nil))
			},
			status: http.StatusBadRequest,
			code:   "invalid_node_name",
		},
		{
			name:   "update bad json",
			server: &Server{nodeMgr: &fakeNodeManager{}},
			handler: func(s *Server, rec *httptest.ResponseRecorder) {
				s.handleConfigNodeItem(rec, httptest.NewRequest(http.MethodPut, "/api/nodes/config/node-a", bytes.NewReader([]byte(`{`))))
			},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			tc.handler(tc.server, rec)

			assertSettingsErrorCode(t, rec, tc.status, tc.code)
		})
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

func TestSubscriptionConfigRejectsTrailingJSON(t *testing.T) {
	server := &Server{}
	req := httptest.NewRequest(http.MethodPut, "/api/subscription/config", bytes.NewReader([]byte(`{"subscriptions":[],"enabled":false,"interval":"10m"}{"extra":true}`)))
	rec := httptest.NewRecorder()

	server.handleSubscriptionConfig(rec, req)

	assertSettingsErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
}

func TestHandleSettingsPreservesManagementListenWhenOmitted(t *testing.T) {
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
	if err := os.WriteFile(configPath, []byte("nodes:\n  - name: base\n    uri: http://127.0.0.1:18080\nmanagement:\n  listen: 127.0.0.1:0\n  password: old-secret\n"), 0o644); err != nil {
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
	originalManagementListen := cfg.Management.Listen
	t.Cleanup(func() { srv.Shutdown(context.Background()) })

	body := []byte(`{"management":{"password":"new-secret"}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	req.Host = oldListen
	rec := httptest.NewRecorder()
	srv.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Management.Listen != originalManagementListen || reloaded.Management.Password != "new-secret" {
		t.Fatalf("management partial update should preserve listen=%q and update password: %#v", originalManagementListen, reloaded.Management)
	}
}

func TestHandleSettingsPreservesManagementPasswordWhenOmitted(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	listen := freeLocalListen(t)
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
management:
  listen: ` + listen + `
  password: old-secret
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfgSrc: cfg, cfg: Config{Listen: listen}, reloadState: "idle"}

	body := []byte(`{"management":{"listen":"` + listen + `"}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Management.Listen != listen || reloaded.Management.Password != "old-secret" {
		t.Fatalf("management partial update should preserve password: %#v", reloaded.Management)
	}
}

func TestHandleSettingsRejectsInvalidManagementListenBeforePersisting(t *testing.T) {
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

	body := []byte(`{"management":{"listen":"127.0.0.1:-1","password":""},"external_ip":"2.2.2.2"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	req.Host = oldListen
	rec := httptest.NewRecorder()
	srv.handleSettings(rec, req)

	assertSettingsErrorCode(t, rec, http.StatusBadRequest, "management_rebind_failed")
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Management.Listen == "127.0.0.1:-1" || reloaded.ExternalIP == "2.2.2.2" {
		t.Fatalf("failed management rebind should not persist settings: listen=%q external_ip=%q", reloaded.Management.Listen, reloaded.ExternalIP)
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

func TestHandleSubscriptionRefreshDisabledReturnsStructuredError(t *testing.T) {
	server := &Server{}

	req := httptest.NewRequest(http.MethodPost, "/api/subscription/refresh", nil)
	rec := httptest.NewRecorder()
	server.handleSubscriptionRefresh(rec, req)

	assertSettingsErrorCode(t, rec, http.StatusServiceUnavailable, "subscription_refresh_disabled")
}

func TestHandleSubscriptionConfigReturnsStructuredErrorCodes(t *testing.T) {
	server := &Server{}

	req := httptest.NewRequest(http.MethodPut, "/api/subscription/config", bytes.NewReader([]byte(`{`)))
	rec := httptest.NewRecorder()
	server.handleSubscriptionConfig(rec, req)

	assertSettingsErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
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

func TestHandleSubscriptionConfigRejectsInvalidIntervalBeforePersisting(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
subscriptions:
  - https://example.test/sub-a
subscription_refresh:
  enabled: true
  interval: 2h
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

	body := []byte(`{"subscriptions":["https://example.test/sub-b"],"enabled":false,"interval":"bad-duration"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/subscription/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSubscriptionConfig(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 body=%s", rec.Code, rec.Body.String())
	}
	if refresher.updateCalls != 0 || refresher.updateRefreshCalls != 0 {
		t.Fatalf("invalid config should not update refresher: update=%d refresh=%d", refresher.updateCalls, refresher.updateRefreshCalls)
	}
	if server.cfgSrc.SubscriptionRefresh.Interval != 2*time.Hour || !server.cfgSrc.SubscriptionRefresh.Enabled || len(server.cfgSrc.Subscriptions) != 1 || server.cfgSrc.Subscriptions[0] != "https://example.test/sub-a" {
		t.Fatalf("invalid interval should not mutate memory: enabled=%v interval=%s subscriptions=%#v", server.cfgSrc.SubscriptionRefresh.Enabled, server.cfgSrc.SubscriptionRefresh.Interval, server.cfgSrc.Subscriptions)
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.SubscriptionRefresh.Interval != 2*time.Hour || !reloaded.SubscriptionRefresh.Enabled || len(reloaded.Subscriptions) != 1 || reloaded.Subscriptions[0] != "https://example.test/sub-a" {
		t.Fatalf("invalid interval should not be persisted: enabled=%v interval=%s subscriptions=%#v", reloaded.SubscriptionRefresh.Enabled, reloaded.SubscriptionRefresh.Interval, reloaded.Subscriptions)
	}
}

func TestHandleSubscriptionConfigRejectsTooShortIntervalBeforePersisting(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
subscriptions:
  - https://example.test/sub-a
subscription_refresh:
  enabled: true
  interval: 2h
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

	body := []byte(`{"subscriptions":["https://example.test/sub-b"],"enabled":false,"interval":"1m"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/subscription/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSubscriptionConfig(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 body=%s", rec.Code, rec.Body.String())
	}
	if refresher.updateCalls != 0 || refresher.updateRefreshCalls != 0 {
		t.Fatalf("invalid config should not update refresher: update=%d refresh=%d", refresher.updateCalls, refresher.updateRefreshCalls)
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.SubscriptionRefresh.Interval != 2*time.Hour || !reloaded.SubscriptionRefresh.Enabled || len(reloaded.Subscriptions) != 1 || reloaded.Subscriptions[0] != "https://example.test/sub-a" {
		t.Fatalf("too-short interval should not be persisted: enabled=%v interval=%s subscriptions=%#v", reloaded.SubscriptionRefresh.Enabled, reloaded.SubscriptionRefresh.Interval, reloaded.Subscriptions)
	}
}

func TestHandleSubscriptionConfigRejectsInvalidURLBeforePersisting(t *testing.T) {
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

	body := []byte(`{"subscriptions":["https://example.test/sub-a","not-a-url"],"enabled":true,"interval":"1h"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/subscription/config", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.handleSubscriptionConfig(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 body=%s", rec.Code, rec.Body.String())
	}
	if refresher.updateCalls != 0 || refresher.updateRefreshCalls != 0 {
		t.Fatalf("invalid config should not update refresher: update=%d refresh=%d", refresher.updateCalls, refresher.updateRefreshCalls)
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Subscriptions) != 1 || reloaded.Subscriptions[0] != "https://example.test/sub-a" {
		t.Fatalf("invalid config should not be persisted: %#v", reloaded.Subscriptions)
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
