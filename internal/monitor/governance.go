package monitor

import (
	"net/url"
	"strings"
	"time"
)

// GovernanceConfig controls subscription-node quarantine and probe skipping.
// When Enabled is false, only legacy zombie behavior (if any) may still apply.
type GovernanceConfig struct {
	Enabled                 bool          `yaml:"enabled" json:"enabled"`
	IsolateProtocols        []string      `yaml:"isolate_protocols" json:"isolate_protocols"`
	IsolateVlessReality     bool          `yaml:"isolate_vless_reality" json:"isolate_vless_reality"`
	HostQuarantine          bool          `yaml:"host_quarantine" json:"host_quarantine"`
	ZombieZeroSuccessFails  int           `yaml:"zombie_zero_success_failures" json:"zombie_zero_success_failures"`
	ZombieDuration          time.Duration `yaml:"zombie_duration" json:"zombie_duration"`
	FlakyMinSuccess         int           `yaml:"flaky_min_success" json:"flaky_min_success"`
	StructuralDuration      time.Duration `yaml:"structural_duration" json:"structural_duration"`
	// ProbeEveryN for isolated structural classes: 0/1 = probe every round; N>1 = every Nth round.
	IsolatedProbeEveryN int `yaml:"isolated_probe_every_n" json:"isolated_probe_every_n"`
}

// DefaultGovernance returns recommended G1 defaults (safe for vmess / vless-ws).
func DefaultGovernance() GovernanceConfig {
	return GovernanceConfig{
		Enabled:                true,
		IsolateProtocols:       []string{"hysteria2", "hy2", "anytls"},
		IsolateVlessReality:    true,
		HostQuarantine:         true,
		ZombieZeroSuccessFails: 10,
		ZombieDuration:         6 * time.Hour,
		FlakyMinSuccess:        5,
		StructuralDuration:     24 * time.Hour,
		IsolatedProbeEveryN:    6, // probe structural isolates ~1/6 as often
	}
}

// Normalize fills zeros with defaults when governance is enabled.
func (g *GovernanceConfig) Normalize() {
	if g == nil {
		return
	}
	def := DefaultGovernance()
	if len(g.IsolateProtocols) == 0 {
		g.IsolateProtocols = append([]string(nil), def.IsolateProtocols...)
	}
	if g.ZombieZeroSuccessFails <= 0 {
		g.ZombieZeroSuccessFails = def.ZombieZeroSuccessFails
	}
	if g.ZombieDuration <= 0 {
		g.ZombieDuration = def.ZombieDuration
	}
	if g.FlakyMinSuccess <= 0 {
		g.FlakyMinSuccess = def.FlakyMinSuccess
	}
	if g.StructuralDuration <= 0 {
		g.StructuralDuration = def.StructuralDuration
	}
	if g.IsolatedProbeEveryN < 0 {
		g.IsolatedProbeEveryN = 0
	}
}

// URISecurityType extracts VLESS-like security/type query params.
func URISecurityType(rawURI string) (security, transport string) {
	rawURI = strings.TrimSpace(rawURI)
	if rawURI == "" {
		return "", ""
	}
	u, err := url.Parse(rawURI)
	if err != nil {
		return "", ""
	}
	q := u.Query()
	return strings.ToLower(strings.TrimSpace(q.Get("security"))), strings.ToLower(strings.TrimSpace(q.Get("type")))
}

// URIHost returns host without port for quarantine keys.
func URIHost(rawURI string) string {
	rawURI = strings.TrimSpace(rawURI)
	if rawURI == "" {
		return ""
	}
	u, err := url.Parse(rawURI)
	if err != nil || u.Host == "" {
		// hysteria2 may have non-standard ports; best-effort
		if i := strings.Index(rawURI, "://"); i >= 0 {
			rest := rawURI[i+3:]
			if at := strings.LastIndex(rest, "@"); at >= 0 {
				rest = rest[at+1:]
			}
			hostport := rest
			if q := strings.IndexAny(hostport, "?#"); q >= 0 {
				hostport = hostport[:q]
			}
			if h, _, err := splitHostPortLoose(hostport); err == nil {
				return h
			}
			return hostport
		}
		return ""
	}
	host := u.Hostname()
	return host
}

func splitHostPortLoose(hostport string) (string, string, error) {
	// net.SplitHostPort needs brackets for ipv6; keep simple
	if strings.HasPrefix(hostport, "[") {
		return hostport, "", nil
	}
	if i := strings.LastIndex(hostport, ":"); i > 0 {
		return hostport[:i], hostport[i+1:], nil
	}
	return hostport, "", nil
}

// ClassifyGovernance returns a stable reason or empty if no structural rule hits.
// Does NOT apply zombie counters (those need success/fail); only structural class.
func (g GovernanceConfig) ClassifyStructural(protocol, rawURI string) string {
	if !g.Enabled {
		return ""
	}
	proto := strings.ToLower(strings.TrimSpace(protocol))
	if proto == "" {
		if i := strings.Index(rawURI, "://"); i > 0 {
			proto = strings.ToLower(rawURI[:i])
		}
	}
	for _, p := range g.IsolateProtocols {
		if strings.EqualFold(strings.TrimSpace(p), proto) {
			return "isolate_protocol:" + proto
		}
	}
	if g.IsolateVlessReality && proto == "vless" {
		sec, _ := URISecurityType(rawURI)
		if sec == "reality" {
			return "isolate_vless_reality"
		}
	}
	return ""
}

// ShouldSkipZombieAutoBlacklist protects flaky vless-ws and all vmess.
func (g GovernanceConfig) ShouldSkipZombieAutoBlacklist(protocol, rawURI string, successCount int64) bool {
	if !g.Enabled {
		return false
	}
	proto := strings.ToLower(strings.TrimSpace(protocol))
	if proto == "vmess" {
		return true
	}
	if successCount >= int64(g.FlakyMinSuccess) {
		// Never treat high-success nodes as zero-success zombies.
		return true
	}
	if proto == "vless" {
		sec, typ := URISecurityType(rawURI)
		if sec == "tls" && (typ == "ws" || typ == "") {
			// Prefer recovery for tls-ws class when they have any history.
			if successCount > 0 {
				return true
			}
		}
	}
	return false
}

// IsKnownDeadVlessWSHost returns true for hosts that experiments showed never healthy.
// Used when HostQuarantine is enabled; keep list small and evidence-based.
func IsKnownDeadVlessWSHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	// From governance experiment: entire cloudflare.182682.xyz family never ok.
	if host == "cloudflare.182682.xyz" || host == "www.cloudflare.182682.xyz" {
		return true
	}
	if strings.HasSuffix(host, ".cloudflare.182682.xyz") {
		return true
	}
	return false
}

func (g GovernanceConfig) ClassifyHostQuarantine(protocol, rawURI string) string {
	if !g.Enabled || !g.HostQuarantine {
		return ""
	}
	proto := strings.ToLower(strings.TrimSpace(protocol))
	if proto != "vless" {
		return ""
	}
	sec, typ := URISecurityType(rawURI)
	if sec != "tls" {
		return ""
	}
	if typ != "ws" && typ != "websocket" {
		return ""
	}
	h := URIHost(rawURI)
	if IsKnownDeadVlessWSHost(h) {
		return "host_quarantine:" + h
	}
	return ""
}
