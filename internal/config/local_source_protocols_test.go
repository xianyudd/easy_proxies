package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

const localTestUUID = "11111111-1111-4111-8111-111111111111"

func vmessTestURI(body string) string {
	return "vmess://" + base64.StdEncoding.EncodeToString([]byte(body))
}

func TestLocalSourceProtocolValidation(t *testing.T) {
	goodJSON := `{"add":"node.test","id":"` + localTestUUID + `","port":"443","ps":"Beta"}`
	tests := []struct {
		name, uri string
		valid     bool
	}{
		{"vmess json", vmessTestURI(goodJSON), true},
		{"vmess rawurl", "vmess://" + base64.RawURLEncoding.EncodeToString([]byte(goodJSON)), true},
		{"vmess default port", vmessTestURI(`{"add":"::1","id":"` + localTestUUID + `"}`), true},
		{"vmess url", "vmess://" + localTestUUID + "@[::1]:443", true},
		{"vmess name only", vmessTestURI(`{"ps":"secret"}`), false},
		{"vmess no id", vmessTestURI(`{"add":"node.test"}`), false},
		{"vmess bad id", vmessTestURI(`{"add":"node.test","id":"secret"}`), false},
		{"vmess bad json", vmessTestURI(`not-json-secret`), false},
		{"vmess bad base64", "vmess://secret!!!", false},
		{"vmess wrong type", vmessTestURI(`{"add":3,"id":"` + localTestUUID + `"}`), false},
		{"vmess port zero", vmessTestURI(strings.Replace(goodJSON, `"443"`, `0`, 1)), false},
		{"vmess port huge", vmessTestURI(strings.Replace(goodJSON, `"443"`, `65536`, 1)), false},
		{"vmess port fractional", vmessTestURI(strings.Replace(goodJSON, `"443"`, `1.5`, 1)), false},
		{"vmess port negative", vmessTestURI(strings.Replace(goodJSON, `"443"`, `-1`, 1)), false},
		{"vmess port text", vmessTestURI(strings.Replace(goodJSON, `"443"`, `"bad"`, 1)), false},
		{"vmess control", vmessTestURI(strings.Replace(goodJSON, "node.test", `node\ntest`, 1)), false},
		{"ss userinfo", "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:secret")) + "@node.test:443", true},
		{"ss plain unsupported by builder", "ss://aes-128-gcm:secret@node.test:443", false},
		{"ss no credentials", "ss://node.test:443", false},
		{"ss bad method", "ss://nonsense:secret@node.test:443", false},
		{"ss plugin", "ss://aes-128-gcm:secret@node.test:443?plugin=unknown", false},
		{"hy2 range", "hy2://secret@node.test:21000-21999", true},
		{"hy2 query", "hysteria2://secret@node.test?ports=1000-2000,3000&hop_interval=30s", true},
		{"hy2 reverse", "hy2://secret@node.test:22000-21000", false},
		{"hy2 huge", "hy2://secret@node.test?ports=65536", false},
		{"hy2 duration", "hy2://secret@node.test?hopInterval=bad", false},
		{"trojan", "trojan://secret@node.test", true},
		{"trojan no credential", "trojan://node.test", false},
		{"anytls", "anytls://secret@node.test", true},
		{"tuic", "tuic://" + localTestUUID + ":secret@node.test", true},
		{"tuic no password", "tuic://" + localTestUUID + "@node.test", false},
		{"http", "http://node.test:8080", true},
		{"socks alias", "socks://node.test:1080", true},
		{"socks5h", "socks5h://node.test:1080", true},
		{"unsupported ssr", "ssr://secret", false},
		{"unsupported hysteria", "hysteria://secret@node.test", false},
		{"empty host", "http://:8080", false},
		{"bad port", "http://node.test:65536", false},
		{"decoded control", "http://secret%0A@node.test", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLocalSourceNodes("# comment\n\n" + tt.uri)
			if tt.valid {
				if err != nil || len(got.Nodes) != 1 {
					t.Fatalf("valid fixture rejected: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid fixture accepted")
			}
			if len(got.Nodes) != 0 || !strings.Contains(err.Error(), "line 3") || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), localTestUUID) {
				t.Fatalf("unsafe or partial failure: %v", err)
			}
		})
	}
}
