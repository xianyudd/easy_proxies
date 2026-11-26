package config

import (
	"strings"
	"testing"
)

// Synthetic canonical UUID reserved for tests; never a real credential.
const testVLESSUUID = "11111111-1111-4111-8111-111111111111"

// TestParseLocalSourceNodesVLESSStructuralValidation pins the structural
// contract of vless:// lines in local sources: canonical UUID userinfo, a
// non-empty host, and an explicit port (when present) within 1-65535.
// Rejections must carry the original line number, never echo the URI or
// its synthetic credentials, and fail the whole import with empty Nodes.
func TestParseLocalSourceNodesVLESSStructuralValidation(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
	}{
		{name: "valid uuid host port", uri: "vless://" + testVLESSUUID + "@node.test:443#Name"},
		{name: "valid ipv6 host", uri: "vless://" + testVLESSUUID + "@[::1]:443"},
		{name: "valid omitted port defaults", uri: "vless://" + testVLESSUUID + "@node.test#Name"},
		{name: "missing userinfo", uri: "vless://node.test:443", wantErr: true},
		{name: "empty uuid", uri: "vless://@node.test:443", wantErr: true},
		{name: "non-uuid userinfo", uri: "vless://bob@node.test:443", wantErr: true},
		{name: "uuid with password", uri: "vless://" + testVLESSUUID + ":pw@node.test:443", wantErr: true},
		{name: "empty host", uri: "vless://" + testVLESSUUID + "@", wantErr: true},
		{name: "port zero", uri: "vless://" + testVLESSUUID + "@node.test:0", wantErr: true},
		{name: "port 65536", uri: "vless://" + testVLESSUUID + "@node.test:65536", wantErr: true},
		{name: "negative port", uri: "vless://" + testVLESSUUID + "@node.test:-1", wantErr: true},
		{name: "non-numeric port", uri: "vless://" + testVLESSUUID + "@node.test:abc", wantErr: true},
		{name: "raw internal space", uri: "vless://" + testVLESSUUID + "@node.test:443 bad", wantErr: true},
		{name: "control char in userinfo", uri: "vless://" + testVLESSUUID + "\x01@node.test:443", wantErr: true},
		{name: "truncated uuid", uri: "vless://11111111-1111-4111-8111-11111111111@node.test:443", wantErr: true},
		{name: "wrong hyphen positions", uri: "vless://111111111111-1111-1111-1111-111111111111@node.test:443", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ParseLocalSourceNodes(tc.uri + "\n")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want rejection, got nodes %+v", result.Nodes)
				}
				msg := err.Error()
				if !strings.Contains(msg, "line 1") {
					t.Fatalf("error must carry original line number: %v", err)
				}
				if strings.Contains(msg, "vless://") || strings.Contains(msg, tc.uri) || strings.Contains(msg, "node.test") {
					t.Fatalf("error echoes URI content: %v", err)
				}
				if strings.Contains(msg, "pw") {
					t.Fatalf("error echoes synthetic credential: %v", err)
				}
				if len(result.Nodes) != 0 {
					t.Fatalf("failed import must return empty nodes, got %+v", result.Nodes)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected rejection: %v", err)
			}
			if len(result.Nodes) != 1 {
				t.Fatalf("nodes = %d, want 1", len(result.Nodes))
			}
		})
	}
}

// TestParseLocalSourceNodesVLESSWholeImportFailsOnBadLine checks that one bad
// vless line fails the whole strict import, reports the offending line, and
// leaves no partial Nodes behind.
func TestParseLocalSourceNodesVLESSWholeImportFailsOnBadLine(t *testing.T) {
	content := "vless://" + testVLESSUUID + "@ok.test:443#Good\n" +
		"vless://bob@bad.test:443#Bad\n"
	result, err := ParseLocalSourceNodes(content)
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("want line-2 failure, got %v", err)
	}
	if len(result.Nodes) != 0 {
		t.Fatalf("failed import must return empty nodes, got %+v", result.Nodes)
	}
	msg := err.Error()
	if strings.Contains(msg, "bob") || strings.Contains(msg, "bad.test") || strings.Contains(msg, "vless://") {
		t.Fatalf("error echoes URI content: %v", err)
	}
}
