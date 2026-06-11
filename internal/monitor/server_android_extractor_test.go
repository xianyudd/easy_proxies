package monitor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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

	if got := androidExtractorPort(cfg, nil, geoip.RegionCH); got != 13019 {
		t.Fatalf("expected override port 13019, got %d", got)
	}
	if got := androidExtractorPort(cfg, nil, geoip.RegionUS); got != 13001 {
		t.Fatalf("expected base mapped port 13001, got %d", got)
	}
	if got := androidExtractorPort(cfg, nil, geoip.RegionOther); got != 13011 {
		t.Fatalf("expected existing other fallback port 13011, got %d", got)
	}
	if got := androidExtractorPort(cfg, nil, geoip.RegionDE); got != 13012 {
		t.Fatalf("expected germany fallback port 13012, got %d", got)
	}
}

func TestAndroidExtractorPortSkipsMultiPortNodeRange(t *testing.T) {
	t.Parallel()

	cfg := config.AndroidProxyConfig{BasePort: 30150}
	usedPorts := make([]uint16, 0, 857)
	for port := uint16(30000); port <= 32856; port++ {
		usedPorts = append(usedPorts, port)
	}

	if got := androidExtractorPort(cfg, usedPorts, geoip.RegionUS); got != 32857 {
		t.Fatalf("expected android us port to avoid multi-port range and use 32857, got %d", got)
	}
	if got := androidExtractorPort(cfg, usedPorts, geoip.RegionJP); got != 32858 {
		t.Fatalf("expected android jp port to avoid multi-port range and use 32858, got %d", got)
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

func TestFormatExtractorEntryMasksPasswordWhenRevealFalse(t *testing.T) {
	t.Parallel()

	entry := extractorProxyEntry{
		Host:       "127.0.0.1",
		Port:       24001,
		Username:   "user",
		Password:   "secret-pass",
		Path:       "/fr/",
		Region:     geoip.RegionFR,
		Remark:     "FR-Node",
		RefreshURL: "http://127.0.0.1:19093/api/refresh",
		NodeName:   "node-fr",
		NodeTag:    "tag-fr",
	}
	formats := []string{
		"http_url",
		"socks5_url",
		"csv",
		"pipe",
		"curl_command",
		"python_requests_json",
		"clash_yaml",
		"host_port_user_pass",
		"user_pass_at_host_port",
		"host_port_user_pass_refresh_remark",
		"user_pass_at_host_port_refresh_remark",
		"json",
	}

	for _, format := range formats {
		format := format
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			got := formatExtractorEntry(entry, format, false)
			raw := stringifyExtractorEntryForTest(t, got)
			if strings.Contains(raw, entry.Password) {
				t.Fatalf("masked %s output leaked raw password: %s", format, raw)
			}
			if !strings.Contains(raw, "***") {
				t.Fatalf("masked %s output did not include mask marker: %s", format, raw)
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

func TestHandleExtractorAndroidOnlyReturnsConfiguredRegions(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	for idx, region := range []string{geoip.RegionUS, geoip.RegionFR} {
		node := mgr.Register(NodeInfo{
			Tag:           "node-" + region,
			Name:          "Node " + region,
			URI:           "http://127.0.0.1:18080",
			ListenAddress: "127.0.0.1",
			Port:          uint16(32000 + idx),
			Region:        region,
		})
		node.MarkInitialCheckDone(true)
	}
	srv := &Server{
		mgr: mgr,
		cfgSrc: &config.Config{
			AndroidProxy: config.AndroidProxyConfig{
				Enabled:  true,
				Listen:   "127.0.0.1",
				BasePort: 13001,
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/extractor?region=all&mode=android&format=json&count=20", nil)
	rec := httptest.NewRecorder()
	srv.handleExtractor(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	gotRegions := map[string]bool{}
	for _, entry := range body.Entries {
		gotRegions[stringValueForTest(entry["region"])] = true
	}
	if len(gotRegions) != 2 || !gotRegions[geoip.RegionUS] || !gotRegions[geoip.RegionFR] {
		t.Fatalf("unexpected android regions: %#v body=%s", gotRegions, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/extractor?region=ad&mode=android&format=json&count=1", nil)
	rec = httptest.NewRecorder()
	srv.handleExtractor(rec, req)
	assertExtractorErrorCode(t, rec, http.StatusBadRequest, "extractor_error")
}

func TestHandleExtractorAndroidSkipsRuntimeMultiPortRange(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	for port := uint16(30000); port <= 32856; port++ {
		node := mgr.Register(NodeInfo{
			Tag:           fmt.Sprintf("node-%d", port),
			Name:          fmt.Sprintf("Node %d", port),
			URI:           "http://127.0.0.1:18080",
			ListenAddress: "127.0.0.1",
			Port:          port,
			Region:        geoip.RegionUS,
		})
		node.MarkInitialCheckDone(true)
	}
	srv := &Server{
		mgr: mgr,
		cfgSrc: &config.Config{
			AndroidProxy: config.AndroidProxyConfig{
				Enabled:  true,
				Listen:   "127.0.0.1",
				BasePort: 30150,
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/extractor?region=us&mode=android&format=json&count=1", nil)
	rec := httptest.NewRecorder()
	srv.handleExtractor(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) != 1 {
		t.Fatalf("expected 1 android entry, got %d body=%s", len(body.Entries), rec.Body.String())
	}
	if got := intValueForTest(body.Entries[0]["port"]); got != 32857 {
		t.Fatalf("expected android extractor port 32857, got %d body=%s", got, rec.Body.String())
	}
}

func stringValueForTest(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func intValueForTest(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func stringifyExtractorEntryForTest(t *testing.T, value any) string {
	t.Helper()
	switch v := value.(type) {
	case string:
		return v
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal extractor entry: %v", err)
		}
		return string(raw)
	}
}
