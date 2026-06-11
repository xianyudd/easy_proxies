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
		{name: "reputation update regions", path: "/api/reputation/check?region=all&mode=multi-port&count=1&update_regions=maybe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			srv.srv.Handler.ServeHTTP(rec, req)

			assertMonitorAPIErrorCode(t, rec, http.StatusBadRequest, "invalid_bool")
		})
	}
}

func TestQualityCompanionChecksRejectNegativeAndOverflowCount(t *testing.T) {
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
		{name: "cloudflare negative", path: "/api/cloudflare/check?region=all&mode=multi-port&count=-1"},
		{name: "cloudflare overflow", path: "/api/cloudflare/check?region=all&mode=multi-port&count=999999999999999999999999999"},
		{name: "reputation negative", path: "/api/reputation/check?region=all&mode=multi-port&count=-1"},
		{name: "reputation overflow", path: "/api/reputation/check?region=all&mode=multi-port&count=999999999999999999999999999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()

			srv.srv.Handler.ServeHTTP(rec, req)

			assertMonitorAPIErrorCode(t, rec, http.StatusBadRequest, "invalid_count")
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

func TestCloudflareCacheHandlerFiltersAndOverlaysCurrentNodeMetadata(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	node := mgr.Register(NodeInfo{
		Tag:           "node-a",
		Name:          "Node A Current",
		URI:           "http://1.1.1.1:80",
		ListenAddress: "127.0.0.1",
		Port:          31001,
		Region:        "sg",
	})
	node.MarkInitialCheckDone(true)
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	srv.cfChecker.Cache().Set("node-a", cloudflarecheck.Result{NodeName: "Node A Old", NodeTag: "node-a", Region: "other", Port: 0, Level: "excellent"})
	srv.cfChecker.Cache().Set("node-stale", cloudflarecheck.Result{NodeName: "Stale", NodeTag: "node-stale", Region: "other", Port: 39999, Level: "excellent"})

	req := httptest.NewRequest(http.MethodGet, "/api/cloudflare/cache", nil)
	rec := httptest.NewRecorder()
	srv.handleCloudflareCache(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("cache status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Count int                      `json:"count"`
		Data  []cloudflarecheck.Result `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Count != 1 || len(body.Data) != 1 {
		t.Fatalf("expected only current node cache row, got %#v body=%s", body, rec.Body.String())
	}
	got := body.Data[0]
	if got.NodeTag != "node-a" || got.NodeName != "Node A Current" || got.Region != "sg" || got.Port != 31001 {
		t.Fatalf("cache row should use current node metadata, got %#v", got)
	}
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

func TestCloudflareCheckerCacheSurvivesQualityConfigReload(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	srv.cfChecker.Cache().Set("node-a", cloudflarecheck.Result{NodeName: "node-a", NodeTag: "node-a", Region: "us", Level: "failed"})
	if got := len(srv.cfChecker.CacheList()); got != 1 {
		t.Fatalf("cloudflare cache before reload = %d, want 1", got)
	}

	srv.SetConfig(&config.Config{QualityCheck: config.QualityCheckConfig{CloudflareTimeout: 3500 * time.Millisecond, CloudflareConcurrency: 24}})

	timeout, concurrency := srv.cfChecker.Settings()
	if timeout != 3500*time.Millisecond || concurrency != 24 {
		t.Fatalf("cloudflare checker settings = %s/%d, want 3.5s/24", timeout, concurrency)
	}
	if got := len(srv.cfChecker.CacheList()); got != 1 {
		t.Fatalf("cloudflare cache after reload = %d, want 1", got)
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

func TestApplyReputationRegionResultsUpdatesRuntimeRegionFromExitCountryCode(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	node := mgr.Register(NodeInfo{Tag: "node-a", Name: "Node A", URI: "http://1.1.1.1:80", Region: "my", Country: "Malaysia", Source: "subscription", Port: 13001})
	node.MarkInitialCheckDone(true)
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)

	summary := srv.applyReputationRegionResults([]reputation.NodeResult{{
		NodeTag: "node-a",
		Result:  &reputation.Result{IP: "50.7.253.170", Country: "Singapore", CountryCode: "SG"},
	}})

	if summary.Updated != 1 || summary.NeedReload != false {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	snap, err := mgr.SnapshotFor("node-a")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Region != "sg" || snap.Country != "新加坡" {
		t.Fatalf("region not updated from exit country: region=%q country=%q", snap.Region, snap.Country)
	}
}

func TestApplyReputationRegionResultsIgnoresInvalidOrFailedCountryCode(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	mgr.Register(NodeInfo{Tag: "node-a", Name: "Node A", URI: "http://1.1.1.1:80", Region: "my", Country: "Malaysia", Source: "subscription", Port: 13001})
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)

	summary := srv.applyReputationRegionResults([]reputation.NodeResult{
		{NodeTag: "node-a", Error: "probe failed", Result: &reputation.Result{CountryCode: "SG"}},
		{NodeTag: "node-a", Result: &reputation.Result{CountryCode: "ZZ"}},
	})

	if summary.Updated != 0 || summary.Skipped == 0 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	snap, err := mgr.SnapshotFor("node-a")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Region != "my" {
		t.Fatalf("region should remain unchanged, got %q", snap.Region)
	}
}

func TestCacheHandlersReturnEmptyArraysInsteadOfNull(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "cloudflare", path: "/api/cloudflare/cache"},
		{name: "reputation", path: "/api/reputation/cache"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			srv.srv.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var body struct {
				Count int             `json:"count"`
				Data  json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Count != 0 || string(body.Data) != "[]" {
				t.Fatalf("empty cache should return data:[], got count=%d data=%s body=%s", body.Count, body.Data, rec.Body.String())
			}
		})
	}
}
