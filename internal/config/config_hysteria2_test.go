package config

import (
	"net/url"
	"testing"
)

func TestParseSubscriptionContent_FiltersPlainHTTPMetadata(t *testing.T) {
	content := `https://example.com/status#473-62-gb
https://example.com/expire/2026-06-18
http://example.com/reset/27
vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none#valid
hy2://secret@example.org:443?sni=example.org#hy2-valid
`

	nodes, err := ParseSubscriptionContent(content)
	if err != nil {
		t.Fatalf("parse subscription failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 real proxy nodes, got %d: %#v", len(nodes), nodes)
	}
	for _, node := range nodes {
		if node.URI == "" || node.URI[:4] == "http" {
			t.Fatalf("unexpected metadata node imported: %#v", node)
		}
	}
}

func TestParseNodesFromContent_AllowsManualHTTPProxy(t *testing.T) {
	nodes, err := parseNodesFromContent("http://user:pass@example.com:8080\nhttps://example.org:8443\n")
	if err != nil {
		t.Fatalf("parse nodes failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected manual HTTP(S) proxy nodes to be kept, got %d", len(nodes))
	}
}
func TestParseClashYAML_Hysteria2PortHoppingAndObfs(t *testing.T) {
	content := `proxies:
  - name: "test-hy2"
    type: "hysteria2"
    server: example.com
    ports: 10000-20000
    password: "secret"
    obfs: "salamander"
    obfs-password: "obfs-secret"
    sni: "hy2.example.com"
    skip-cert-verify: true
`

	nodes, err := parseClashYAML(content)
	if err != nil {
		t.Fatalf("parse clash yaml failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	u, err := url.Parse(nodes[0].URI)
	if err != nil {
		t.Fatalf("parse generated uri failed: %v", err)
	}
	if u.Scheme != "hysteria2" {
		t.Fatalf("expected scheme hysteria2, got %q", u.Scheme)
	}
	if u.Host != "example.com:443" {
		t.Fatalf("expected host example.com:443, got %q", u.Host)
	}

	query := u.Query()
	if query.Get("ports") != "10000:20000" {
		t.Fatalf("expected ports=10000:20000, got %q", query.Get("ports"))
	}
	if query.Get("obfs") != "salamander" {
		t.Fatalf("expected obfs=salamander, got %q", query.Get("obfs"))
	}
	if query.Get("obfs-password") != "obfs-secret" {
		t.Fatalf("expected obfs-password=obfs-secret, got %q", query.Get("obfs-password"))
	}
	if query.Get("sni") != "hy2.example.com" {
		t.Fatalf("expected sni=hy2.example.com, got %q", query.Get("sni"))
	}
	if query.Get("insecure") != "1" {
		t.Fatalf("expected insecure=1, got %q", query.Get("insecure"))
	}
}
