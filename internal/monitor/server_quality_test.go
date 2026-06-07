package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"easy_proxies/internal/cloudflarecheck"
	"easy_proxies/internal/quality"
)

type monitorFakeRunner struct{}

func (monitorFakeRunner) CheckQuick(ctx context.Context, target quality.Target) quality.Result {
	if target.NodeTag == "node-b" {
		return quality.Result{Kind: quality.CheckQuick, Target: target, TargetIndex: target.Index, TargetID: target.ID, Status: "failed", Success: false, Error: "connect refused", Quick: map[string]any{"status": "failed", "failure_reason": "connect_refused"}}
	}
	return quality.Result{Kind: quality.CheckQuick, Target: target, TargetIndex: target.Index, TargetID: target.ID, Status: "completed", Success: true, Quick: map[string]any{"status": "ok"}}
}

func (monitorFakeRunner) CheckCloudflare(ctx context.Context, target quality.Target) quality.Result {
	return quality.Result{Kind: quality.CheckCloudflare, Target: target, TargetIndex: target.Index, TargetID: target.ID, Status: "completed", Success: true, Score: 90, CF: map[string]any{"level": "excellent"}}
}

func (monitorFakeRunner) CheckReputation(ctx context.Context, target quality.Target, expectedCountry string) quality.Result {
	return quality.Result{Kind: quality.CheckReputation, Target: target, TargetIndex: target.Index, TargetID: target.ID, Status: "completed", Success: true, Score: 80, Reputation: map[string]any{"risk_level": "low"}}
}

func TestQualityJobAPI(t *testing.T) {
	srv := newQualityAPITestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/quality/jobs", strings.NewReader(`{"kind":"combined","region":"all","count":2,"include_unavailable":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		JobID       string `json:"job_id"`
		Status      string `json:"status"`
		Kind        string `json:"kind"`
		ProgressURL string `json:"progress_url"`
		ResultsURL  string `json:"results_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.JobID == "" || created.ProgressURL == "" || created.ResultsURL == "" || created.Kind != "combined" {
		t.Fatalf("bad create response: %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, created.ProgressURL, nil)
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("progress status=%d body=%s", rec.Code, rec.Body.String())
	}
	var snap quality.JobSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.ID != created.JobID || snap.Total != 2 {
		t.Fatalf("bad snapshot: %#v", snap)
	}

	waitForMonitorQualityJob(t, srv, created.JobID)
	req = httptest.NewRequest(http.MethodGet, created.ResultsURL+"?page=1&page_size=1", nil)
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("results status=%d body=%s", rec.Code, rec.Body.String())
	}
	var page quality.PagedResults
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Count != 2 || page.Page != 1 || page.PageSize != 1 || !page.HasNext || len(page.Data) != 1 {
		t.Fatalf("bad page: %#v", page)
	}
}

func TestQualityJobAPICancelAndErrors(t *testing.T) {
	srv := newQualityAPITestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/quality/jobs", strings.NewReader(`{"kind":"bad"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/quality/jobs/missing", nil)
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing job status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/quality/jobs", strings.NewReader(`{"kind":"cloudflare","count":2,"include_unavailable":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/quality/jobs/"+created.JobID+"/cancel", nil)
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func newQualityAPITestServer(t *testing.T) *Server {
	t.Helper()
	mgr, err := NewManager(Config{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	a := mgr.Register(NodeInfo{Tag: "node-a", Name: "Node A", URI: "http://1.1.1.1:80", ListenAddress: "127.0.0.1", Port: 13001, Region: "sg", Source: "free_proxy"})
	a.MarkInitialCheckDone(true)
	b := mgr.Register(NodeInfo{Tag: "node-b", Name: "Node B", URI: "http://2.2.2.2:80", ListenAddress: "127.0.0.1", Port: 13002, Region: "us", Source: "subscription"})
	b.MarkInitialCheckDone(true)
	srv := NewServer(Config{Enabled: true, Listen: "127.0.0.1:0"}, mgr, nil)
	if srv == nil || srv.srv == nil {
		t.Fatal("expected server")
	}
	srv.qualitySvc = quality.NewService(quality.ServiceOptions{TargetSource: newMonitorQualityTargetSource(srv), QuickRunner: monitorFakeRunner{}, CloudflareRunner: monitorFakeRunner{}, ReputationRunner: monitorFakeRunner{}, MaxWorkers: 2})
	return srv
}

func TestQualityPipelineJobAPI(t *testing.T) {
	srv := newQualityAPITestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/quality/jobs", strings.NewReader(`{"kind":"pipeline","region":"all","count":2,"include_unavailable":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		JobID string `json:"job_id"`
		Kind  string `json:"kind"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Kind != "pipeline" {
		t.Fatalf("kind = %q, want pipeline", created.Kind)
	}
	got := waitForMonitorQualityJob(t, srv, created.JobID)
	if got.Summary.Quick["ok"] != 1 || got.Summary.Quick["failed"] != 1 || got.Summary.Final["recommend"] != 1 {
		t.Fatalf("unexpected pipeline summary: %#v", got.Summary)
	}
}

func waitForMonitorQualityJob(t *testing.T, srv *Server, id string) quality.JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snap, ok := srv.qualitySvc.GetJob(id)
		if ok && (snap.Status == quality.JobCompleted || snap.Status == quality.JobFailed || snap.Status == quality.JobCancelled) {
			return snap
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("quality job %s did not finish", id)
	return quality.JobSnapshot{}
}

func TestLegacyQualityCheckBackgroundModeCreatesJob(t *testing.T) {
	srv := newQualityAPITestServer(t)

	for _, tc := range []struct {
		name string
		path string
		kind string
	}{
		{name: "cloudflare", path: "/api/cloudflare/check?background=true&region=all&count=2&include_unavailable=true", kind: "cloudflare"},
		{name: "reputation", path: "/api/reputation/check?async=true&region=all&count=2&include_unavailable=true", kind: "reputation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			srv.srv.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var created struct {
				JobID string `json:"job_id"`
				Kind  string `json:"kind"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
				t.Fatal(err)
			}
			if created.JobID == "" || created.Kind != tc.kind {
				t.Fatalf("bad created response: %#v", created)
			}
			waitForMonitorQualityJob(t, srv, created.JobID)
		})
	}
}

func TestLegacyBackgroundCountIsNotClampedToSyncLimit(t *testing.T) {
	srv := newQualityAPITestServer(t)
	for i := 2; i < 600; i++ {
		node := srv.mgr.Register(NodeInfo{Tag: "node-extra-" + strconv.Itoa(i), Name: "Node Extra", URI: "http://10.0.0." + strconv.Itoa(i%250+1) + ":80", ListenAddress: "127.0.0.1", Port: uint16(14000 + i), Region: "sg", Source: "test"})
		node.MarkAvailable(true)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/cloudflare/check?background=true&region=all&count=600&include_unavailable=true", nil)
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	snap, ok := srv.qualitySvc.GetJob(created.JobID)
	if !ok {
		t.Fatal("expected created job")
	}
	if snap.Query.Kind != quality.CheckCloudflare {
		t.Fatalf("query kind = %q, want cloudflare", snap.Query.Kind)
	}
	if snap.Total != 600 {
		t.Fatalf("background count should not be clamped to sync 500 limit, got total=%d", snap.Total)
	}
}

func TestMonitorQualityRetryFailedFiltersTargets(t *testing.T) {
	srv := newQualityAPITestServer(t)
	cache := cloudflarecheck.NewCache(time.Hour)
	cache.Set("node-b", cloudflarecheck.Result{NodeTag: "node-b", Level: "failed", Error: "dial failed"})
	srv.cfChecker = cloudflarecheck.NewChecker(cloudflarecheck.WithCache(cache))

	targets, err := newMonitorQualityTargetSource(srv).ListTargets(context.Background(), quality.TargetQuery{Kind: quality.CheckCloudflare, Region: "all", IncludeUnavailable: true, RetryFailed: true})
	if err != nil {
		t.Fatalf("ListTargets returned error: %v", err)
	}
	if len(targets) != 1 || targets[0].NodeTag != "node-b" || targets[0].Index != 0 {
		t.Fatalf("retry_failed should select and reindex only failed target, got %#v", targets)
	}
	if !targets[0].Retry {
		t.Fatalf("retry_failed target should be marked for cache bypass: %#v", targets[0])
	}
}

func TestMonitorQualityTargetSourceFiltersSource(t *testing.T) {
	srv := newQualityAPITestServer(t)
	srv.mgr.Register(NodeInfo{Tag: "node-free-unchecked", Name: "Free Unchecked", URI: "http://3.3.3.3:80", ListenAddress: "127.0.0.1", Port: 13003, Region: "sg", Source: "free_proxy"})

	targets, err := newMonitorQualityTargetSource(srv).ListTargets(context.Background(), quality.TargetQuery{Region: "all", Source: "free_proxy", IncludeUnavailable: true})
	if err != nil {
		t.Fatalf("ListTargets returned error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("source filter with include_unavailable should include unchecked free_proxy target, got %#v", targets)
	}
	seen := map[string]bool{}
	for _, target := range targets {
		if target.Source != "free_proxy" {
			t.Fatalf("unexpected non-free source-filtered target: %#v", target)
		}
		seen[target.NodeTag] = true
	}
	if !seen["node-a"] || !seen["node-free-unchecked"] {
		t.Fatalf("unexpected source-filtered targets: %#v", targets)
	}

	availableTargets, err := newMonitorQualityTargetSource(srv).ListTargets(context.Background(), quality.TargetQuery{Region: "all", Source: "free_proxy", IncludeUnavailable: false})
	if err != nil {
		t.Fatalf("ListTargets returned error: %v", err)
	}
	if len(availableTargets) != 1 || availableTargets[0].NodeTag != "node-a" {
		t.Fatalf("source filter without include_unavailable should select only checked available targets, got %#v", availableTargets)
	}
}

func TestLegacyBackgroundQualityCheckAcceptsSourceFilter(t *testing.T) {
	srv := newQualityAPITestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/cloudflare/check?background=true&region=all&source=free_proxy&count=10&include_unavailable=true", nil)
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	snap := waitForMonitorQualityJob(t, srv, created.JobID)
	if snap.Total != 1 || snap.Query.Source != "free_proxy" {
		t.Fatalf("expected source-filtered job, got %#v", snap)
	}
	page := srv.qualitySvc.ListResults(created.JobID, quality.ResultQuery{Page: 1, PageSize: 10})
	if page.Count != 1 || len(page.Data) != 1 || page.Data[0].Source != "free_proxy" {
		t.Fatalf("unexpected source-filtered results: %#v", page)
	}
}

func TestMonitorQualityTargetSourceIncludesUpstreamURL(t *testing.T) {
	srv := newQualityAPITestServer(t)

	targets, err := newMonitorQualityTargetSource(srv).ListTargets(context.Background(), quality.TargetQuery{Region: "all", IncludeUnavailable: true})
	if err != nil {
		t.Fatalf("ListTargets returned error: %v", err)
	}
	if len(targets) == 0 {
		t.Fatal("expected targets")
	}
	if targets[0].ProxyURL == targets[0].UpstreamURL {
		t.Fatalf("ProxyURL should remain local copy URL and UpstreamURL should preserve original upstream URI: %#v", targets[0])
	}
	if targets[0].UpstreamURL != "http://1.1.1.1:80" {
		t.Fatalf("unexpected upstream URL: %q", targets[0].UpstreamURL)
	}
	if !strings.Contains(targets[0].ProxyURL, "127.0.0.1:13001") {
		t.Fatalf("unexpected local proxy URL: %q", targets[0].ProxyURL)
	}
}

func TestMonitorQualityRunnerPrefersHTTPCompatibleUpstreamURL(t *testing.T) {
	target := quality.Target{ProxyURL: "http://127.0.0.1:13001", UpstreamURL: "http://203.0.113.10:8080"}
	if got := qualityCheckProxyURL(target); got != target.UpstreamURL {
		t.Fatalf("quality checks should prefer compatible upstream URL, got %q", got)
	}
}

func TestMonitorQualityRunnerFallsBackToLocalProxyForUnsupportedUpstreamScheme(t *testing.T) {
	target := quality.Target{ProxyURL: "http://127.0.0.1:13001", UpstreamURL: "vmess://example"}
	if got := qualityCheckProxyURL(target); got != target.ProxyURL {
		t.Fatalf("quality checks should fall back to local proxy URL for unsupported upstream schemes, got %q", got)
	}
}
