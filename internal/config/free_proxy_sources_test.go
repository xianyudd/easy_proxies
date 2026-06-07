package config

import (
	"context"
	"fmt"

	"easy_proxies/internal/nodesource"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMergesFreeProxySourcesAfterInlineNodes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "free.txt"), []byte("1.2.3.4:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `mode: pool
nodes:
  - name: inline
    uri: http://9.9.9.9:9000
free_proxy_cache:
  enabled: false
free_proxy_sources:
  - name: local-free
    file: free.txt
    format: txt
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if len(cfg.Nodes) != 2 {
		t.Fatalf("expected inline + free proxy nodes, got %d: %#v", len(cfg.Nodes), cfg.Nodes)
	}
	if cfg.Nodes[0].Source != NodeSourceInline {
		t.Fatalf("inline source not preserved: %#v", cfg.Nodes[0])
	}
	if cfg.Nodes[1].URI != "http://1.2.3.4:8080" {
		t.Fatalf("unexpected free proxy uri: %#v", cfg.Nodes[1])
	}
	if cfg.Nodes[1].Source != NodeSourceFreeProxy {
		t.Fatalf("expected free proxy source, got %q", cfg.Nodes[1].Source)
	}
}

func TestLoadDefaultsToUnlimitedFreeProxyRuntimeActivation(t *testing.T) {
	dir := t.TempDir()
	lines := make([]string, 0, 3)
	for i := 1; i <= 3; i++ {
		lines = append(lines, fmt.Sprintf("http://10.20.0.%d:8080", i))
	}
	if err := os.WriteFile(filepath.Join(dir, "free.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `mode: pool
free_proxy_cache:
  enabled: false
free_proxy_sources:
  - name: local-free
    file: free.txt
    format: txt
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if cfg.FreeProxyMaxNodes != 0 {
		t.Fatalf("expected unset free_proxy_max_nodes to stay unlimited/0, got %d", cfg.FreeProxyMaxNodes)
	}
	if len(cfg.Nodes) != 3 {
		t.Fatalf("expected all free proxy nodes with default unlimited cap, got %d: %#v", len(cfg.Nodes), cfg.Nodes)
	}
}

func TestLoadAppliesFreeProxyCapsAndDedupeAfterSubscription(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "free.txt"), []byte(`
socks5://2.2.2.2:80
http://3.3.3.3:80
http://4.4.4.4:80
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("socks5://2.2.2.2:80\nsocks5://9.9.9.9:90\n"))
	}))
	defer sub.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	content := `mode: pool
nodes:
  - name: inline
    uri: http://1.1.1.1:80
subscriptions:
  - ` + sub.URL + `
free_proxy_max_nodes: 2
free_proxy_cache:
  enabled: false
free_proxy_sources:
  - name: local-free
    file: free.txt
    format: txt
    max_nodes: 10
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if len(cfg.Nodes) != 5 {
		t.Fatalf("expected inline + 2 subscription + 2 deduped free nodes, got %d: %#v", len(cfg.Nodes), cfg.Nodes)
	}
	if cfg.Nodes[1].Source != NodeSourceSubscription || cfg.Nodes[2].Source != NodeSourceSubscription {
		t.Fatalf("subscription source/order not preserved: %#v", cfg.Nodes)
	}
	if cfg.Nodes[3].URI != "http://3.3.3.3:80" || cfg.Nodes[4].URI != "http://4.4.4.4:80" {
		t.Fatalf("unexpected free nodes/order/dedupe: %#v", cfg.Nodes)
	}
	if cfg.Nodes[3].Source != NodeSourceFreeProxy || cfg.Nodes[4].Source != NodeSourceFreeProxy {
		t.Fatalf("free proxy source not marked: %#v", cfg.Nodes)
	}
}

func TestNormalizeFreeProxyCompositionIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	freeFile := filepath.Join(dir, "free.txt")
	if err := os.WriteFile(freeFile, []byte("1.1.1.1:80\n2.2.2.2:80\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		Mode:              "pool",
		FreeProxyMaxNodes: 1,
		FreeProxySources: []nodesource.SourceConfig{{
			Name: "local",
			File: freeFile,
		}},
	}
	if err := cfg.normalize(); err != nil {
		t.Fatalf("first normalize failed: %v", err)
	}
	if len(cfg.Nodes) != 1 {
		t.Fatalf("expected one free node, got %#v", cfg.Nodes)
	}
	if err := cfg.normalize(); err != nil {
		t.Fatalf("second normalize failed: %v", err)
	}
	if len(cfg.Nodes) != 1 {
		t.Fatalf("expected idempotent free composition, got %#v", cfg.Nodes)
	}
}

func TestLoadDoesNotRequestLaterFreeProxySourcesAfterGlobalCapFilled(t *testing.T) {
	secondRequests := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("http://10.0.0.1:8080\nhttp://10.0.0.2:8080\nhttp://10.0.0.3:8080\n"))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondRequests++
		_, _ = w.Write([]byte("http://10.0.1.1:8080\n"))
	}))
	defer second.Close()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `mode: pool
free_proxy_max_nodes: 2
free_proxy_cache:
  enabled: false
free_proxy_sources:
  - name: first
    url: ` + first.URL + `
    format: txt
    max_nodes: 100
  - name: second
    url: ` + second.URL + `
    format: txt
    max_nodes: 100
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if len(cfg.Nodes) != 2 {
		t.Fatalf("expected exactly 2 capped free nodes, got %d: %#v", len(cfg.Nodes), cfg.Nodes)
	}
	if cfg.Nodes[0].URI != "http://10.0.0.1:8080" || cfg.Nodes[1].URI != "http://10.0.0.2:8080" {
		t.Fatalf("unexpected capped free node order: %#v", cfg.Nodes)
	}
	if secondRequests != 0 {
		t.Fatalf("expected second source not to be requested after global cap filled, got %d requests", secondRequests)
	}
}

func TestLoadLimitedFetchesEnoughFreeProxyRowsToFillCapAfterDedupe(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `mode: pool
nodes:
  - name: existing
    uri: http://10.0.0.1:8080
free_proxy_max_nodes: 2
free_proxy_cache:
  enabled: false
free_proxy_sources:
  - name: local-free
    format: txt
    max_nodes: 100
    file: free.txt
`
	if err := os.WriteFile(filepath.Join(dir, "free.txt"), []byte("http://10.0.0.1:8080\nhttp://10.0.0.2:8080\nhttp://10.0.0.3:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if len(cfg.Nodes) != 3 {
		t.Fatalf("expected existing + 2 unique free nodes after dedupe, got %d: %#v", len(cfg.Nodes), cfg.Nodes)
	}
	if cfg.Nodes[1].URI != "http://10.0.0.2:8080" || cfg.Nodes[2].URI != "http://10.0.0.3:8080" {
		t.Fatalf("unexpected deduped capped nodes: %#v", cfg.Nodes)
	}
}

func TestLoadFiltersFreeProxySourcesBeforeAppending(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generate_204" {
			t.Fatalf("unexpected good probe path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `mode: pool
free_proxy_max_nodes: 10
free_proxy_filter:
  enabled: true
  min_tier: http_basic
  workers: 4
  timeout: 2s
  probes:
    http: /generate_204
free_proxy_cache:
  enabled: false
free_proxy_sources:
  - name: local-free
    file: free.txt
    format: txt
`
	freeContent := good.URL + "\n" + bad.URL + "\n"
	if err := os.WriteFile(filepath.Join(dir, "free.txt"), []byte(freeContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if len(cfg.Nodes) != 1 {
		t.Fatalf("expected only one filtered free node, got %d: %#v", len(cfg.Nodes), cfg.Nodes)
	}
	if cfg.Nodes[0].URI != good.URL {
		t.Fatalf("unexpected accepted proxy: %#v", cfg.Nodes[0])
	}
	if cfg.Nodes[0].Source != NodeSourceFreeProxy {
		t.Fatalf("accepted proxy not marked free_proxy: %#v", cfg.Nodes[0])
	}
}

func TestLoadUsesFreeProxyCacheWithoutRequestingRemoteSource(t *testing.T) {
	remoteRequests := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteRequests++
		_, _ = w.Write([]byte("http://10.10.10.10:8080\n"))
	}))
	defer remote.Close()

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "free-cache.txt")
	if err := os.WriteFile(cachePath, []byte("http://1.2.3.4:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `mode: pool
free_proxy_max_nodes: 5
free_proxy_cache:
  enabled: true
  path: free-cache.txt
free_proxy_sources:
  - name: remote
    url: ` + remote.URL + `
    format: txt
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if len(cfg.Nodes) != 1 || cfg.Nodes[0].URI != "http://1.2.3.4:8080" {
		t.Fatalf("expected cached free node only, got %#v", cfg.Nodes)
	}
	if remoteRequests != 0 {
		t.Fatalf("expected startup not to request remote source, got %d requests", remoteRequests)
	}
}

func TestRefreshFreeProxyCacheDetailedReportsPerSourceFailures(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.txt")
	cfg := &Config{
		filePath:          filepath.Join(dir, "config.yaml"),
		FreeProxyMaxNodes: 0,
		FreeProxySources: []nodesource.SourceConfig{
			{Name: "missing", File: filepath.Join(dir, "missing.txt"), Format: "txt"},
		},
		FreeProxyCache: FreeProxyCacheConfig{
			Path: cachePath,
		},
		FreeProxyFilter: nodesource.FilterConfig{Enabled: false},
	}

	count, details, err := cfg.RefreshFreeProxyCacheDetailed(context.Background())
	if err == nil {
		t.Fatal("expected refresh error for missing source")
	}
	if count != 0 {
		t.Fatalf("count=%d, want 0", count)
	}
	if len(details) != 1 {
		t.Fatalf("details len=%d, want 1: %#v", len(details), details)
	}
	if details[0].Name != "missing" || !details[0].Enabled || details[0].Error == "" {
		t.Fatalf("unexpected details: %#v", details[0])
	}
	if _, readErr := os.Stat(cachePath); !os.IsNotExist(readErr) {
		t.Fatalf("failed refresh without prior cache should not create cache file, stat err=%v", readErr)
	}
}

func TestFreeProxyCacheDefaultWorkersCoversAllConfiguredSources(t *testing.T) {
	cfg := FreeProxyCacheConfig{}.Normalized(filepath.Join(t.TempDir(), "config.yaml"), true)
	if cfg.Workers != DefaultFreeProxyCacheWorkers {
		t.Fatalf("workers=%d, want default %d", cfg.Workers, DefaultFreeProxyCacheWorkers)
	}
	many := FreeProxyCacheConfig{Workers: 100}.Normalized(filepath.Join(t.TempDir(), "config.yaml"), true)
	if many.Workers != MaxFreeProxyCacheWorkers {
		t.Fatalf("workers=%d, want capped %d", many.Workers, MaxFreeProxyCacheWorkers)
	}
}

func TestRefreshFreeProxyCacheDetailedKeepsStaleCacheWhenNoCandidatesAccepted(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.txt")
	if err := os.WriteFile(cachePath, []byte("http://9.9.9.9:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		filePath:          filepath.Join(dir, "config.yaml"),
		FreeProxyMaxNodes: 0,
		FreeProxySources: []nodesource.SourceConfig{
			{Name: "missing", File: filepath.Join(dir, "missing.txt"), Format: "txt"},
		},
		FreeProxyCache: FreeProxyCacheConfig{
			Path: cachePath,
		},
		FreeProxyFilter: nodesource.FilterConfig{Enabled: false},
	}

	count, _, err := cfg.RefreshFreeProxyCacheDetailed(context.Background())
	if err == nil {
		t.Fatal("expected refresh error for missing source")
	}
	if count != 0 {
		t.Fatalf("count=%d, want 0", count)
	}
	content, readErr := os.ReadFile(cachePath)
	if readErr != nil {
		t.Fatalf("cache should still exist: %v", readErr)
	}
	if got, want := string(content), "http://9.9.9.9:8080\n"; got != want {
		t.Fatalf("stale cache should be preserved, got %q want %q", got, want)
	}
}
