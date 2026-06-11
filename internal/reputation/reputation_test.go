package reputation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type stubProvider struct {
	name  string
	calls atomic.Int32
	res   *Result
	err   error
}

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) Lookup(ctx context.Context, ip string) (*Result, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	cp := *s.res
	cp.IP = ip
	cp.Provider = s.name
	return &cp, nil
}

func TestCheckerLookupUsesCache(t *testing.T) {
	provider := &stubProvider{name: "stub", res: &Result{CountryCode: "JP", RiskScore: 10, RiskLevel: "low"}}
	checker := NewChecker(WithProviders(provider), WithCache(NewCache(time.Hour)))
	if _, err := checker.LookupIP(context.Background(), "1.1.1.1"); err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	got, err := checker.LookupIP(context.Background(), "1.1.1.1")
	if err != nil {
		t.Fatalf("cached lookup failed: %v", err)
	}
	if !got.Cached {
		t.Fatal("expected cached result")
	}
	if provider.calls.Load() != 1 {
		t.Fatalf("expected one provider call, got %d", provider.calls.Load())
	}
}

func TestCheckerLookupFallbackProvider(t *testing.T) {
	bad := &stubProvider{name: "bad", err: errors.New("boom")}
	good := &stubProvider{name: "good", res: &Result{CountryCode: "US"}}
	checker := NewChecker(WithProviders(bad, good), WithCache(NewCache(time.Hour)))
	got, err := checker.LookupIP(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if got.Provider != "good" {
		t.Fatalf("expected fallback provider, got %s", got.Provider)
	}
}

func TestCheckerRejectsInvalidIP(t *testing.T) {
	checker := NewChecker(WithProviders(&stubProvider{name: "stub", res: &Result{}}))
	if _, err := checker.LookupIP(context.Background(), "not-an-ip"); err == nil {
		t.Fatal("expected invalid ip error")
	}
}

func TestCheckerExitIPViaProxyFallsBackAcrossEndpoints(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/bad":
			http.Error(w, "bad endpoint", http.StatusBadGateway)
		case "/good":
			_, _ = w.Write([]byte(`{"ip":"203.0.113.10"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	checker := NewChecker(
		WithExitIPEndpoints(server.URL+"/bad", server.URL+"/good"),
		WithTimeout(time.Second),
	)
	ip, _, err := checker.ExitIPViaProxy(context.Background(), "")
	if err != nil {
		t.Fatalf("exit IP lookup should fall back to second endpoint: %v", err)
	}
	if ip != "203.0.113.10" {
		t.Fatalf("unexpected exit IP %q", ip)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected both endpoints to be attempted, got %d calls", calls.Load())
	}
}
