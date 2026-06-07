package monitor

import (
	"encoding/json"
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

func TestHandleNodesSummaryOnlyReturnsStatsWithoutRows(t *testing.T) {
	server := newTestNodesServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes?summary_only=true", nil)
	rr := httptest.NewRecorder()

	server.handleNodes(rr, req)

	var payload struct {
		Nodes         []Snapshot    `json:"nodes"`
		TotalNodes    int           `json:"total_nodes"`
		TotalFiltered int           `json:"total_filtered"`
		VisibleNodes  int           `json:"visible_nodes"`
		Available     int           `json:"available"`
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
