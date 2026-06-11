package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy_proxies/internal/config"
)

func TestHandleExportOnlyIncludesCheckedAvailableNodes(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	ok := mgr.Register(NodeInfo{Tag: "ok", Name: "OK", URI: "http://1.1.1.1:80", ListenAddress: "127.0.0.1", Port: 31001})
	ok.MarkInitialCheckDone(true)
	failed := mgr.Register(NodeInfo{Tag: "failed", Name: "Failed", URI: "http://2.2.2.2:80", ListenAddress: "127.0.0.1", Port: 31002})
	failed.MarkInitialCheckDone(false)
	unchecked := mgr.Register(NodeInfo{Tag: "unchecked", Name: "Unchecked", URI: "http://3.3.3.3:80", ListenAddress: "127.0.0.1", Port: 31003})
	unchecked.MarkAvailable(false)
	blacklisted := mgr.Register(NodeInfo{Tag: "blacklisted", Name: "Blacklisted", URI: "http://4.4.4.4:80", ListenAddress: "127.0.0.1", Port: 31004})
	blacklisted.MarkInitialCheckDone(true)
	blacklisted.Blacklist(time.Now().Add(time.Hour))

	srv := &Server{
		mgr: mgr,
		cfg: Config{ProxyUsername: "user", ProxyPassword: "pass"},
		cfgSrc: &config.Config{
			Mode: "multi-port",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/export?scheme=http", nil)
	rec := httptest.NewRecorder()
	srv.handleExport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "127.0.0.1:31001") {
		t.Fatalf("expected available node in export, got %q", body)
	}
	for _, port := range []string{"31002", "31003", "31004"} {
		if strings.Contains(body, port) {
			t.Fatalf("export should not contain unavailable/unchecked/blacklisted port %s: %q", port, body)
		}
	}
}

func TestHandleExportSocks5DoesNotIncludeGeoIPHTTPEntry(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{
		mgr: mgr,
		cfg: Config{ProxyUsername: "user", ProxyPassword: "pass"},
		cfgSrc: &config.Config{
			Mode:     "multi-port",
			Listener: config.ListenerConfig{Username: "user", Password: "pass"},
			GeoIP:    config.GeoIPConfig{Enabled: true, Listen: "127.0.0.1", Port: 1221},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/export?scheme=socks5", nil)
	rec := httptest.NewRecorder()
	srv.handleExport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "GeoIP") || strings.Contains(body, "http://") {
		t.Fatalf("socks5 export should not include HTTP-only GeoIP entries: %q", body)
	}
}

func TestHandleExportRejectsInvalidSchemeWithStructuredCode(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{mgr: mgr}

	req := httptest.NewRequest(http.MethodGet, "/api/export?scheme=ftp", nil)
	rec := httptest.NewRecorder()
	srv.handleExport(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error == "" || body.Code != "invalid_scheme" {
		t.Fatalf("unexpected body: %#v raw=%s", body, rec.Body.String())
	}
}

func TestHandleExportGeoIPDocumentsUsernameSuffixInsteadOfPath(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{
		mgr: mgr,
		cfg: Config{ProxyUsername: "user", ProxyPassword: "pass"},
		cfgSrc: &config.Config{
			Mode:     "hybrid",
			Listener: config.ListenerConfig{Address: "127.0.0.1", Port: 12080, Username: "user", Password: "pass"},
			GeoIP:    config.GeoIPConfig{Enabled: true, Listen: "127.0.0.1", Port: 1221},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/export?scheme=http", nil)
	rec := httptest.NewRecorder()
	srv.handleExport(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "地区使用用户名后缀") || !strings.Contains(body, "user-us") {
		t.Fatalf("geoip export should document username suffixes: %q", body)
	}
	if strings.Contains(body, "支持路径") || strings.Contains(body, "/us/") {
		t.Fatalf("geoip export should not advertise path-based routing: %q", body)
	}
}
