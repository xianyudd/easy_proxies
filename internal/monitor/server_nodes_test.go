package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easy_proxies/internal/config"
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

func TestHandleNodesPagedSearchAliasesFilterRows(t *testing.T) {
	server := newTestNodesServer(t)
	for _, tc := range []struct {
		name    string
		path    string
		wantTag string
	}{
		{name: "q", path: "/api/nodes?page=1&page_size=10&availability=all&q=Sub%20A", wantTag: "sub-a"},
		{name: "search", path: "/api/nodes?page=1&page_size=10&availability=all&search=2.2.2.2", wantTag: "free-b"},
		{name: "keyword", path: "/api/nodes?page=1&page_size=10&availability=all&keyword=Free%20C", wantTag: "free-c"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rr := httptest.NewRecorder()

			server.handleNodes(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200 body=%s", rr.Code, rr.Body.String())
			}
			var payload struct {
				Nodes         []Snapshot `json:"nodes"`
				TotalFiltered int        `json:"total_filtered"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.TotalFiltered != 1 || len(payload.Nodes) != 1 || payload.Nodes[0].Tag != tc.wantTag {
				t.Fatalf("expected only %s, got %#v", tc.wantTag, payload)
			}
		})
	}
}

func TestHandleNodesPagedAvailableBoolAliasFiltersRows(t *testing.T) {
	server := newTestNodesServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes?page=1&page_size=10&source=free_proxy&available=true", nil)
	rr := httptest.NewRecorder()

	server.handleNodes(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Nodes         []Snapshot `json:"nodes"`
		TotalFiltered int        `json:"total_filtered"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TotalFiltered != 0 || len(payload.Nodes) != 0 {
		t.Fatalf("available=true should filter to healthy free_proxy rows, got %#v", payload)
	}
}

func TestHandleNodesPagedRegionOtherIncludesBlankAndOtherOnly(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(NodeInfo{Tag: "blank-region", Name: "Blank Region", URI: "http://1.1.1.1:80", Region: "", Source: "free_proxy", Port: 13001}).MarkInitialCheckDone(true)
	mgr.Register(NodeInfo{Tag: "other-region", Name: "Other Region", URI: "http://2.2.2.2:80", Region: "other", Source: "free_proxy", Port: 13002}).MarkInitialCheckDone(true)
	mgr.Register(NodeInfo{Tag: "us-region", Name: "US Region", URI: "http://3.3.3.3:80", Region: "us", Source: "free_proxy", Port: 13003}).MarkInitialCheckDone(true)
	server := &Server{mgr: mgr}

	req := httptest.NewRequest(http.MethodGet, "/api/nodes?page=1&page_size=10&region=other&availability=all", nil)
	rr := httptest.NewRecorder()
	server.handleNodes(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Nodes         []Snapshot     `json:"nodes"`
		TotalFiltered int            `json:"total_filtered"`
		RegionStats   map[string]int `json:"region_stats"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TotalFiltered != 2 || len(payload.Nodes) != 2 {
		t.Fatalf("region=other should expose only blank/other queue rows, got %#v", payload)
	}
	gotTags := map[string]bool{}
	for _, node := range payload.Nodes {
		gotTags[node.Tag] = true
		if node.Tag == "us-region" {
			t.Fatalf("region=other must not include concrete region rows: %#v", payload.Nodes)
		}
	}
	if !gotTags["blank-region"] || !gotTags["other-region"] {
		t.Fatalf("region=other missing blank/other rows: %#v", payload.Nodes)
	}
	if payload.RegionStats["other"] != 2 || payload.RegionStats["us"] != 1 {
		t.Fatalf("summary stats should count blank region as other and keep concrete regions: %#v", payload.RegionStats)
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

func TestHandleNodesZeroResultHugePageDoesNotPanic(t *testing.T) {
	server := newTestNodesServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes?page=9223372036854775807&page_size=500&region=ca", nil)
	rr := httptest.NewRecorder()

	server.handleNodes(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 body=%s", rr.Code, rr.Body.String())
	}
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
	if payload.TotalFiltered != 0 || len(payload.Nodes) != 0 || payload.Page != 1 || payload.TotalPages != 0 || payload.HasNext {
		t.Fatalf("zero-result huge page should return empty first page without next page: %#v", payload)
	}
	if payload.PageSize != 500 {
		t.Fatalf("page_size=%d, want 500", payload.PageSize)
	}
}

func TestHandleNodesRejectsInvalidPagination(t *testing.T) {
	server := newTestNodesServer(t)
	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "bad page", path: "/api/nodes?page=abc&page_size=100"},
		{name: "zero page", path: "/api/nodes?page=0&page_size=100"},
		{name: "negative page", path: "/api/nodes?page=-1&page_size=100"},
		{name: "bad page size", path: "/api/nodes?page=1&page_size=abc"},
		{name: "zero page size", path: "/api/nodes?page=1&page_size=0"},
		{name: "negative page size", path: "/api/nodes?page=1&page_size=-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rr := httptest.NewRecorder()

			server.handleNodes(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400 body=%s", rr.Code, rr.Body.String())
			}
			assertNodeActionErrorCode(t, rr, "invalid_pagination")
		})
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

func TestHandleNodesAcceptsFrontendDefaultLatencySort(t *testing.T) {
	server := newTestNodesServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes?page=1&page_size=100&region=all&source=all&availability=available&latency=all&sort=latency", nil)
	rr := httptest.NewRecorder()

	server.handleNodes(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("frontend default nodes query should be accepted, status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Nodes         []Snapshot `json:"nodes"`
		TotalFiltered int        `json:"total_filtered"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TotalFiltered != 1 || len(payload.Nodes) != 1 || payload.Nodes[0].Tag != "sub-a" {
		t.Fatalf("expected available node sorted by latency, got %#v", payload)
	}
}

func TestSortNodeSnapshotsDefaultUsesStablePortOrder(t *testing.T) {
	nodes := []Snapshot{
		{NodeInfo: NodeInfo{Tag: "slow", Name: "Slow", Port: 13003}, LastLatencyMs: 1},
		{NodeInfo: NodeInfo{Tag: "fast", Name: "Fast", Port: 13001}, LastLatencyMs: 999},
		{NodeInfo: NodeInfo{Tag: "middle", Name: "Middle", Port: 13002}, LastLatencyMs: 50},
	}

	sortNodeSnapshots(nodes, "")

	got := []string{nodes[0].Tag, nodes[1].Tag, nodes[2].Tag}
	want := []string{"fast", "middle", "slow"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("default sort tags=%v, want stable port order %v", got, want)
	}
}

func TestSortNodeSnapshotsLatencySortRemainsExplicit(t *testing.T) {
	nodes := []Snapshot{
		{NodeInfo: NodeInfo{Tag: "slow", Name: "Slow", Port: 13003}, LastLatencyMs: 999},
		{NodeInfo: NodeInfo{Tag: "fast", Name: "Fast", Port: 13001}, LastLatencyMs: 1},
		{NodeInfo: NodeInfo{Tag: "middle", Name: "Middle", Port: 13002}, LastLatencyMs: 50},
	}

	sortNodeSnapshots(nodes, "latency")

	got := []string{nodes[0].Tag, nodes[1].Tag, nodes[2].Tag}
	want := []string{"fast", "middle", "slow"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("latency sort tags=%v, want %v", got, want)
	}
}

func TestHandleNodesRejectsInvalidSummaryOnly(t *testing.T) {
	server := newTestNodesServer(t)
	for _, raw := range []string{"maybe", "2", "yes"} {
		t.Run(raw, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/nodes?summary_only="+raw, nil)
			rr := httptest.NewRecorder()

			server.handleNodes(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400 body=%s", rr.Code, rr.Body.String())
			}
			assertNodeActionErrorCode(t, rr, "invalid_bool")
		})
	}
}

func TestHandleNodesRejectsInvalidFiltersAndSort(t *testing.T) {
	server := newTestNodesServer(t)
	for _, tc := range []struct {
		name string
		path string
		code string
	}{
		{name: "region", path: "/api/nodes?page=1&region=moon", code: "invalid_region"},
		{name: "source", path: "/api/nodes?page=1&source=bad", code: "invalid_source"},
		{name: "availability", path: "/api/nodes?page=1&availability=bad", code: "invalid_availability"},
		{name: "latency", path: "/api/nodes?page=1&latency=bad", code: "invalid_latency"},
		{name: "sort", path: "/api/nodes?page=1&sort=bad", code: "invalid_sort"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rr := httptest.NewRecorder()

			server.handleNodes(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400 body=%s", rr.Code, rr.Body.String())
			}
			assertNodeActionErrorCode(t, rr, tc.code)
		})
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

func TestHandleDebugRejectsInvalidSummaryOnly(t *testing.T) {
	server := newTestNodesServer(t)
	for _, raw := range []string{"maybe", "2", "yes"} {
		t.Run(raw, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/debug?summary_only="+raw, nil)
			rr := httptest.NewRecorder()

			server.handleDebug(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400 body=%s", rr.Code, rr.Body.String())
			}
			assertNodeActionErrorCode(t, rr, "invalid_bool")
		})
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

func TestReadOnlyNodeHandlersRejectMethodsWithStructuredCode(t *testing.T) {
	server := newTestNodesServer(t)
	for _, tc := range []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{name: "nodes", path: "/api/nodes", handler: server.handleNodes},
		{name: "debug", path: "/api/debug", handler: server.handleDebug},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			rr := httptest.NewRecorder()

			tc.handler(rr, req)

			if rr.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status=%d, want 405 body=%s", rr.Code, rr.Body.String())
			}
			assertNodeActionErrorCode(t, rr, "method_not_allowed")
		})
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

func TestNodeActionRouteErrorsAreStructured(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{mgr: mgr}

	for _, tc := range []struct {
		name   string
		method string
		path   string
		status int
		code   string
	}{
		{name: "unknown action", method: http.MethodPost, path: "/api/nodes/node-a/unknown", status: http.StatusNotFound, code: "unknown_node_action"},
		{name: "wrong method", method: http.MethodGet, path: "/api/nodes/node-a/release", status: http.StatusMethodNotAllowed, code: "method_not_allowed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()
			server.handleNodeAction(rr, req)
			if rr.Code != tc.status {
				t.Fatalf("status=%d, want %d body=%s", rr.Code, tc.status, rr.Body.String())
			}
			assertNodeActionErrorCode(t, rr, tc.code)
		})
	}
}

func TestBlacklistRejectsInvalidBodyWithoutMutatingNode(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	h := mgr.Register(NodeInfo{Tag: "node-a", Name: "Node A", URI: "http://127.0.0.1:1", Region: "us", Source: "free_proxy", Port: 13020})
	h.MarkInitialCheckDone(true)
	h.MarkAvailable(true)
	server := &Server{mgr: mgr}

	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{name: "bad json", body: `{`, code: "invalid_request"},
		{name: "bad duration", body: `{"duration":"bad-duration"}`, code: "invalid_blacklist_duration"},
		{name: "zero duration", body: `{"duration":"0s"}`, code: "invalid_blacklist_duration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/nodes/node-a/blacklist", strings.NewReader(tc.body))
			rr := httptest.NewRecorder()

			server.handleNodeAction(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400 body=%s", rr.Code, rr.Body.String())
			}
			assertNodeActionErrorCode(t, rr, tc.code)
			snap := mgr.Snapshot()[0]
			if snap.Blacklisted {
				t.Fatalf("invalid blacklist request should not mutate node, got %#v", snap)
			}
		})
	}
}

func TestNodeActionBlacklistRejectsTrailingJSONWithoutMutatingNode(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	h := mgr.Register(NodeInfo{Tag: "node-a", Name: "Node A", URI: "http://127.0.0.1:1", Region: "us", Source: "free_proxy", Port: 13022})
	h.MarkInitialCheckDone(true)
	h.MarkAvailable(true)
	server := &Server{mgr: mgr}

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/node-a/blacklist", strings.NewReader(`{"duration":"1h"}{"extra":true}`))
	rr := httptest.NewRecorder()

	server.handleNodeAction(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 body=%s", rr.Code, rr.Body.String())
	}
	assertNodeActionErrorCode(t, rr, "invalid_request")
	snap := mgr.Snapshot()[0]
	if snap.Blacklisted {
		t.Fatalf("trailing JSON blacklist request should not mutate node, got %#v", snap)
	}
}

func TestBlacklistEmptyBodyUsesDefaultDuration(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	h := mgr.Register(NodeInfo{Tag: "node-a", Name: "Node A", URI: "http://127.0.0.1:1", Region: "us", Source: "free_proxy", Port: 13021})
	h.MarkInitialCheckDone(true)
	h.MarkAvailable(true)
	server := &Server{mgr: mgr}

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/node-a/blacklist", nil)
	rr := httptest.NewRecorder()

	server.handleNodeAction(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	snap := mgr.Snapshot()[0]
	if !snap.Blacklisted {
		t.Fatalf("empty blacklist body should use default duration and blacklist node, got %#v", snap)
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

func TestNodeActionRegionConfirmsRuntimeRegionAndPersistsOverride(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: manual
    uri: http://4.4.4.4:80#Manual
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(NodeInfo{Tag: "manual-node", Name: "Manual", URI: "http://4.4.4.4:80#Manual", Region: "other", Country: "Unknown", Source: "free_proxy", Port: 13030})
	server := &Server{mgr: mgr, cfgSrc: cfg}

	req := httptest.NewRequest(http.MethodPost, "/api/nodes/manual-node/region", strings.NewReader(`{"region":"jp"}`))
	rr := httptest.NewRecorder()
	server.handleNodeAction(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		NeedReload bool     `json:"need_reload"`
		Node       Snapshot `json:"node"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.NeedReload || payload.Node.Region != "jp" || payload.Node.Country != "日本" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	snap, err := mgr.SnapshotFor("manual-node")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Region != "jp" || snap.Country != "日本" {
		t.Fatalf("runtime region not updated: %#v", snap)
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reloaded.RegionOverrideForURI("http://4.4.4.4:80#manual"); !ok || got != "jp" {
		t.Fatalf("persisted override = %q, %v; want jp,true", got, ok)
	}
}

func TestNodeActionRegionRejectsOtherAndInvalidRegionWithoutMutation(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	mgr.Register(NodeInfo{Tag: "manual-node", Name: "Manual", URI: "http://4.4.4.4:80", Region: "other", Country: "Unknown", Source: "free_proxy", Port: 13030})
	server := &Server{mgr: mgr}

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "other", body: `{"region":"other"}`},
		{name: "all", body: `{"region":"all"}`},
		{name: "invalid", body: `{"region":"moon"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/nodes/manual-node/region", strings.NewReader(tc.body))
			rr := httptest.NewRecorder()
			server.handleNodeAction(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want 400 body=%s", rr.Code, rr.Body.String())
			}
			assertNodeActionErrorCode(t, rr, "invalid_region")
			snap, err := mgr.SnapshotFor("manual-node")
			if err != nil {
				t.Fatal(err)
			}
			if snap.Region != "other" || snap.Country != "Unknown" {
				t.Fatalf("invalid region request should not mutate node, got %#v", snap)
			}
		})
	}
}
