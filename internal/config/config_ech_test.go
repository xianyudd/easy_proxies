package config

import (
	"net/url"
	"strings"
	"testing"
)

func TestParseClashYAML_ECHOptsToURI(t *testing.T) {
	content := `proxies:
  - name: "cf-ech-ws"
    type: vless
    server: cloudflare.182682.xyz
    port: 8443
    uuid: 53d2e2ce-fcc4-4e5e-aeab-0bfcfd164636
    network: ws
    tls: true
    servername: alice01.799989.xyz
    client-fingerprint: chrome
    ws-opts:
      path: /alice01799989xyz
      headers:
        Host: alice01.799989.xyz
    ech-opts:
      enable: true
      query-server-name: cloudflare-ech.com
  - name: "no-ech"
    type: vless
    server: example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000001
    tls: true
    servername: example.com
`

	nodes, err := ParseSubscriptionContent(content)
	if err != nil {
		t.Fatalf("parse clash: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d: %#v", len(nodes), nodes)
	}

	var echURI, plainURI string
	for _, n := range nodes {
		switch n.Name {
		case "cf-ech-ws":
			echURI = n.URI
		case "no-ech":
			plainURI = n.URI
		}
	}
	if echURI == "" || plainURI == "" {
		t.Fatalf("missing nodes: ech=%q plain=%q", echURI, plainURI)
	}

	u, err := url.Parse(strings.Split(echURI, "#")[0])
	if err != nil {
		t.Fatalf("parse ech uri: %v", err)
	}
	q := u.Query()
	if got := q.Get("ech"); got != "1" {
		t.Fatalf("expected ech=1 on enabled node, got %q in %s", got, echURI)
	}
	if q.Get("sni") != "alice01.799989.xyz" {
		t.Fatalf("unexpected sni: %q", q.Get("sni"))
	}
	if q.Get("type") != "ws" {
		t.Fatalf("unexpected type: %q", q.Get("type"))
	}

	pu, err := url.Parse(strings.Split(plainURI, "#")[0])
	if err != nil {
		t.Fatalf("parse plain uri: %v", err)
	}
	if pu.Query().Get("ech") != "" {
		t.Fatalf("plain node should not carry ech: %s", plainURI)
	}
}

func TestParseSubscriptionContent_PreservesShareLinkECH(t *testing.T) {
	// Share-link form used by charity9116: mihomo-style ech payload is kept as-is.
	content := `vless://53d2e2ce-fcc4-4e5e-aeab-0bfcfd164636@cloudflare.182682.xyz:8443?encryption=none&security=tls&type=ws&sni=alice01.799989.xyz&ech=cloudflare-ech.com%2Bhttps%3A%2F%2Fdoh.pub%2Fdns-query&path=%2Falice01799989xyz&host=alice01.799989.xyz#cf-ech
`

	nodes, err := ParseSubscriptionContent(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	u, err := url.Parse(strings.Split(nodes[0].URI, "#")[0])
	if err != nil {
		t.Fatalf("parse uri: %v", err)
	}
	if got := u.Query().Get("ech"); !strings.Contains(got, "cloudflare-ech.com") {
		t.Fatalf("share-link ech payload lost: %q uri=%s", got, nodes[0].URI)
	}
}

func TestApplyECHParams_DisabledIgnored(t *testing.T) {
	params := url.Values{}
	applyECHParams(params, clashProxy{ECHOpts: &clashECHOptions{Enable: false}})
	if params.Get("ech") != "" {
		t.Fatalf("disabled ech-opts should not set ech, got %v", params)
	}
	applyECHParams(params, clashProxy{ECHOpts: &clashECHOptions{Enable: true}})
	if params.Get("ech") != "1" {
		t.Fatalf("enabled ech-opts should set ech=1, got %v", params)
	}
}
