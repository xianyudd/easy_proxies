package config

import (
	"context"
	"fmt"
	"time"

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

func TestLoadSkipsFreeProxyCacheWhenAllSourcesDisabled(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "free-cache.txt")
	if err := os.WriteFile(cachePath, []byte("http://1.2.3.4:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `mode: pool
nodes:
  - name: inline
    uri: http://127.0.0.1:1
free_proxy_cache:
  enabled: true
  path: free-cache.txt
free_proxy_sources:
  - name: disabled
    enabled: false
    url: http://127.0.0.1:9/list.txt
    format: txt
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	if len(cfg.Nodes) != 1 || cfg.Nodes[0].Source == NodeSourceFreeProxy {
		t.Fatalf("disabled free proxy sources must not materialize cached free nodes: %#v", cfg.Nodes)
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

func TestRefreshFreeProxyCacheUsesFreshCacheWithoutFetchingRemote(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.txt")
	if err := os.WriteFile(cachePath, []byte("http://9.9.9.9:8080\nhttp://8.8.8.8:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hits := 0
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		t.Fatal("fresh cache refresh should not fetch remote source")
	}))
	defer slow.Close()

	cfg := &Config{
		filePath: filepath.Join(dir, "config.yaml"),
		FreeProxySources: []nodesource.SourceConfig{
			{Name: "remote", URL: slow.URL, Format: "txt", Timeout: time.Second},
		},
		FreeProxyCache:  FreeProxyCacheConfig{Path: cachePath, MaxAge: time.Hour},
		FreeProxyFilter: nodesource.FilterConfig{Enabled: false},
	}
	cache := cfg.FreeProxyCache.Normalized(cfg.filePath, true)
	if err := writeFreeProxyCacheSignature(cache.Path, cfg.freeProxyCacheSignature(cache)); err != nil {
		t.Fatalf("write cache metadata: %v", err)
	}

	started := time.Now()
	summary, err := cfg.RefreshFreeProxyCacheSummary(context.Background())
	if err != nil {
		t.Fatalf("refresh should reuse fresh cache: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("fresh cache reuse took too long: %s", elapsed)
	}
	if summary.Count != 2 || summary.CacheUpdated {
		t.Fatalf("summary=%#v, want two cached nodes without cache update", summary)
	}
	if len(summary.Sources) != 1 || summary.Sources[0].Candidates != 0 || summary.Sources[0].Accepted != 0 || summary.Sources[0].Error != "" {
		t.Fatalf("fresh-cache skip should keep source telemetry neutral, got %#v", summary.Sources)
	}
	if hits != 0 {
		t.Fatalf("remote hits=%d, want 0", hits)
	}
}

func TestRefreshFreeProxyCacheReusesFreshLegacyCacheWithoutMetadata(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.txt")
	if err := os.WriteFile(cachePath, []byte("http://9.9.9.9:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hits := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		t.Fatal("fresh legacy cache refresh should not fetch remote source")
	}))
	defer remote.Close()

	cfg := &Config{
		filePath: filepath.Join(dir, "config.yaml"),
		FreeProxySources: []nodesource.SourceConfig{
			{Name: "remote", URL: remote.URL, Format: "txt", Timeout: time.Second},
		},
		FreeProxyCache:  FreeProxyCacheConfig{Path: cachePath, MaxAge: time.Hour},
		FreeProxyFilter: nodesource.FilterConfig{Enabled: false},
	}

	summary, err := cfg.RefreshFreeProxyCacheSummary(context.Background())
	if err != nil {
		t.Fatalf("refresh should reuse fresh legacy cache: %v", err)
	}
	if hits != 0 {
		t.Fatalf("remote hits=%d, want 0", hits)
	}
	if summary.Count != 1 || summary.CacheUpdated {
		t.Fatalf("summary=%#v, want one cached node without cache update", summary)
	}
}

func TestRefreshFreeProxyCacheRefetchesFreshCacheWhenSignatureChanged(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.txt")
	if err := os.WriteFile(cachePath, []byte("http://9.9.9.9:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hits := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("http://7.7.7.7:8080\n"))
	}))
	defer remote.Close()

	cfg := &Config{
		filePath: filepath.Join(dir, "config.yaml"),
		FreeProxySources: []nodesource.SourceConfig{
			{Name: "remote", URL: remote.URL, Format: "txt", Timeout: time.Second},
		},
		FreeProxyCache:  FreeProxyCacheConfig{Path: cachePath, MaxAge: time.Hour},
		FreeProxyFilter: nodesource.FilterConfig{Enabled: false},
	}
	oldCfg := *cfg
	oldCfg.FreeProxyMaxNodes = 1
	cache := cfg.FreeProxyCache.Normalized(cfg.filePath, true)
	if err := writeFreeProxyCacheSignature(cache.Path, oldCfg.freeProxyCacheSignature(cache)); err != nil {
		t.Fatalf("write stale cache metadata: %v", err)
	}

	summary, err := cfg.RefreshFreeProxyCacheSummary(context.Background())
	if err != nil {
		t.Fatalf("refresh should fetch remote after signature change: %v", err)
	}
	if hits != 1 {
		t.Fatalf("remote hits=%d, want 1", hits)
	}
	if summary.Count != 1 || !summary.CacheUpdated {
		t.Fatalf("summary=%#v, want one refreshed node with cache update", summary)
	}
	content, readErr := os.ReadFile(cachePath)
	if readErr != nil {
		t.Fatalf("read cache: %v", readErr)
	}
	if got, want := string(content), "http://7.7.7.7:8080\n"; got != want {
		t.Fatalf("cache content=%q, want %q", got, want)
	}
	if !freeProxyCacheSignatureMatches(cache.Path, cfg.freeProxyCacheSignature(cache)) {
		t.Fatalf("cache metadata should be updated to current free proxy signature")
	}
}

func TestRefreshFreeProxyCacheDoesNotReuseMismatchedCacheWhenRefreshAcceptsNothing(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.txt")
	if err := os.WriteFile(cachePath, []byte("http://9.9.9.9:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		filePath: filepath.Join(dir, "config.yaml"),
		FreeProxySources: []nodesource.SourceConfig{
			{Name: "missing", File: filepath.Join(dir, "missing.txt"), Format: "txt"},
		},
		FreeProxyCache:  FreeProxyCacheConfig{Path: cachePath, MaxAge: time.Hour},
		FreeProxyFilter: nodesource.FilterConfig{Enabled: false},
	}
	oldCfg := *cfg
	oldCfg.FreeProxySources = []nodesource.SourceConfig{
		{Name: "removed-source", File: filepath.Join(dir, "removed.txt"), Format: "txt"},
	}
	cache := cfg.FreeProxyCache.Normalized(cfg.filePath, true)
	if err := writeFreeProxyCacheSignature(cache.Path, oldCfg.freeProxyCacheSignature(cache)); err != nil {
		t.Fatalf("write stale cache metadata: %v", err)
	}

	summary, err := cfg.RefreshFreeProxyCacheSummary(context.Background())
	if err == nil {
		t.Fatalf("refresh should not report success by reusing a cache signed for different sources: summary=%#v", summary)
	}
	if summary.Count != 0 || summary.CacheUpdated {
		t.Fatalf("summary=%#v, want no accepted count and no cache update", summary)
	}
	if len(summary.Sources) != 1 || summary.Sources[0].Error == "" {
		t.Fatalf("expected source failure telemetry, got %#v", summary.Sources)
	}
	content, readErr := os.ReadFile(cachePath)
	if readErr != nil {
		t.Fatalf("mismatched cache should be preserved on disk for manual recovery: %v", readErr)
	}
	if got, want := string(content), "http://9.9.9.9:8080\n"; got != want {
		t.Fatalf("cache content=%q, want preserved %q", got, want)
	}
}

func TestRefreshFreeProxyCacheDetailedReusesStaleCacheWhenNoCandidatesAccepted(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.txt")
	if err := os.WriteFile(cachePath, []byte("http://9.9.9.9:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(cachePath, old, old); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		filePath:          filepath.Join(dir, "config.yaml"),
		FreeProxyMaxNodes: 0,
		FreeProxySources: []nodesource.SourceConfig{
			{Name: "missing", File: filepath.Join(dir, "missing.txt"), Format: "txt"},
		},
		FreeProxyCache: FreeProxyCacheConfig{
			Path:   cachePath,
			MaxAge: time.Hour,
		},
		FreeProxyFilter: nodesource.FilterConfig{Enabled: false},
	}

	summary, err := cfg.RefreshFreeProxyCacheSummary(context.Background())
	if err != nil {
		t.Fatalf("stale cache should allow refresh to remain usable, got error: %v", err)
	}
	if summary.Count != 1 || summary.CacheUpdated {
		t.Fatalf("summary=%#v, want stale count 1 without cache update", summary)
	}
	if len(summary.Sources) != 1 || summary.Sources[0].Error == "" {
		t.Fatalf("expected per-source failure telemetry to be preserved, got %#v", summary.Sources)
	}
	content, readErr := os.ReadFile(cachePath)
	if readErr != nil {
		t.Fatalf("cache should still exist: %v", readErr)
	}
	if got, want := string(content), "http://9.9.9.9:8080\n"; got != want {
		t.Fatalf("stale cache should be preserved, got %q want %q", got, want)
	}
}

func TestRefreshFreeProxyCacheDetailedFallsBackToHTTPBasicWhenSimpleWebRejectsAll(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.txt")
	sourcePath := filepath.Join(dir, "free.txt")

	httpOnly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generate_204":
			w.WriteHeader(http.StatusNoContent)
		case "/https":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer httpOnly.Close()

	if err := os.WriteFile(sourcePath, []byte(httpOnly.URL+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		filePath:          filepath.Join(dir, "config.yaml"),
		FreeProxyMaxNodes: 0,
		FreeProxySources: []nodesource.SourceConfig{
			{Name: "http-only", File: sourcePath, Format: "txt"},
		},
		FreeProxyCache: FreeProxyCacheConfig{Path: cachePath},
		FreeProxyFilter: nodesource.FilterConfig{
			Enabled: true,
			MinTier: "simple_web",
			Workers: 1,
			Timeout: 2 * time.Second,
			Probes:  nodesource.FilterProbes{HTTP: "/generate_204", HTTPS: "/https"},
		},
	}

	count, details, err := cfg.RefreshFreeProxyCacheDetailed(context.Background())
	if err != nil {
		t.Fatalf("refresh should fall back to http_basic instead of failing: %v details=%#v", err, details)
	}
	if count != 1 {
		t.Fatalf("count=%d, want 1 details=%#v", count, details)
	}
	content, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(content), httpOnly.URL+"\n"; got != want {
		t.Fatalf("cache=%q, want %q", got, want)
	}
	if len(details) != 1 || details[0].Accepted != 1 {
		t.Fatalf("details should report fallback accepted node: %#v", details)
	}
}

func TestRefreshFreeProxyCacheUsesProbeBudgetWithoutParseCap(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "proxies.txt")
	cachePath := filepath.Join(dir, "cache.txt")

	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generate_204" {
			t.Fatalf("unexpected probe path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	lines := []string{good.URL}
	for i := 0; i < 8; i++ {
		lines = append(lines, bad.URL)
	}
	lines = append(lines, good.URL)
	if err := os.WriteFile(sourcePath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		filePath: filepath.Join(dir, "config.yaml"),
		FreeProxySources: []nodesource.SourceConfig{
			{Name: "budgeted", File: sourcePath, Format: "txt"},
		},
		FreeProxyCache: FreeProxyCacheConfig{Path: cachePath},
		FreeProxyFilter: nodesource.FilterConfig{
			Enabled:            true,
			MinTier:            "http_basic",
			Workers:            2,
			Timeout:            time.Second,
			MaxProbeCandidates: 2,
			Probes:             nodesource.FilterProbes{HTTP: "/generate_204"},
		},
	}

	count, details, err := cfg.RefreshFreeProxyCacheDetailed(context.Background())
	if err != nil {
		t.Fatalf("refresh failed: %v details=%#v", err, details)
	}
	if count != 1 {
		t.Fatalf("count=%d, want deduplicated accepted count 1", count)
	}
	if len(details) != 1 || details[0].Candidates != 10 || details[0].Accepted != 2 {
		t.Fatalf("details should report full parsed candidates and budgeted accepted probes: %#v", details)
	}
	content, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(content), good.URL+"\n"; got != want {
		t.Fatalf("cache=%q, want %q", got, want)
	}
}
