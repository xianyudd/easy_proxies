package config

import (
	"net/url"
	"strings"
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

func TestParseSubscriptionContentFiltersLocalSSMetadataPlaceholders(t *testing.T) {
	content := `ss://YWVzLTEyOC1nY206ZmFrZQ@127.0.0.1:6666#套餐到期：2026-06-18
ss://YWVzLTEyOC1nY206ZmFrZQ@127.0.0.1:6666#距离下次重置剩余：7 天
ss://YWVzLTEyOC1nY206ZmFrZQ@127.0.0.1:6666#%E5%A5%97%E9%A4%90%E5%88%B0%E6%9C%9F%EF%BC%9A2026-06-18
ss://YWVzLTEyOC1nY206ZmFrZQ@127.0.0.1:6666#%E8%B7%9D%E7%A6%BB%E4%B8%8B%E6%AC%A1%E9%87%8D%E7%BD%AE%E5%89%A9%E4%BD%99%EF%BC%9A7+%E5%A4%A9
ss://YWVzLTEyOC1nY206ZmFrZQ@example.com:8388#日本JP
`

	nodes, err := ParseSubscriptionContent(content)
	if err != nil {
		t.Fatalf("parse subscription failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected only real proxy node, got %d: %#v", len(nodes), nodes)
	}
	if got := nodes[0].URI; got == "" || !strings.Contains(got, "example.com:8388") {
		t.Fatalf("unexpected node kept: %#v", nodes[0])
	}
}

func TestParseClashYAMLFiltersLocalMetadataPlaceholders(t *testing.T) {
	content := `proxies:
  - name: "套餐到期：2026-06-18"
    type: ss
    server: 127.0.0.1
    port: 6666
    cipher: aes-128-gcm
    password: fake
  - name: "距离下次重置剩余：7 天"
    type: ss
    server: 127.0.0.1
    port: 6666
    cipher: aes-128-gcm
    password: fake
  - name: "日本JP"
    type: ss
    server: example.com
    port: 8388
    cipher: aes-128-gcm
    password: fake
`

	nodes, err := ParseSubscriptionContent(content)
	if err != nil {
		t.Fatalf("parse clash yaml failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected only real proxy node, got %d: %#v", len(nodes), nodes)
	}
	if got := nodes[0].Name; got != "日本JP" {
		t.Fatalf("unexpected node kept: %#v", nodes[0])
	}
}

func TestParseSubscriptionContentFiltersRemoteMetadataNames(t *testing.T) {
	content := `vmess://460a0cf8-c89d-4ed9-9484-26be6424ba8d@planb.mojcn.com:16617?path=%2F&type=ws#套餐到期：长期有效
vmess://460a0cf8-c89d-4ed9-9484-26be6424ba8d@planb.mojcn.com:16617?path=%2F&type=ws#剩余流量：23.34 GB
vless://00000000-0000-0000-0000-000000000000@example.com:443?encryption=none#日本JP
`

	nodes, err := ParseSubscriptionContent(content)
	if err != nil {
		t.Fatalf("parse subscription failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected only real proxy node, got %d: %#v", len(nodes), nodes)
	}
	if got := ExtractNodeName(nodes[0].URI); got != "日本JP" {
		t.Fatalf("unexpected node kept: name=%q node=%#v", got, nodes[0])
	}
}

func TestParseClashYAMLFiltersRemoteMetadataNames(t *testing.T) {
	content := `proxies:
  - name: "套餐到期：长期有效"
    type: vmess
    server: planb.mojcn.com
    port: 16617
    uuid: 460a0cf8-c89d-4ed9-9484-26be6424ba8d
    alterId: 0
    cipher: auto
    network: ws
  - name: "剩余流量：23.34 GB"
    type: vmess
    server: planb.mojcn.com
    port: 16617
    uuid: 460a0cf8-c89d-4ed9-9484-26be6424ba8d
    alterId: 0
    cipher: auto
    network: ws
  - name: "日本JP"
    type: vmess
    server: example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    alterId: 0
    cipher: auto
`

	nodes, err := ParseSubscriptionContent(content)
	if err != nil {
		t.Fatalf("parse clash yaml failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected only real proxy node, got %d: %#v", len(nodes), nodes)
	}
	if got := nodes[0].Name; got != "日本JP" {
		t.Fatalf("unexpected node kept: %#v", nodes[0])
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
