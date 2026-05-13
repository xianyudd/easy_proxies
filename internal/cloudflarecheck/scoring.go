package cloudflarecheck

import "strings"

type ScoreInput struct {
	ExpectedRegion  string
	CFLoc           string
	HTTP204OK       bool
	TraceOK         bool
	ChallengeStatus string
	LatencyMS       int64
	Error           string
}

func Score(input ScoreInput) (int, string) {
	if IsFailed(input.HTTP204OK, input.TraceOK, input.Error) {
		return 0, "failed"
	}
	score := 100
	if !input.HTTP204OK {
		score -= 30
	}
	if !input.TraceOK {
		score -= 20
	}
	expected := strings.ToUpper(strings.TrimSpace(input.ExpectedRegion))
	loc := strings.ToUpper(strings.TrimSpace(input.CFLoc))
	if expected != "" && expected != "ALL" && expected != "OTHER" && loc != "" && loc != expected {
		score -= 15
	}
	switch strings.ToLower(strings.TrimSpace(input.ChallengeStatus)) {
	case "forbidden":
		score -= 40
	case "managed_challenge", "challenge":
		score -= 25
	}
	if input.LatencyMS > 3000 {
		score -= 10
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score, Level(score)
}

func Level(score int) string {
	switch {
	case score >= 80:
		return "excellent"
	case score >= 60:
		return "good"
	case score >= 40:
		return "fair"
	default:
		return "poor"
	}
}

func IsFailed(http204OK, traceOK bool, err string) bool {
	return !http204OK && !traceOK && strings.TrimSpace(err) != ""
}
