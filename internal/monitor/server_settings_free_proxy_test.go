package monitor

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"easy_proxies/internal/config"
)

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
