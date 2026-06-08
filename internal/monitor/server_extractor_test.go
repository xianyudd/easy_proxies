package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"easy_proxies/internal/config"
)

func TestExtractorSnapshotMatchesRegionExtendedAliases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		snap   Snapshot
		region string
		want   bool
	}{
		{
			name:   "switzerland by exact region",
			snap:   Snapshot{NodeInfo: NodeInfo{Region: "ch", Name: "瑞士苏黎世", Country: "Switzerland"}},
			region: "ch",
			want:   true,
		},
		{
			name:   "india by alias fallback",
			snap:   Snapshot{NodeInfo: NodeInfo{Name: "印度孟买", Country: "India"}},
			region: "in",
			want:   true,
		},
		{
			name:   "germany by name alias",
			snap:   Snapshot{NodeInfo: NodeInfo{Name: "德国DE-HY2"}},
			region: "de",
			want:   true,
		},
		{
			name:   "uk by name alias",
			snap:   Snapshot{NodeInfo: NodeInfo{Name: "英国-优化2"}},
			region: "gb",
			want:   true,
		},
		{
			name:   "canada excluded from other",
			snap:   Snapshot{NodeInfo: NodeInfo{Name: "加拿大-优化"}},
			region: "other",
			want:   false,
		},
		{
			name:   "other excludes extended regions",
			snap:   Snapshot{NodeInfo: NodeInfo{Region: "ae", Name: "迪拜"}},
			region: "other",
			want:   false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := extractorSnapshotMatchesRegion(tc.snap, tc.region); got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestHandleExtractorRejectsInvalidCount(t *testing.T) {
	mgr, err := NewManager(Config{Enabled: true, Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	node := mgr.Register(NodeInfo{Tag: "node-a", Name: "Node A", URI: "http://1.1.1.1:80", ListenAddress: "127.0.0.1", Port: 31001, Region: "us"})
	node.MarkInitialCheckDone(true)
	srv := &Server{
		mgr: mgr,
		cfg: Config{ProxyUsername: "user", ProxyPassword: "pass"},
		cfgSrc: &config.Config{
			Mode: "multi-port",
		},
	}

	for _, rawCount := range []string{"0", "-1", "abc"} {
		req := httptest.NewRequest(http.MethodGet, "/api/extractor?region=all&mode=multi-port&format=http_url&count="+rawCount, nil)
		rec := httptest.NewRecorder()
		srv.handleExtractor(rec, req)
		assertExtractorErrorCode(t, rec, http.StatusBadRequest, "invalid_count")
	}
}

func TestHandleExtractorRejectsInvalidReveal(t *testing.T) {
	srv := &Server{}
	for _, raw := range []string{"maybe", "2", "yes"} {
		t.Run(raw, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/extractor?region=all&mode=pool&format=http_url&reveal="+raw, nil)
			rec := httptest.NewRecorder()

			srv.handleExtractor(rec, req)

			assertExtractorErrorCode(t, rec, http.StatusBadRequest, "invalid_bool")
		})
	}
}

func TestHandleExtractorReturnsStructuredErrorCodes(t *testing.T) {
	srv := &Server{}
	cases := []struct {
		name string
		path string
		code string
	}{
		{name: "region", path: "/api/extractor?region=moon&mode=pool&format=http_url", code: "invalid_region"},
		{name: "mode", path: "/api/extractor?region=all&mode=bad&format=http_url", code: "invalid_mode"},
		{name: "format", path: "/api/extractor?region=all&mode=pool&format=bad", code: "invalid_format"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			srv.handleExtractor(rec, req)
			assertExtractorErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func assertExtractorErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
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
