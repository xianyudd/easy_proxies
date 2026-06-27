package nodesource

import (
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	DefaultFilterWorkers       = 200
	MaxFilterWorkers           = 800
	TargetFullScanBatchLatency = 8 * time.Second
	MinAdaptiveFilterTimeout   = 2 * time.Second
)

// FilterConfig controls optional pre-ingestion validation for free proxy sources.
type FilterConfig struct {
	Enabled            bool          `yaml:"enabled" json:"enabled"`
	MinTier            string        `yaml:"min_tier" json:"min_tier"`
	Workers            int           `yaml:"workers" json:"workers"`
	Timeout            time.Duration `yaml:"timeout" json:"timeout"`
	MaxCandidates      int           `yaml:"max_candidates" json:"max_candidates"`
	MaxProbeCandidates int           `yaml:"max_probe_candidates" json:"max_probe_candidates"`
	Probes             FilterProbes  `yaml:"probes" json:"probes"`
}

// FilterProbes names the probe URLs used by the free-proxy prefilter.
type FilterProbes struct {
	HTTP  string `yaml:"http" json:"http"`
	HTTPS string `yaml:"https" json:"https"`
}

// FilterResult is the output of applying a free-proxy prefilter.
type FilterResult struct {
	Accepted []Node
	Summary  FilterSummary
}

// FilterSummary describes how many candidate proxies survived each tier.
type FilterSummary struct {
	Total      int            `json:"total"`
	Accepted   int            `json:"accepted"`
	Rejected   int            `json:"rejected"`
	TierCounts map[string]int `json:"tier_counts"`
}

type nodeDecision struct {
	key  string
	node Node
	tier string
}

// Normalized returns a copy with safe defaults and bounded concurrency.
func (f FilterConfig) Normalized() FilterConfig {
	if strings.TrimSpace(f.MinTier) == "" {
		f.MinTier = "http_basic"
	}
	f.MinTier = strings.ToLower(strings.TrimSpace(f.MinTier))
	if f.Workers <= 0 {
		f.Workers = DefaultFilterWorkers
	}
	if f.Workers > MaxFilterWorkers {
		f.Workers = MaxFilterWorkers
	}
	if f.Timeout <= 0 {
		f.Timeout = 2 * time.Second
	}
	if f.MaxCandidates < 0 {
		f.MaxCandidates = 0
	}
	if f.MaxProbeCandidates < 0 {
		f.MaxProbeCandidates = 0
	}
	if f.Probes.HTTP == "" {
		f.Probes.HTTP = "http://cp.cloudflare.com/generate_204"
	}
	if f.Probes.HTTPS == "" {
		f.Probes.HTTPS = "https://example.com/"
	}
	return f
}

// LoadLimit caps source parsing before filtering. A zero/negative return means uncapped.
func (f FilterConfig) LoadLimit(remaining int) int {
	if !f.Enabled || f.MaxCandidates <= 0 {
		return remaining
	}
	if remaining <= 0 || f.MaxCandidates < remaining {
		return f.MaxCandidates
	}
	return remaining
}

// SelectProbeCandidates returns the candidate subset to probe. A non-positive
// maxProbeCandidates keeps the full source. When capped, candidates are sampled
// deterministically across the whole source instead of taking only the head, so
// very large lists do not let one stale prefix hide later usable entries.
func SelectProbeCandidates(nodes []Node, maxProbeCandidates int) []Node {
	if maxProbeCandidates <= 0 || len(nodes) <= maxProbeCandidates {
		return nodes
	}
	if maxProbeCandidates == 1 {
		return append([]Node(nil), nodes[0])
	}
	out := make([]Node, 0, maxProbeCandidates)
	last := -1
	denom := maxProbeCandidates - 1
	for i := 0; i < maxProbeCandidates; i++ {
		idx := i * (len(nodes) - 1) / denom
		if idx == last {
			continue
		}
		out = append(out, nodes[idx])
		last = idx
	}
	return out
}

// FilterNodes validates candidates and preserves source order for accepted nodes.
func FilterNodes(nodes []Node, cfg FilterConfig) FilterResult {
	cfg = cfg.Normalized()
	if !cfg.Enabled || len(nodes) == 0 {
		return FilterResult{Accepted: nodes, Summary: FilterSummary{Total: len(nodes), Accepted: len(nodes), Rejected: 0, TierCounts: map[string]int{"unfiltered": len(nodes)}}}
	}
	workers := cfg.effectiveWorkers(len(nodes))
	cfg.Timeout = cfg.effectiveTimeout(len(nodes), workers)

	jobs := make(chan Node)
	decisions := make(chan nodeDecision, len(nodes))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for node := range jobs {
				tier := cfg.Tier(node.URI)
				if TierRank(tier) >= TierRank(cfg.MinTier) {
					decisions <- nodeDecision{key: canonicalURI(node.URI), node: node, tier: tier}
				} else {
					decisions <- nodeDecision{key: canonicalURI(node.URI), node: node, tier: "reject"}
				}
			}
		}()
	}
	for _, node := range nodes {
		jobs <- node
	}
	close(jobs)
	wg.Wait()
	close(decisions)

	byKey := make(map[string]nodeDecision, len(nodes))
	summary := FilterSummary{Total: len(nodes), TierCounts: make(map[string]int)}
	for decision := range decisions {
		summary.TierCounts[decision.tier]++
		if decision.tier != "reject" {
			byKey[decision.key] = decision
		}
	}

	accepted := make([]Node, 0, len(byKey))
	for _, node := range nodes {
		if decision, ok := byKey[canonicalURI(node.URI)]; ok {
			accepted = append(accepted, decision.node)
		}
	}
	summary.Accepted = len(accepted)
	summary.Rejected = len(nodes) - len(accepted)
	return FilterResult{Accepted: accepted, Summary: summary}
}

func (f FilterConfig) effectiveWorkers(candidateCount int) int {
	f = f.Normalized()
	if candidateCount <= 0 {
		return 0
	}
	workers := f.Workers
	if f.MaxCandidates <= 0 && f.Timeout > 0 && candidateCount > workers {
		needed := int(math.Ceil(float64(candidateCount) * float64(f.Timeout) / float64(TargetFullScanBatchLatency)))
		if needed > workers {
			workers = needed
		}
	}
	if workers < 1 {
		workers = 1
	}
	if workers > MaxFilterWorkers {
		workers = MaxFilterWorkers
	}
	if workers > candidateCount {
		workers = candidateCount
	}
	return workers
}

func (f FilterConfig) effectiveTimeout(candidateCount, workers int) time.Duration {
	f = f.Normalized()
	// Skip adaptive tuning: when user set max_candidates, when candidates
	// fit inside one batch, or when candidates vastly outnumber workers
	// (adaptive formula would yield an unreasonably short timeout).
	if f.MaxCandidates > 0 || candidateCount <= workers || workers <= 0 || f.Timeout <= MinAdaptiveFilterTimeout {
		return f.Timeout
	}
	if candidateCount > workers*4 {
		return f.Timeout
	}
	target := time.Duration(float64(TargetFullScanBatchLatency) * float64(workers) / float64(candidateCount))
	if target < MinAdaptiveFilterTimeout {
		target = MinAdaptiveFilterTimeout
	}
	if target > f.Timeout {
		return f.Timeout
	}
	return target
}

// Tier returns the lightweight pre-ingestion tier for one proxy URI.
func (f FilterConfig) Tier(proxyURI string) string {
	f = f.Normalized()
	httpOK := f.probe(proxyURI, f.Probes.HTTP, http.StatusNoContent, "")
	if !httpOK {
		return "reject"
	}
	if TierRank(f.MinTier) <= TierRank("http_basic") {
		return "http_basic"
	}
	if f.probe(proxyURI, f.Probes.HTTPS, http.StatusOK, "Example Domain") {
		return "simple_web"
	}
	return "http_basic"
}

// TierRank ranks lightweight free proxy tiers.
func TierRank(tier string) int {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "reject":
		return 0
	case "http_basic", "http_basic_only":
		return 1
	case "simple_web", "simple_web_scraping", "web":
		return 2
	case "recommended", "general_web":
		return 3
	default:
		return 1
	}
}

func (f FilterConfig) probe(proxyURI, target string, wantStatus int, wantBody string) bool {
	proxyURL, err := url.Parse(strings.TrimSpace(proxyURI))
	if err != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
		return false
	}
	if strings.HasPrefix(target, "/") {
		target = strings.TrimRight(proxyURI, "/") + target
	}
	targetURL, err := url.Parse(target)
	if err != nil || targetURL.Scheme == "" || targetURL.Host == "" {
		return false
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: f.Timeout, Transport: transport}
	resp, err := client.Get(targetURL.String())
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		return false
	}
	if wantBody == "" {
		return true
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return false
	}
	return strings.Contains(string(body), wantBody)
}

func canonicalURI(uri string) string {
	return strings.ToLower(strings.TrimSpace(uri))
}
