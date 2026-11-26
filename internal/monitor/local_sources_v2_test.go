package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"easy_proxies/internal/config"
)

type localTestAPIManager struct {
	fakeNodeManager
	state   config.LocalSourcesState
	saveErr error
}

func (m *localTestAPIManager) LocalSources(context.Context) (config.LocalSourcesState, error) {
	return m.state, nil
}
func (m *localTestAPIManager) SaveLocalSource(_ context.Context, _ string, _ int, _ string, _ bool, content string) (config.LocalSourcesState, error) {
	if m.saveErr != nil {
		return m.state, m.saveErr
	}
	_, err := config.ParseLocalSourceNodes(content)
	return m.state, err
}
func (m *localTestAPIManager) DeleteLocalSource(context.Context, string, int) (config.LocalSourcesState, error) {
	return m.state, nil
}
func (m *localTestAPIManager) ApplyLocalSources(context.Context, int) error { return nil }

func TestLocalSaveReturnsSafeOriginalLine(t *testing.T) {
	s := newTestNodesServer(t)
	m := &localTestAPIManager{}
	s.nodeMgr = m
	for _, method := range []string{"POST", "PUT"} {
		for _, content := range []string{"# comment\n\nhttp://private-user:private-secret@127.0.0.1:0", "# comment\nhttp://127.0.0.1:1\nprivate-secret://invalid"} {
			body, _ := json.Marshal(map[string]any{"expected_revision": 0, "name": "test", "content": string(rune(0xfeff)) + content})
			suffix := ""
			if method == "PUT" {
				suffix = "/existing"
			}
			w := httptest.NewRecorder()
			s.handleLocalSources(w, httptest.NewRequest(method, "/api/local-sources"+suffix, strings.NewReader(string(body))))
			var got struct {
				Line   int    `json:"line"`
				Reason string `json:"reason"`
				Error  string `json:"error"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &got)
			if w.Code != 400 || got.Line != 3 || got.Reason == "" {
				t.Fatalf("%s missing safe line: %s", method, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private-") || strings.Contains(w.Body.String(), "127.0.0.1") {
				t.Fatal("error leaked URI")
			}
		}
	}
	m.saveErr = errors.New("internal private-secret /private/path")
	w := httptest.NewRecorder()
	s.handleLocalSources(w, httptest.NewRequest("POST", "/api/local-sources", strings.NewReader(`{"expected_revision":0,"content":"http://127.0.0.1:1"}`)))
	if strings.Contains(w.Body.String(), "private") {
		t.Fatal("internal exception leaked")
	}
}

func TestLocalMemberHealthIncludesEverySavedMember(t *testing.T) {
	s := newTestNodesServer(t)
	source := config.LocalSource{ID: "a", Name: "Europe", Enabled: true}
	for i := 1; i <= 4; i++ {
		source.Nodes = append(source.Nodes, config.LocalSourceNode{URI: "http://private-user:private-secret@127.0.0.1:" + string(rune('0'+i))})
	}
	m := &localTestAPIManager{state: config.LocalSourcesState{Pending: true, Sources: []config.LocalSource{source}}}
	s.nodeMgr = m
	for i := 0; i < 3; i++ {
		h := s.mgr.Register(NodeInfo{Tag: []string{"good", "bad", "untested"}[i], URI: source.Nodes[i].URI, Source: "subscription"})
		if i < 2 {
			h.MarkInitialCheckDone(i == 0)
		}
	}
	w := httptest.NewRecorder()
	s.handleLocalSources(w, httptest.NewRequest("GET", "/api/local-sources/a", nil))
	var got struct {
		Health []struct {
			Status string `json:"status"`
			Tag    string `json:"tag"`
		} `json:"member_health"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Health) != 4 {
		t.Fatalf("lost health members: %s", w.Body.String())
	}
	for i, want := range []string{"available", "unavailable", "untested", "unapplied"} {
		if got.Health[i].Status != want {
			t.Fatalf("member %d: %q want %s", i, got.Health[i].Status, want)
		}
	}
	// Disabled, absent runtime is not a failed health check.
	m.state.Pending = false
	m.state.Sources[0].Enabled = false
	w = httptest.NewRecorder()
	s.handleLocalSources(w, httptest.NewRequest("GET", "/api/local-sources/a", nil))
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Health[3].Status != "not_running" || got.Health[3].Tag != "" {
		t.Fatal("disabled member misreported as failure")
	}
	if got.Health[0].Status != "available" {
		t.Fatal("shared running member hidden when source disabled")
	}
}
