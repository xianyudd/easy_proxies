package reputation

import (
	"context"
	"errors"
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
