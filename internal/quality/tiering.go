package quality

import "strings"

type Tier string

const (
	TierReject      Tier = "reject"
	TierRescue      Tier = "rescue"
	TierHTTPOnly    Tier = "http_only"
	TierSimpleWeb   Tier = "simple_web"
	TierRecommended Tier = "recommended"
	TierPremium     Tier = "premium"
)

type TierDecision struct {
	Tier         Tier
	Pool         string
	Score        int
	Capabilities []string
	Reasons      []string
}

func ClassifyResult(result Result) TierDecision {
	reasons := make([]string, 0, 6)
	caps := make([]string, 0, 6)
	score := 0

	quickOK := quickMapOK(result.Quick)
	if quickOK {
		score += 10
		caps = append(caps, "socket_reachable")
		reasons = append(reasons, "quick_ok")
	} else {
		reasons = append(reasons, "quick_failed")
		return TierDecision{Tier: TierReject, Pool: "reject_pool", Score: 0, Reasons: reasons}
	}

	httpOK := result.Success && len(result.CF) > 0
	httpsOK := mapOK(result.CF, "trace_ok")
	highRisk := riskLevel(result.Reputation) == "high" || riskLevel(result.Reputation) == "failed"
	lowRisk := riskLevel(result.Reputation) == "low" || riskLevel(result.Reputation) == "medium"
	cfScore := intFromAny(result.CF, "score")

	if httpOK {
		caps = append(caps, "http_basic")
		score += 10
	}
	if httpsOK {
		caps = append(caps, "https_basic")
		score += 15
	}
	if cfScore > 0 {
		score += cfScore / 5
		if cfScore >= 60 {
			caps = append(caps, "https_cf")
			reasons = append(reasons, "cf_good")
		}
		if cfScore >= 80 {
			caps = append(caps, "strict_web")
		}
	}
	if lowRisk {
		caps = append(caps, "low_risk_exit")
		score += 15
		reasons = append(reasons, "risk_low")
	} else if highRisk {
		score -= 20
		reasons = append(reasons, "risk_high")
	}

	tier := TierReject
	pool := "reject_pool"
	switch {
	case quickOK && !httpOK:
		tier, pool = TierRescue, "rescue_pool"
		reasons = append(reasons, "rescue")
	case httpOK && !httpsOK:
		tier, pool = TierHTTPOnly, "http_pool"
		reasons = append(reasons, "http_only")
	case httpOK && httpsOK && lowRisk && cfScoreAtLeast(result, 80):
		tier, pool = TierPremium, "strict_pool"
		reasons = append(reasons, "premium")
	case httpOK && httpsOK && lowRisk && cfScoreAtLeast(result, 60):
		tier, pool = TierRecommended, "recommended_pool"
		reasons = append(reasons, "recommended")
	case httpOK && httpsOK:
		tier, pool = TierSimpleWeb, "web_pool"
		reasons = append(reasons, "simple_web")
	}

	return TierDecision{
		Tier:         tier,
		Pool:         pool,
		Score:        clampScore(score),
		Capabilities: uniqueStrings(caps),
		Reasons:      uniqueStrings(reasons),
	}
}

func riskLevel(m map[string]any) string {
	if m == nil {
		return ""
	}
	if s, ok := m["risk_level"].(string); ok {
		return strings.ToLower(strings.TrimSpace(s))
	}
	return ""
}

func intFromAny(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

func mapOK(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	b, _ := m[key].(bool)
	return b
}

func cfScoreAtLeast(result Result, threshold int) bool {
	return intFromAny(result.CF, "score") >= threshold
}

func clampScore(score int) int {
	switch {
	case score < 0:
		return 0
	case score > 100:
		return 100
	default:
		return score
	}
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
