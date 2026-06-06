package nodesource

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFilterNodesKeepsHTTPBasicCandidates(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/generate_204" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	cfg := FilterConfig{Enabled: true, MinTier: "http_basic", Workers: 4, Timeout: 2 * time.Second, Probes: FilterProbes{HTTP: "/generate_204"}}
	result := FilterNodes([]Node{{URI: good.URL}, {URI: bad.URL}}, cfg)
	if len(result.Accepted) != 1 || result.Accepted[0].URI != good.URL {
		t.Fatalf("unexpected accepted nodes: %#v", result.Accepted)
	}
	if result.Summary.Total != 2 || result.Summary.Accepted != 1 || result.Summary.Rejected != 1 {
		t.Fatalf("unexpected summary: %#v", result.Summary)
	}
}

func TestFilterNodesRequiresHTTPSForSimpleWeb(t *testing.T) {
	full := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generate_204":
			w.WriteHeader(http.StatusNoContent)
		case "/https":
			_, _ = w.Write([]byte("Example Domain"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer full.Close()
	httpOnly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/generate_204":
			w.WriteHeader(http.StatusNoContent)
		case "/https":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer httpOnly.Close()

	cfg := FilterConfig{Enabled: true, MinTier: "simple_web", Workers: 2, Timeout: 2 * time.Second, Probes: FilterProbes{HTTP: "/generate_204", HTTPS: "/https"}}
	result := FilterNodes([]Node{{URI: httpOnly.URL}, {URI: full.URL}}, cfg)
	if len(result.Accepted) != 1 || result.Accepted[0].URI != full.URL {
		t.Fatalf("unexpected accepted nodes: %#v", result.Accepted)
	}
	if result.Summary.TierCounts["simple_web"] != 1 || result.Summary.TierCounts["reject"] != 1 {
		t.Fatalf("unexpected tier counts: %#v", result.Summary.TierCounts)
	}
}
