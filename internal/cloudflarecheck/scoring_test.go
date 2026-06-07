package cloudflarecheck

import "testing"

func TestLevelBoundaries(t *testing.T) {
	cases := []struct {
		score int
		want  string
	}{
		{100, "excellent"}, {80, "excellent"}, {79, "good"}, {60, "good"}, {59, "fair"}, {40, "fair"}, {39, "poor"}, {0, "poor"},
	}
	for _, tc := range cases {
		if got := Level(tc.score); got != tc.want {
			t.Fatalf("Level(%d)=%s want %s", tc.score, got, tc.want)
		}
	}
}

func TestScoreFailedRule(t *testing.T) {
	score, level := Score(ScoreInput{ExpectedRegion: "jp", HTTP204OK: false, TraceOK: false, Error: "timeout"})
	if score != 0 || level != "failed" {
		t.Fatalf("expected failed 0, got %d %s", score, level)
	}
}

func TestScorePenalties(t *testing.T) {
	score, level := Score(ScoreInput{ExpectedRegion: "jp", CFLoc: "US", HTTP204OK: false, TraceOK: false, ChallengeStatus: "forbidden", LatencyMS: 4000})
	if score != 0 || level != "poor" {
		t.Fatalf("expected poor 0 when multiple penalties apply, got %d %s", score, level)
	}
	score, level = Score(ScoreInput{ExpectedRegion: "jp", CFLoc: "JP", HTTP204OK: true, TraceOK: true, ChallengeStatus: "not_configured", LatencyMS: 300})
	if score != 100 || level != "excellent" {
		t.Fatalf("expected excellent 100, got %d %s", score, level)
	}
	score, level = Score(ScoreInput{ExpectedRegion: "jp", CFLoc: "US", HTTP204OK: true, TraceOK: true, ChallengeStatus: "managed_challenge", LatencyMS: 100})
	if score != 60 || level != "good" {
		t.Fatalf("expected good 60, got %d %s", score, level)
	}
}

func TestFinalizeResultClearsNonFatalProbeErrors(t *testing.T) {
	got := finalizeResultScore(Result{HTTP204OK: true, TraceOK: false, Error: "trace timeout"}, "all")
	if got.Level == "failed" || got.Error != "" {
		t.Fatalf("partial probe failure should be represented by booleans/score, not fatal error: %#v", got)
	}

	got = finalizeResultScore(Result{HTTP204OK: false, TraceOK: true, Error: "204 timeout"}, "all")
	if got.Level == "failed" || got.Error != "" {
		t.Fatalf("trace success should clear non-fatal 204 error: %#v", got)
	}
}

func TestFinalizeResultKeepsFatalProbeErrors(t *testing.T) {
	got := finalizeResultScore(Result{HTTP204OK: false, TraceOK: false, Error: "dial timeout"}, "all")
	if got.Level != "failed" || got.Error == "" || got.Score != 0 {
		t.Fatalf("fatal probe failure should keep error and failed level: %#v", got)
	}
}
