package monitor

import (
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
