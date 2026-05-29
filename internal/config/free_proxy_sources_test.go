package config

import (
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
