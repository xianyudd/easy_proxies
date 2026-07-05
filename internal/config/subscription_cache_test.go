package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKeepsSubscriptionCacheWhenRefreshReturnsFewerNodes(t *testing.T) {
	dir := t.TempDir()
	nodesFile := filepath.Join(dir, "nodes.txt")
	if err := os.WriteFile(nodesFile, []byte("socks5://1.1.1.1:8080#cached-a\nsocks5://2.2.2.2:8080#cached-b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("socks5://3.3.3.3:8080#fresh-only\n"))
	}))
	defer sub.Close()

	cfgPath := filepath.Join(dir, "config.yaml")
	content := `mode: pool
nodes_file: nodes.txt
subscriptions:
  - ` + sub.URL + `
free_proxy_cache:
  enabled: false
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	var subscriptionNodes int
	for _, node := range cfg.Nodes {
		if node.Source == NodeSourceSubscription {
			subscriptionNodes++
		}
	}
	if subscriptionNodes != 2 {
		t.Fatalf("expected cached subscription nodes to win, got %d", subscriptionNodes)
	}
}
