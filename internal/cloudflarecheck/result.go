package cloudflarecheck

import "time"

// Result is a local Cloudflare compatibility score for one proxy target.
// It is not Cloudflare Enterprise Bot Score.
type Result struct {
	NodeName        string    `json:"node_name,omitempty"`
	NodeTag         string    `json:"node_tag,omitempty"`
	Region          string    `json:"region,omitempty"`
	Host            string    `json:"host,omitempty"`
	Port            uint16    `json:"port,omitempty"`
	ExitIP          string    `json:"exit_ip,omitempty"`
	CFLoc           string    `json:"cf_loc,omitempty"`
	CFColo          string    `json:"cf_colo,omitempty"`
	HTTPProtocol    string    `json:"http_protocol,omitempty"`
	TLSVersion      string    `json:"tls_version,omitempty"`
	Warp            string    `json:"warp,omitempty"`
	HTTP204OK       bool      `json:"http_204_ok"`
	TraceOK         bool      `json:"trace_ok"`
	ChallengeStatus string    `json:"challenge_status"`
	Score           int       `json:"score"`
	Level           string    `json:"level"`
	LatencyMS       int64     `json:"latency_ms"`
	Cached          bool      `json:"cached"`
	CheckedAt       time.Time `json:"checked_at"`
	Error           string    `json:"error,omitempty"`
}

// Trace is parsed from https://www.cloudflare.com/cdn-cgi/trace.
type Trace struct {
	IP   string `json:"ip,omitempty"`
	LOC  string `json:"loc,omitempty"`
	COLO string `json:"colo,omitempty"`
	HTTP string `json:"http,omitempty"`
	TLS  string `json:"tls,omitempty"`
	WARP string `json:"warp,omitempty"`
}

type ProxyTarget struct {
	NodeName string
	NodeTag  string
	Region   string
	Host     string
	Port     uint16
	ProxyURL string
}
