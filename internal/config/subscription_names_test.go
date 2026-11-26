package config

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubscriptionResponseNameDoesNotEchoCredentials(t *testing.T) {
	cases := []struct {
		url, title string
	}{
		{"https://example.test/sub?token=client-secret-92", "Name client-secret-92"},
		{"https://alice:password@example.test/sub", "alice"},
		{"https://example.test/private-path-token", "private-path-token.yaml"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.url, nil)
		header := http.Header{"Profile-Title": []string{tc.title}}
		if name := subscriptionResponseName(header, req.URL); name != "example.test" {
			t.Fatalf("expected hostname fallback, got %q", name)
		}
	}
}

func TestSubscriptionCacheKeepsAttributionForSharedEndpoint(t *testing.T) {
	first := NodeConfig{URI: "socks5://proxy.test:1080#first", SubscriptionName: "甲", SubscriptionID: subscriptionID("https://a.test/feed")}
	second := NodeConfig{URI: "socks5://proxy.test:1080#second", SubscriptionName: "乙", SubscriptionID: subscriptionID("https://b.test/feed")}
	path := filepath.Join(t.TempDir(), "nodes.txt")
	if err := WriteSubscriptionNodes(path, []NodeConfig{first, second}); err != nil {
		t.Fatal(err)
	}
	nodes, err := LoadNodesFromFile(path)
	if err != nil || len(nodes) != 2 {
		t.Fatalf("reading cache: %v (%d nodes)", err, len(nodes))
	}
	first.SubscriptionName = "甲更新"
	if !MergeSubscriptionNames(nodes, []NodeConfig{first}) || nodes[0].SubscriptionName != "甲更新" || nodes[1].SubscriptionName != "乙" {
		t.Fatal("partial refresh replaced a different subscription's label")
	}
}

func TestSubscriptionNamesFetchAndCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := strings.TrimPrefix(r.URL.Path, "/")
		switch index {
		case "title":
			w.Header().Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte("魔戒.net")))
			w.Header().Set("Content-Disposition", `attachment; filename="ignored.yaml"`)
		case "filename":
			w.Header().Set("Content-Disposition", `attachment; filename*=UTF-8''%E8%8B%8F%E8%8F%B2.yaml`)
		default:
			w.Header().Set("Profile-Title", "https://example.test/sub?token=private")
		}
		_, _ = w.Write([]byte("socks5://" + index + ".test:1080#original\n"))
	}))
	defer srv.Close()
	urls := []string{srv.URL + "/title?token=private", srv.URL + "/filename", srv.URL + "/fallback"}
	nodes, stats := FetchSubscriptionNodes(context.Background(), urls, SubscriptionFetchOptions{Client: srv.Client()})
	if len(nodes) != 3 || stats.Successful != 3 {
		t.Fatalf("received %d nodes, stats=%+v", len(nodes), stats)
	}
	cfg := Config{Subscriptions: urls, Nodes: nodes}
	want := []string{"魔戒.net", "苏菲", "127.0.0.1"}
	check := func(names []string) {
		t.Helper()
		if strings.Join(names, "|") != strings.Join(want, "|") {
			t.Fatalf("names = %q, want %q", names, want)
		}
	}
	check(cfg.SubscriptionNames())
	file := filepath.Join(t.TempDir(), "nodes.txt")
	if err := writeNodesToFile(file, nodes); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private") {
		t.Fatal("cache exposes subscription URL token")
	}
	cfg.Nodes, err = LoadNodesFromFile(file)
	if err != nil {
		t.Fatal(err)
	}
	check(cfg.SubscriptionNames())
	for _, node := range cfg.Nodes {
		if node.SubscriptionName == "" || ExtractNodeName(node.URI) != "original" {
			t.Fatalf("cache metadata or node name was lost: %+v", node)
		}
	}
}
