package monitor

import (
	"easy_proxies/internal/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSharedLocalOnlySourceStats(t *testing.T) {
	s := newTestNodesServer(t)
	_, _, before := nodeStats(s.mgr.Snapshot())
	s.mgr.Register(NodeInfo{Tag: "shared", Source: "subscription", LocalSources: []config.LocalSourceRef{{ID: "a"}, {ID: "b"}}})
	w := httptest.NewRecorder()
	s.handleNodes(w, httptest.NewRequest("GET", "/api/nodes?summary_only=true", nil))
	var got struct {
		SourceStats map[string]int `json:"source_stats"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SourceStats["subscription"] != before["subscription"]+1 || got.SourceStats["local_source"] != 1 {
		t.Fatalf("shared-only filter missing or overcounted: %v", got.SourceStats)
	}
	_, _, stats := nodeStats([]Snapshot{{NodeInfo: NodeInfo{Source: "local_source", LocalSources: []config.LocalSourceRef{{ID: "a"}, {ID: "b"}}}}})
	if stats["local_source"] != 1 {
		t.Fatal("local primary counted twice")
	}
}

func TestLocalSourceOverviewFilterAndSearch(t *testing.T) {
	s := newTestNodesServer(t)
	s.mgr.Register(NodeInfo{Tag: "local-only", Name: "node-A", Source: "local_source", LocalSources: []config.LocalSourceRef{{ID: "eu", Name: "Europe"}}})
	s.mgr.Register(NodeInfo{Tag: "shared", Name: "node-B", Source: "subscription", LocalSources: []config.LocalSourceRef{{ID: "eu", Name: "Europe"}}})
	for _, q := range []string{"source=local_source", "q=Europe"} {
		t.Run(q, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/nodes?page=1&page_size=100&availability=all&"+q, nil)
			w := httptest.NewRecorder()
			s.handleNodes(w, r)
			if w.Code != 200 {
				t.Fatalf("status=%d", w.Code)
			}
			var result struct {
				Nodes []Snapshot `json:"nodes"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Nodes) != 2 {
				t.Fatalf("want local and shared members, got %d", len(result.Nodes))
			}
		})
	}
}
