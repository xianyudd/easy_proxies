package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleNodesLegacyAndUnknownQueryUseFilteredNodes(t *testing.T) {
	server := newTestNodesServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes?_=cachebuster", nil)
	rr := httptest.NewRecorder()

	server.handleNodes(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var payload struct {
		Nodes []Snapshot `json:"nodes"`
		Total int        `json:"total_nodes"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 3 {
		t.Fatalf("expected total 3, got %d", payload.Total)
	}
	if len(payload.Nodes) != 2 || payload.Nodes[0].Tag != "sub-a" || payload.Nodes[1].Tag != "free-c" {
		t.Fatalf("legacy response should include healthy and unchecked nodes, got %#v", payload.Nodes)
	}
}

func TestHandleNodesPagedFiltersSourceAndAvailability(t *testing.T) {
	server := newTestNodesServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes?page=1&page_size=1&source=free_proxy&availability=unavailable", nil)
	rr := httptest.NewRecorder()

	server.handleNodes(rr, req)

	var payload struct {
		Nodes         []Snapshot     `json:"nodes"`
		TotalNodes    int            `json:"total_nodes"`
		TotalFiltered int            `json:"total_filtered"`
		Page          int            `json:"page"`
		PageSize      int            `json:"page_size"`
		HasNext       bool           `json:"has_next"`
		SourceStats   map[string]int `json:"source_stats"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TotalNodes != 3 || payload.TotalFiltered != 1 || payload.Page != 1 || payload.PageSize != 1 {
		t.Fatalf("bad metadata: %#v", payload)
	}
	if len(payload.Nodes) != 1 || payload.Nodes[0].Tag != "free-b" {
		t.Fatalf("expected unavailable free node, got %#v", payload.Nodes)
	}
	if payload.SourceStats["subscription"] != 1 || payload.SourceStats["free_proxy"] != 2 {
		t.Fatalf("bad source stats: %#v", payload.SourceStats)
	}
}

func TestHandleNodesPagedClampsOutOfRangePageAndReportsTotalPages(t *testing.T) {
	server := newTestNodesServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes?page=99&page_size=1&availability=all", nil)
	rr := httptest.NewRecorder()

	server.handleNodes(rr, req)

	var payload struct {
		Nodes         []Snapshot `json:"nodes"`
		TotalFiltered int        `json:"total_filtered"`
		Page          int        `json:"page"`
		PageSize      int        `json:"page_size"`
		TotalPages    int        `json:"total_pages"`
		HasNext       bool       `json:"has_next"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TotalFiltered != 3 || payload.TotalPages != 3 {
		t.Fatalf("bad totals: %#v", payload)
	}
	if payload.Page != 3 || payload.PageSize != 1 || payload.HasNext {
		t.Fatalf("out-of-range page should clamp to the last page: %#v", payload)
	}
	if len(payload.Nodes) != 1 || payload.Nodes[0].Tag != "free-c" {
		t.Fatalf("expected last page row, got %#v", payload.Nodes)
	}
}

func TestHandleNodesClampsLargePageSize(t *testing.T) {
	server := newTestNodesServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes?page=1&page_size=999999&availability=all", nil)
	rr := httptest.NewRecorder()

	server.handleNodes(rr, req)

	var payload struct {
		PageSize int `json:"page_size"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PageSize != 500 {
		t.Fatalf("page_size=%d, want clamp to 500", payload.PageSize)
	}
}

func TestHandleNodesSummaryOnlyReturnsStatsWithoutRows(t *testing.T) {
	server := newTestNodesServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes?summary_only=true", nil)
	rr := httptest.NewRecorder()

	server.handleNodes(rr, req)

	var payload struct {
		Nodes         []Snapshot     `json:"nodes"`
		TotalNodes    int            `json:"total_nodes"`
		TotalFiltered int            `json:"total_filtered"`
		VisibleNodes  int            `json:"visible_nodes"`
		Available     int            `json:"available"`
		PortRange     map[string]int `json:"port_range"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TotalNodes != 3 || payload.TotalFiltered != 3 || len(payload.Nodes) != 0 {
		t.Fatalf("bad summary response: %#v", payload)
	}
	if payload.VisibleNodes != 2 || payload.Available != 1 {
		t.Fatalf("summary should include visible/available counts, got %#v", payload)
	}
	if payload.PortRange == nil || payload.PortRange["first"] != 13001 || payload.PortRange["last"] != 13003 {
		t.Fatalf("summary should include full port range, got %#v", payload.PortRange)
	}
}

func TestHandleDebugSummaryOnlyOmitsNodeDetails(t *testing.T) {
	server := newTestNodesServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug?summary_only=true", nil)
	rr := httptest.NewRecorder()

	server.handleDebug(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Nodes        []map[string]any `json:"nodes"`
		NodeCount    int              `json:"node_count"`
		TotalCalls   int64            `json:"total_calls"`
		TotalSuccess int64            `json:"total_success"`
		SuccessRate  float64          `json:"success_rate"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Nodes) != 0 {
		t.Fatalf("summary response should omit node details, got %d nodes", len(payload.Nodes))
	}
	if payload.NodeCount != 3 || payload.TotalCalls == 0 || payload.TotalSuccess == 0 || payload.SuccessRate <= 0 {
		t.Fatalf("summary metrics missing: %#v", payload)
	}
}

func TestManualProbeFailureMarksNodeUnavailable(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	h := mgr.Register(NodeInfo{Tag: "bad", Name: "Bad", URI: "http://127.0.0.1:1", Region: "us", Source: "free_proxy", Port: 13010})
	h.MarkInitialCheckDone(true)
	h.SetProbe(func(ctx context.Context) (time.Duration, error) {
		return 0, errors.New("probe failed")
	})
	server := &Server{mgr: mgr}

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/bad/probe", nil)
	rr := httptest.NewRecorder()
	server.handleNodeAction(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	assertNodeActionErrorCode(t, rr, "probe_failed")
	snap := mgr.Snapshot()[0]
	if snap.Available || !snap.InitialCheckDone || snap.FailureCount != 1 || snap.LastError != "probe failed" {
		t.Fatalf("probe failure should mark unavailable and record error, got %#v", snap)
	}
}

func TestNodeActionMissingNodeReturnsStructuredErrors(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{mgr: mgr}

	for _, tc := range []struct {
		name   string
		path   string
		status int
		code   string
	}{
		{name: "probe", path: "/api/nodes/missing/probe", status: http.StatusBadGateway, code: "probe_failed"},
		{name: "release", path: "/api/nodes/missing/release", status: http.StatusNotFound, code: "node_not_found"},
		{name: "blacklist", path: "/api/nodes/missing/blacklist", status: http.StatusNotFound, code: "node_not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			rr := httptest.NewRecorder()
			server.handleNodeAction(rr, req)
			if rr.Code != tc.status {
				t.Fatalf("status=%d, want %d body=%s", rr.Code, tc.status, rr.Body.String())
			}
			assertNodeActionErrorCode(t, rr, tc.code)
		})
	}
}

func TestManualProbeSuccessMarksNodeAvailable(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	h := mgr.Register(NodeInfo{Tag: "good", Name: "Good", URI: "http://127.0.0.1:1", Region: "us", Source: "free_proxy", Port: 13011})
	h.MarkInitialCheckDone(false)
	h.SetProbe(func(ctx context.Context) (time.Duration, error) {
		return 25 * time.Millisecond, nil
	})
	server := &Server{mgr: mgr}

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/good/probe", nil)
	rr := httptest.NewRecorder()
	server.handleNodeAction(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	snap := mgr.Snapshot()[0]
	if !snap.Available || !snap.InitialCheckDone || snap.SuccessCount != 1 || snap.LastLatencyMs != 25 {
		t.Fatalf("probe success should mark available and record latency, got %#v", snap)
	}
}

func newTestNodesServer(t *testing.T) *Server {
	t.Helper()
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	sub := mgr.Register(NodeInfo{Tag: "sub-a", Name: "Sub A", URI: "socks5://1.1.1.1:80", Region: "us", Source: "subscription", Port: 13001})
	sub.RecordSuccessWithLatency(100 * time.Millisecond)
	sub.MarkInitialCheckDone(true)

	freeUnavailable := mgr.Register(NodeInfo{Tag: "free-b", Name: "Free B", URI: "http://2.2.2.2:80", Region: "jp", Source: "free_proxy", Port: 13002})
	freeUnavailable.MarkInitialCheckDone(false)

	freeUnchecked := mgr.Register(NodeInfo{Tag: "free-c", Name: "Free C", URI: "http://3.3.3.3:80", Region: "jp", Source: "free_proxy", Port: 13003})
	freeUnchecked.MarkAvailable(false)

	return &Server{mgr: mgr}
}

func assertNodeActionErrorCode(t *testing.T, rr *httptest.ResponseRecorder, code string) {
	t.Helper()
	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error == "" || body.Code != code {
		t.Fatalf("unexpected body: %#v raw=%s", body, rr.Body.String())
	}
}
