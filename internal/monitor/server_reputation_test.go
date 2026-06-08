package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy_proxies/internal/cloudflarecheck"
	"easy_proxies/internal/config"
	"easy_proxies/internal/reputation"
)

func TestReputationExpectedCountry(t *testing.T) {
	if got := reputationExpectedCountry("jp"); got != "JP" {
		t.Fatalf("expected JP, got %q", got)
	}
	if got := reputationExpectedCountry("all"); got != "" {
		t.Fatalf("expected empty for all, got %q", got)
	}
}

func TestSummarizeReputation(t *testing.T) {
	got := summarizeReputation([]reputation.NodeResult{
		{Result: &reputation.Result{RiskLevel: "low"}},
		{Result: &reputation.Result{RiskLevel: "medium"}},
		{Result: &reputation.Result{RiskLevel: "high"}},
		{Error: "failed"},
	})
	if got["low"] != 1 || got["medium"] != 1 || got["high"] != 1 || got["failed"] != 1 {
		t.Fatalf("unexpected summary: %#v", got)
	}
}

func TestReputationHTTPHandlers(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	if srv == nil || srv.srv == nil {
		t.Fatal("expected server")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/reputation/cache", nil)
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cache status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/reputation/check?region=bad", nil)
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	assertMonitorAPIErrorCode(t, rec, http.StatusBadRequest, "invalid_region")
}

func TestReputationCheckRequiresBackgroundForLargeSyncCount(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	if srv == nil || srv.srv == nil {
		t.Fatal("expected server")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/reputation/check?region=all&mode=multi-port&count=999999", nil)
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("large sync count status=%d, want 400 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "background") {
		t.Fatalf("large sync count should explain background mode, body=%s", rec.Body.String())
	}
}

func TestReputationCheckReturnsStructuredErrorCodes(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	if srv == nil || srv.srv == nil {
		t.Fatal("expected server")
	}

	for _, tc := range []struct {
		name string
		path string
		code string
	}{
		{name: "invalid count", path: "/api/reputation/check?region=all&mode=multi-port&count=bad", code: "invalid_count"},
		{name: "invalid region", path: "/api/reputation/check?region=bad&mode=multi-port&count=1", code: "invalid_region"},
		{name: "invalid mode", path: "/api/reputation/check?region=all&mode=single&count=1", code: "invalid_mode"},
		{name: "missing ip", path: "/api/reputation/ip", code: "missing_ip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			srv.srv.Handler.ServeHTTP(rec, req)
			assertMonitorAPIErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestQualityCompanionChecksRejectInvalidBoolQuery(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	if srv == nil || srv.srv == nil {
		t.Fatal("expected server")
	}

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "cloudflare include unavailable", path: "/api/cloudflare/check?region=all&mode=multi-port&count=1&include_unavailable=maybe"},
		{name: "cloudflare retry failed", path: "/api/cloudflare/check?region=all&mode=multi-port&count=1&retry_failed=maybe"},
		{name: "reputation include unavailable", path: "/api/reputation/check?region=all&mode=multi-port&count=1&include_unavailable=maybe"},
		{name: "reputation retry failed", path: "/api/reputation/check?region=all&mode=multi-port&count=1&retry_failed=maybe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			srv.srv.Handler.ServeHTTP(rec, req)

			assertMonitorAPIErrorCode(t, rec, http.StatusBadRequest, "invalid_bool")
		})
	}
}

func TestQualityCompanionHandlersRejectMethodsWithStructuredCode(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	if srv == nil || srv.srv == nil {
		t.Fatal("expected server")
	}

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "cloudflare check", method: http.MethodPost, path: "/api/cloudflare/check"},
		{name: "cloudflare cache", method: http.MethodPatch, path: "/api/cloudflare/cache"},
		{name: "reputation ip", method: http.MethodPost, path: "/api/reputation/ip"},
		{name: "reputation check", method: http.MethodPost, path: "/api/reputation/check"},
		{name: "reputation cache", method: http.MethodPatch, path: "/api/reputation/cache"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			srv.srv.Handler.ServeHTTP(rec, req)
			assertMonitorAPIErrorCode(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
		})
	}
}

func TestSummarizeCloudflare(t *testing.T) {
	got := summarizeCloudflare([]cloudflarecheck.Result{
		{Level: "excellent"},
		{Level: "good"},
		{Level: "fair"},
		{Level: "poor"},
		{Error: "dial failed"},
	})
	if got["excellent"] != 1 || got["good"] != 1 || got["fair"] != 1 || got["poor"] != 1 || got["failed"] != 1 {
		t.Fatalf("unexpected summary: %#v", got)
	}
}

func TestCloudflareHTTPHandlers(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	if srv == nil || srv.srv == nil {
		t.Fatal("expected server")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/cloudflare/cache", nil)
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cache status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/cloudflare/check?region=bad", nil)
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	assertMonitorAPIErrorCode(t, rec, http.StatusBadRequest, "invalid_region")

	req = httptest.NewRequest(http.MethodGet, "/api/cloudflare/check?region=jp&count=bad", nil)
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	assertMonitorAPIErrorCode(t, rec, http.StatusBadRequest, "invalid_count")
}

func TestCloudflareCheckRequiresBackgroundForLargeSyncCount(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	if srv == nil || srv.srv == nil {
		t.Fatal("expected server")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/cloudflare/check?region=all&mode=multi-port&count=999999", nil)
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("large sync count status=%d, want 400 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "background") {
		t.Fatalf("large sync count should explain background mode, body=%s", rec.Body.String())
	}
}

func TestCloudflareCheckReturnsStructuredErrorCodes(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	if srv == nil || srv.srv == nil {
		t.Fatal("expected server")
	}

	for _, tc := range []struct {
		name string
		path string
		code string
	}{
		{name: "invalid count", path: "/api/cloudflare/check?region=all&mode=multi-port&count=bad", code: "invalid_count"},
		{name: "invalid region", path: "/api/cloudflare/check?region=bad&mode=multi-port&count=1", code: "invalid_region"},
		{name: "invalid mode", path: "/api/cloudflare/check?region=all&mode=single&count=1", code: "invalid_mode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			srv.srv.Handler.ServeHTTP(rec, req)
			assertMonitorAPIErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestCloudflareCheckerUsesQualityConfig(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	srv.SetConfig(&config.Config{QualityCheck: config.QualityCheckConfig{CloudflareTimeout: 3500 * time.Millisecond, CloudflareConcurrency: 24}})

	timeout, concurrency := srv.cfChecker.Settings()
	if timeout != 3500*time.Millisecond || concurrency != 24 {
		t.Fatalf("cloudflare checker settings = %s/%d, want 3.5s/24", timeout, concurrency)
	}
}

func assertMonitorAPIErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status=%d, want %d body=%s", rec.Code, status, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error == "" || body.Code != code {
		t.Fatalf("unexpected body: %#v raw=%s", body, rec.Body.String())
	}
}
