package config

import (
	"easy_proxies/internal/nodesource"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
