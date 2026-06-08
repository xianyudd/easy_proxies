package monitor

import (
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
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid region status=%d body=%s", rec.Code, rec.Body.String())
	}
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
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid region status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/cloudflare/check?region=jp&count=bad", nil)
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid count status=%d body=%s", rec.Code, rec.Body.String())
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
