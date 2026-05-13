package monitor

import (
	"reflect"
	"testing"

	"easy_proxies/internal/config"
	"easy_proxies/internal/geoip"
)

func TestAndroidExtractorPortUsesOverride(t *testing.T) {
	t.Parallel()

	cfg := config.AndroidProxyConfig{
		BasePort: 13001,
		RegionPorts: map[string]uint16{
			geoip.RegionCH: 13019,
		},
	}

	if got := androidExtractorPort(cfg, geoip.RegionCH); got != 13019 {
		t.Fatalf("expected override port 13019, got %d", got)
	}
	if got := androidExtractorPort(cfg, geoip.RegionUS); got != 13001 {
		t.Fatalf("expected base mapped port 13001, got %d", got)
	}
	if got := androidExtractorPort(cfg, geoip.RegionOther); got != 13011 {
		t.Fatalf("expected existing other fallback port 13011, got %d", got)
	}
	if got := androidExtractorPort(cfg, geoip.RegionDE); got != 13012 {
		t.Fatalf("expected germany fallback port 13012, got %d", got)
	}
}

func TestFormatExtractorEntryAdditionalFormats(t *testing.T) {
	t.Parallel()

	entry := extractorProxyEntry{
		Host:     "127.0.0.1",
		Port:     24001,
		Username: "user",
		Password: "pass",
		Region:   geoip.RegionJP,
		Remark:   "JP-Node",
	}

	cases := []struct {
		name   string
		format string
		want   any
	}{
		{name: "http no auth", format: "http_no_auth", want: "http://127.0.0.1:24001"},
		{name: "socks5 auth", format: "socks5_url", want: "socks5://user:pass@127.0.0.1:24001"},
		{name: "socks5 no auth", format: "socks5_no_auth", want: "socks5://127.0.0.1:24001"},
		{name: "csv", format: "csv", want: "127.0.0.1,24001,user,pass"},
		{name: "pipe", format: "pipe", want: "127.0.0.1|24001|user|pass"},
		{name: "curl", format: "curl_command", want: "curl -x http://user:pass@127.0.0.1:24001 http://cp.cloudflare.com/generate_204"},
		{name: "python requests", format: "python_requests_json", want: map[string]any{
			"http":  "http://user:pass@127.0.0.1:24001",
			"https": "http://user:pass@127.0.0.1:24001",
		}},
		{name: "clash", format: "clash_yaml", want: map[string]any{
			"name":     "JP-Node",
			"type":     "http",
			"server":   "127.0.0.1",
			"port":     uint16(24001),
			"username": "user",
			"password": "pass",
		}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := formatExtractorEntry(entry, tc.format, true); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("expected %#v, got %#v", tc.want, got)
			}
		})
	}
}

func TestFormatExtractorEntryADBCommand(t *testing.T) {
	t.Parallel()

	got := formatExtractorEntry(extractorProxyEntry{Host: "127.0.0.1", Port: 13019}, "adb_command", true)
	want := "adb shell settings put global http_proxy 127.0.0.1:13019"
	if got != want {
		t.Fatalf("expected %q, got %v", want, got)
	}
}
