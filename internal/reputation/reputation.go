package reputation

import "time"

// Result is the normalized reputation profile for one public exit IP.
type Result struct {
	IP          string    `json:"ip"`
	Country     string    `json:"country,omitempty"`
	CountryCode string    `json:"country_code,omitempty"`
	Region      string    `json:"region,omitempty"`
	City        string    `json:"city,omitempty"`
	ASN         string    `json:"asn,omitempty"`
	ISP         string    `json:"isp,omitempty"`
	Org         string    `json:"org,omitempty"`
	Hosting     bool      `json:"hosting"`
	Proxy       bool      `json:"proxy"`
	VPN         bool      `json:"vpn"`
	Tor         bool      `json:"tor"`
	Mobile      bool      `json:"mobile"`
	IsHosting   bool      `json:"is_hosting"`
	IsProxy     bool      `json:"is_proxy"`
	IsVPN       bool      `json:"is_vpn"`
	IsTor       bool      `json:"is_tor"`
	IsMobile    bool      `json:"is_mobile"`
	RiskScore   int       `json:"risk_score"`
	RiskLevel   string    `json:"risk_level"`
	Provider    string    `json:"provider"`
	Cached      bool      `json:"cached"`
	CheckedAt   time.Time `json:"checked_at"`
	LatencyMS   int64     `json:"latency_ms"`
	Error       string    `json:"error,omitempty"`
}

// NormalizeAliases keeps legacy and explicit boolean fields in sync.
func (r *Result) NormalizeAliases() {
	if r == nil {
		return
	}
	r.Proxy = r.Proxy || r.IsProxy
	r.IsProxy = r.Proxy
	r.VPN = r.VPN || r.IsVPN
	r.IsVPN = r.VPN
	r.Tor = r.Tor || r.IsTor
	r.IsTor = r.Tor
	r.Hosting = r.Hosting || r.IsHosting
	r.IsHosting = r.Hosting
	r.Mobile = r.Mobile || r.IsMobile
	r.IsMobile = r.Mobile
}

// NodeResult binds a reputation result to a local easy_proxies entry.
type NodeResult struct {
	NodeName string  `json:"node_name,omitempty"`
	NodeTag  string  `json:"node_tag,omitempty"`
	Region   string  `json:"region,omitempty"`
	Host     string  `json:"host,omitempty"`
	Port     uint16  `json:"port,omitempty"`
	Mode     string  `json:"mode,omitempty"`
	Result   *Result `json:"result,omitempty"`
	Error    string  `json:"error,omitempty"`
}

// RiskLevel converts a numeric score into a UI-friendly level.
func RiskLevel(score int) string {
	switch {
	case score <= 30:
		return "low"
	case score <= 70:
		return "medium"
	default:
		return "high"
	}
}
