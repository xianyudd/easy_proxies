package reputation

import "testing"

func TestScoreHostingKeyword(t *testing.T) {
	r := &Result{IP: "1.1.1.1", Org: "Example Cloud Hosting"}
	Score(r, "", false, 100)
	if !r.Hosting {
		t.Fatalf("expected hosting heuristic")
	}
	if r.RiskLevel != "low" || r.RiskScore != 30 {
		t.Fatalf("unexpected risk: %s %d", r.RiskLevel, r.RiskScore)
	}
}

func TestScoreMismatchCountryAndProxy(t *testing.T) {
	r := &Result{IP: "1.1.1.1", CountryCode: "US", Proxy: true}
	Score(r, "JP", false, 100)
	if r.RiskScore != 60 || r.RiskLevel != "medium" {
		t.Fatalf("unexpected risk: %s %d", r.RiskLevel, r.RiskScore)
	}
}
