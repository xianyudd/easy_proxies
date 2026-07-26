package builder

import (
	"encoding/pem"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"easy_proxies/internal/config"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

func resetECHCacheForTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("EASY_PROXIES_CACHE_DIR", dir)
	globalECHCache = nil
	globalECHCacheOnce = sync.Once{}
	return dir
}

func TestIsECHEnabled(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"off", false},
		{"1", true},
		{"true", true},
		{"cloudflare-ech.com+https://doh.pub/dns-query", true},
	}
	for _, tc := range cases {
		q := url.Values{}
		if tc.raw != "" {
			q.Set("ech", tc.raw)
		}
		if got := isECHEnabled(q); got != tc.want {
			t.Fatalf("isECHEnabled(%q)=%v want %v", tc.raw, got, tc.want)
		}
	}
}

func TestBuildTLSOptions_EnablesECH(t *testing.T) {
	resetECHCacheForTest(t)

	// Seed a synthetic ECH CONFIGS PEM so the test does not depend on live DNS.
	sni := "alice01.799989.xyz"
	fakeList := []byte{0x00, 0x04, 0xaa, 0xbb, 0xcc, 0xdd}
	pemBody := string(pem.EncodeToMemory(&pem.Block{Type: "ECH CONFIGS", Bytes: fakeList}))
	getECHCache().put(sni, pemBody, time.Hour)

	q := url.Values{}
	q.Set("security", "tls")
	q.Set("sni", sni)
	q.Set("ech", "cloudflare-ech.com+https://doh.pub/dns-query")
	q.Set("fp", "chrome")

	tls, err := buildTLSOptions(q, false)
	if err != nil {
		t.Fatalf("buildTLSOptions: %v", err)
	}
	if tls == nil || !tls.Enabled {
		t.Fatal("expected TLS enabled")
	}
	if tls.ServerName != sni {
		t.Fatalf("sni=%q", tls.ServerName)
	}
	if tls.ECH == nil || !tls.ECH.Enabled {
		t.Fatalf("expected ECH enabled, got %#v", tls.ECH)
	}
	if len(tls.ECH.Config) == 0 {
		t.Fatal("expected ECH Config PEM lines injected from cache")
	}
	joined := strings.Join([]string(tls.ECH.Config), "\n")
	if !strings.Contains(joined, "BEGIN ECH CONFIGS") {
		t.Fatalf("unexpected ECH config lines: %v", tls.ECH.Config)
	}
}

func TestBuildTLSOptions_NoECHWhenAbsent(t *testing.T) {
	q := url.Values{}
	q.Set("security", "tls")
	q.Set("sni", "example.com")
	tls, err := buildTLSOptions(q, false)
	if err != nil {
		t.Fatalf("buildTLSOptions: %v", err)
	}
	if tls.ECH != nil {
		t.Fatalf("unexpected ECH: %#v", tls.ECH)
	}
}

func TestBuildTrojanTLSOptions_EnablesECH(t *testing.T) {
	resetECHCacheForTest(t)

	sni := "example.com"
	fakeList := []byte{0x00, 0x02, 0x11, 0x22}
	pemBody := string(pem.EncodeToMemory(&pem.Block{Type: "ECH CONFIGS", Bytes: fakeList}))
	getECHCache().put(sni, pemBody, time.Hour)

	q := url.Values{}
	q.Set("sni", sni)
	q.Set("ech", "1")
	tls, err := buildTrojanTLSOptions(q, false)
	if err != nil {
		t.Fatalf("buildTrojanTLSOptions: %v", err)
	}
	if tls.ECH == nil || !tls.ECH.Enabled {
		t.Fatalf("expected ECH enabled, got %#v", tls.ECH)
	}
	if len(tls.ECH.Config) == 0 {
		t.Fatal("expected cached ECH Config on trojan TLS")
	}
}

func TestBuild_DNSLocalNoUpstreamDetour(t *testing.T) {
	resetECHCacheForTest(t)

	// Seed ECH so Build does not need live DNS for the CF node.
	sni := "alice01.799989.xyz"
	fakeList := []byte{0x00, 0x03, 0x01, 0x02, 0x03}
	pemBody := string(pem.EncodeToMemory(&pem.Block{Type: "ECH CONFIGS", Bytes: fakeList}))
	getECHCache().put(sni, pemBody, time.Hour)

	cfg := &config.Config{
		Mode:          "pool",
		LogLevel:      "warn",
		UpstreamProxy: "socks5://127.0.0.1:7890",
		Listener: config.ListenerConfig{
			Address:  "127.0.0.1",
			Port:     18080,
			Username: "u",
			Password: "p",
		},
		Management: config.ManagementConfig{
			ClashAPIListen: "127.0.0.1:19090",
		},
		Nodes: []config.NodeConfig{
			{
				Name: "cf-ech",
				URI:  "vless://53d2e2ce-fcc4-4e5e-aeab-0bfcfd164636@cloudflare.182682.xyz:8443?encryption=none&security=tls&type=ws&sni=alice01.799989.xyz&ech=1&path=%2Fpath&host=alice01.799989.xyz&fp=chrome",
			},
		},
	}

	opts, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if opts.DNS == nil || len(opts.DNS.Servers) == 0 {
		t.Fatal("expected DNS servers")
	}
	if opts.DNS.Final != "local" {
		t.Fatalf("dns final=%q want local (no DoH via upstream)", opts.DNS.Final)
	}
	if opts.Route == nil || opts.Route.DefaultDomainResolver == nil || opts.Route.DefaultDomainResolver.Server != "local" {
		t.Fatalf("route default domain resolver missing/wrong: %#v", opts.Route)
	}

	var sawLocal, sawUDP, sawDoH bool
	for _, s := range opts.DNS.Servers {
		switch s.Tag {
		case "local":
			sawLocal = true
		case "udp-cloudflare", "udp-google":
			sawUDP = true
			if s.Type != C.DNSTypeUDP {
				t.Fatalf("%s type=%s want udp", s.Tag, s.Type)
			}
			remote, ok := s.Options.(*option.RemoteDNSServerOptions)
			if !ok {
				t.Fatalf("%s options type %T", s.Tag, s.Options)
			}
			if remote.Detour != "" {
				t.Fatalf("%s must not use upstream detour, got %q", s.Tag, remote.Detour)
			}
		case "doh":
			sawDoH = true
		}
	}
	if !sawLocal || !sawUDP {
		t.Fatalf("dns servers incomplete local=%v udp=%v", sawLocal, sawUDP)
	}
	if sawDoH {
		t.Fatal("DoH-via-upstream must not be present")
	}

	// VLESS outbound must have ECH enabled with injected Config.
	var foundECH bool
	for _, ob := range opts.Outbounds {
		if ob.Type != C.TypeVLESS {
			continue
		}
		switch o := ob.Options.(type) {
		case *option.VLESSOutboundOptions:
			if o.TLS == nil || o.TLS.ECH == nil || !o.TLS.ECH.Enabled {
				t.Fatalf("VLESS TLS ECH not enabled: %#v", o.TLS)
			}
			if len(o.TLS.ECH.Config) == 0 {
				t.Fatal("expected prefetched ECH Config on VLESS outbound")
			}
			foundECH = true
		case option.VLESSOutboundOptions:
			if o.TLS == nil || o.TLS.ECH == nil || !o.TLS.ECH.Enabled {
				t.Fatalf("VLESS TLS ECH not enabled: %#v", o.TLS)
			}
			if len(o.TLS.ECH.Config) == 0 {
				t.Fatal("expected prefetched ECH Config on VLESS outbound")
			}
			foundECH = true
		default:
			t.Fatalf("unexpected VLESS options type %T", ob.Options)
		}
	}
	if !foundECH {
		t.Fatal("no VLESS outbound with ECH")
	}
}

func TestBuild_DNSLocalWithoutUpstream(t *testing.T) {
	resetECHCacheForTest(t)

	cfg := &config.Config{
		Mode:     "pool",
		LogLevel: "warn",
		Listener: config.ListenerConfig{
			Address: "127.0.0.1",
			Port:    18081,
		},
		Management: config.ManagementConfig{
			ClashAPIListen: "127.0.0.1:19091",
		},
		Nodes: []config.NodeConfig{
			{
				Name: "plain",
				URI:  "vless://00000000-0000-0000-0000-000000000001@example.com:443?encryption=none&security=tls&sni=example.com",
			},
		},
	}
	opts, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if opts.DNS == nil || opts.DNS.Final != "local" {
		t.Fatalf("expected local DNS final, got %#v", opts.DNS)
	}
}

func TestECHCache_PersistAndLoad(t *testing.T) {
	dir := resetECHCacheForTest(t)

	c := getECHCache()
	pemBody := string(pem.EncodeToMemory(&pem.Block{Type: "ECH CONFIGS", Bytes: []byte{1, 2, 3, 4}}))
	c.put("test.example", pemBody, time.Hour)

	// Force reload from disk.
	globalECHCache = nil
	globalECHCacheOnce = sync.Once{}
	c2 := getECHCache()
	got, ok := c2.get("test.example")
	if !ok || !strings.Contains(got, "BEGIN ECH CONFIGS") {
		t.Fatalf("cache reload failed: ok=%v got=%q path=%s", ok, got, c2.path)
	}
	if _, err := os.Stat(filepath.Join(dir, "ech-configs.json")); err != nil {
		t.Fatalf("cache file missing: %v", err)
	}
}

func TestFetchECHConfigList_Live(t *testing.T) {
	if testing.Short() {
		t.Skip("live DNS")
	}
	list, ttl, err := fetchECHConfigList("alice01.799989.xyz")
	if err != nil {
		t.Fatalf("fetchECHConfigList: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("empty ECH list")
	}
	// DNS may return a short remaining TTL; cache put() clamps to ≥5m.
	if ttl <= 0 {
		t.Fatalf("unexpected ttl %v", ttl)
	}
}
