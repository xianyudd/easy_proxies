package nodesource

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseFreeProxyTextAddsDefaultHTTPSchemeAndSkipsComments(t *testing.T) {
	nodes, err := ParseFreeProxyContent("txt", []byte(`
# comment
1.2.3.4:8080
http://5.6.7.8:3128
bad line
`))
	if err != nil {
		t.Fatalf("parse free proxy text failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d: %#v", len(nodes), nodes)
	}
	if nodes[0].URI != "http://1.2.3.4:8080" {
		t.Fatalf("expected default http scheme, got %q", nodes[0].URI)
	}
	if nodes[1].URI != "http://5.6.7.8:3128" {
		t.Fatalf("expected explicit uri to be preserved, got %q", nodes[1].URI)
	}
}

func TestParseFreeProxyJSONSupportsArrayObjects(t *testing.T) {
	nodes, err := ParseFreeProxyContent("json", []byte(`[
  {"ip":"1.2.3.4","port":8080,"protocol":"https","country":"US"},
  {"host":"example.com","port":"1080","type":"socks5"},
  {"uri":"http://5.6.7.8:3128","name":"named"}
]`))
	if err != nil {
		t.Fatalf("parse free proxy json failed: %v", err)
	}
	want := []string{"https://1.2.3.4:8080", "socks5://example.com:1080", "http://5.6.7.8:3128"}
	if len(nodes) != len(want) {
		t.Fatalf("expected %d nodes, got %d: %#v", len(want), len(nodes), nodes)
	}
	for i := range want {
		if nodes[i].URI != want[i] {
			t.Fatalf("node %d uri: want %q got %q", i, want[i], nodes[i].URI)
		}
	}
	if nodes[0].Region != "US" || nodes[2].Name != "named" {
		t.Fatalf("metadata not preserved: %#v", nodes)
	}
}

func TestProviderLoadsFromFileAndHTTP(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "proxies.txt")
	if err := os.WriteFile(file, []byte("1.2.3.4:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fileNodes, err := NewProvider(SourceConfig{Name: "local", File: file}).Load()
	if err != nil {
		t.Fatalf("load file source failed: %v", err)
	}
	if len(fileNodes) != 1 || fileNodes[0].SourceName != "local" {
		t.Fatalf("unexpected file nodes: %#v", fileNodes)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"ip":"5.6.7.8","port":3128}]`))
	}))
	defer server.Close()

	httpNodes, err := NewProvider(SourceConfig{Name: "remote", URL: server.URL, Format: "json"}).Load()
	if err != nil {
		t.Fatalf("load http source failed: %v", err)
	}
	if len(httpNodes) != 1 || httpNodes[0].URI != "http://5.6.7.8:3128" || httpNodes[0].SourceName != "remote" {
		t.Fatalf("unexpected http nodes: %#v", httpNodes)
	}
}

func TestParseFreeProxyContentLimitedCapsTextAndJSON(t *testing.T) {
	textNodes, err := ParseFreeProxyContentLimited("txt", []byte(`
1.1.1.1:80
bad line
2.2.2.2:80
3.3.3.3:80
`), 2)
	if err != nil {
		t.Fatalf("parse text failed: %v", err)
	}
	if len(textNodes) != 2 {
		t.Fatalf("expected 2 text nodes, got %d: %#v", len(textNodes), textNodes)
	}

	jsonNodes, err := ParseFreeProxyContentLimited("json", []byte(`[
{"ip":"1.1.1.1","port":80},
{"ip":"2.2.2.2","port":80},
{"ip":"3.3.3.3","port":80}
]`), 2)
	if err != nil {
		t.Fatalf("parse json failed: %v", err)
	}
	if len(jsonNodes) != 2 {
		t.Fatalf("expected 2 json nodes, got %d: %#v", len(jsonNodes), jsonNodes)
	}
}

func TestProviderRejectsOversizedSourceBeforeParse(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "oversized.txt")
	if err := os.WriteFile(file, []byte("1.1.1.1:80\n2.2.2.2:80\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewProvider(SourceConfig{Name: "small", File: file, MaxBytes: 4}).Load()
	if err == nil {
		t.Fatal("expected oversized source error")
	}
}

func TestProviderLoadLimitedUsesLowerPositiveLimit(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "proxies.txt")
	if err := os.WriteFile(file, []byte("1.1.1.1:80\n2.2.2.2:80\n3.3.3.3:80\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nodes, err := NewProvider(SourceConfig{Name: "local", File: file, MaxNodes: 2}).LoadLimited(10)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected source max_nodes cap, got %d", len(nodes))
	}
	nodes, err = NewProvider(SourceConfig{Name: "local", File: file, MaxNodes: 10}).LoadLimited(1)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected caller cap, got %d", len(nodes))
	}
}
