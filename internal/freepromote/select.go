package freepromote

import (
	"sort"
	"strings"
	"time"

	"easy_proxies/internal/config"
)

// Candidate is a free_proxy node eligible for promotion.
type Candidate struct {
	Name          string
	URI           string
	SuccessCount  int64
	FailureCount  int
	LastLatencyMs int64
	LastSuccess   time.Time
	LastFailure   time.Time
	LastError     string
	Region        string
	Country       string
}

// Snapshot is the runtime health view needed for promotion selection.
type Snapshot struct {
	Name             string
	URI              string
	Source           string
	Port             uint16
	Available        bool
	InitialCheckDone bool
	Blacklisted      bool
	SuccessCount     int64
	FailureCount     int
	LastLatencyMs    int64
	LastSuccess      time.Time
	LastFailure      time.Time
	LastError        string
	Region           string
	Country          string
}

// CountPromoted returns how many config nodes use the promote name prefix.
func CountPromoted(nodes []config.NodeConfig, prefix string) int {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return 0
	}
	count := 0
	for _, node := range nodes {
		if strings.HasPrefix(node.Name, prefix) {
			count++
		}
	}
	return count
}

// PromotedURISet returns canonical URIs already held by promoted (or any non-free) nodes.
func PromotedURISet(nodes []config.NodeConfig, prefix string) map[string]struct{} {
	out := make(map[string]struct{}, len(nodes))
	prefix = strings.TrimSpace(prefix)
	for _, node := range nodes {
		key := canonicalURI(node.URI)
		if key == "" {
			continue
		}
		if node.Source != config.NodeSourceFreeProxy {
			out[key] = struct{}{}
			continue
		}
		if prefix != "" && strings.HasPrefix(node.Name, prefix) {
			out[key] = struct{}{}
		}
	}
	return out
}

// SelectCandidates picks up to batchSize free_proxy snapshots that pass filters.
// Results are ordered by ascending latency, then by success count descending.
func SelectCandidates(snaps []Snapshot, nodes []config.NodeConfig, cfg config.FreeProxyPromoteConfig) []Candidate {
	cfg = cfg.Normalized()
	promoted := CountPromoted(nodes, cfg.NamePrefix)
	slots := cfg.MaxPromoted - promoted
	if slots <= 0 {
		return nil
	}
	limit := cfg.BatchSize
	if limit > slots {
		limit = slots
	}
	if limit <= 0 {
		return nil
	}

	seenURI := PromotedURISet(nodes, cfg.NamePrefix)
	// Also skip URIs already selected-looking free nodes that somehow share promoted names.
	for _, node := range nodes {
		if key := canonicalURI(node.URI); key != "" && strings.HasPrefix(node.Name, cfg.NamePrefix) {
			seenURI[key] = struct{}{}
		}
	}

	candidates := make([]Candidate, 0, limit)
	for _, snap := range snaps {
		if !strings.EqualFold(strings.TrimSpace(snap.Source), string(config.NodeSourceFreeProxy)) {
			continue
		}
		if snap.Blacklisted || !snap.InitialCheckDone || !snap.Available {
			continue
		}
		if snap.SuccessCount < cfg.MinSuccessCount {
			continue
		}
		if cfg.MaxFailureCount >= 0 && snap.FailureCount > cfg.MaxFailureCount {
			continue
		}
		if cfg.RecentSuccessWithin > 0 {
			if snap.LastSuccess.IsZero() || time.Since(snap.LastSuccess) > cfg.RecentSuccessWithin {
				continue
			}
		}
		if cfg.MaxLatencyMS > 0 && snap.LastLatencyMs > 0 && snap.LastLatencyMs > cfg.MaxLatencyMS {
			continue
		}
		uri := strings.TrimSpace(snap.URI)
		key := canonicalURI(uri)
		if key == "" {
			continue
		}
		if _, exists := seenURI[key]; exists {
			continue
		}
		seenURI[key] = struct{}{}
		name := strings.TrimSpace(snap.Name)
		if name == "" {
			name = key
		}
		candidates = append(candidates, Candidate{
			Name:          name,
			URI:           uri,
			SuccessCount:  snap.SuccessCount,
			FailureCount:  snap.FailureCount,
			LastLatencyMs: snap.LastLatencyMs,
			LastSuccess:   snap.LastSuccess,
			LastFailure:   snap.LastFailure,
			LastError:     strings.TrimSpace(snap.LastError),
			Region:        strings.TrimSpace(snap.Region),
			Country:       strings.TrimSpace(snap.Country),
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		li, lj := candidates[i].LastLatencyMs, candidates[j].LastLatencyMs
		// Prefer known low latency; treat 0 as unknown and push later.
		if li == 0 && lj != 0 {
			return false
		}
		if lj == 0 && li != 0 {
			return true
		}
		if li != lj {
			return li < lj
		}
		if candidates[i].SuccessCount != candidates[j].SuccessCount {
			return candidates[i].SuccessCount > candidates[j].SuccessCount
		}
		return candidates[i].URI < candidates[j].URI
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

// PromotedNodeName builds a stable unique name for a promoted free proxy.
func PromotedNodeName(prefix, uri string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = config.DefaultFreeProxyPromoteNamePrefix
	}
	sum := fnv32a(canonicalURI(uri))
	return prefix + formatHex8(sum)
}

func canonicalURI(uri string) string {
	uri = strings.TrimSpace(uri)
	if i := strings.Index(uri, "#"); i >= 0 {
		uri = uri[:i]
	}
	return strings.ToLower(strings.TrimSpace(uri))
}

func fnv32a(s string) uint32 {
	const (
		offset = 2166136261
		prime  = 16777619
	)
	h := uint32(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	return h
}

func formatHex8(v uint32) string {
	const hexdigits = "0123456789abcdef"
	var b [8]byte
	for i := 7; i >= 0; i-- {
		b[i] = hexdigits[v&0xf]
		v >>= 4
	}
	return string(b[:])
}
