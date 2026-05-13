package reputation

import "strings"

var hostingKeywords = []string{
	"hosting", "host", "cloud", "datacenter", "data center", "server", "vps",
	"amazon", "aws", "google", "microsoft", "azure", "oracle", "digitalocean",
	"linode", "akamai", "ovh", "hetzner", "contabo", "leaseweb", "choopa",
}

// Score applies deterministic local heuristics on normalized provider data.
func Score(r *Result, expectedCountryCode string, failed bool, latencyMS int64) {
	if r == nil {
		return
	}
	score := 0
	text := strings.ToLower(strings.Join([]string{r.ASN, r.ISP, r.Org}, " "))
	for _, kw := range hostingKeywords {
		if strings.Contains(text, kw) {
			r.Hosting = true
			break
		}
	}
	if r.Hosting {
		score += 30
	}
	if r.Proxy {
		score += 40
	}
	if r.VPN {
		score += 40
	}
	if r.Tor {
		score += 80
	}
	if expectedCountryCode != "" && r.CountryCode != "" && !strings.EqualFold(expectedCountryCode, r.CountryCode) {
		score += 20
	}
	if failed {
		score += 50
	}
	if latencyMS > 3000 {
		score += 10
	}
	if score > 100 {
		score = 100
	}
	r.RiskScore = score
	r.RiskLevel = RiskLevel(score)
}
