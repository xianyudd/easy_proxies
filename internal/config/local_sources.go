package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Local node source limits (EP-02).
const (
	// MaxLocalSourceBodyBytes caps the raw import text at 1 MiB.
	MaxLocalSourceBodyBytes = 1 << 20
	// MaxLocalSourceLineBytes caps a single import line at 16 KiB.
	MaxLocalSourceLineBytes = 16 << 10
	// MaxLocalSourceNodesPerSource caps members of one source at 500.
	MaxLocalSourceNodesPerSource = 500
	// MaxLocalSourceCount caps the number of named local sources at 50.
	MaxLocalSourceCount = 50
	// MaxLocalSourceNodesTotal caps members across all sources (including
	// disabled ones) at 5000.
	MaxLocalSourceNodesTotal = 5000
	// MinLocalSourceNameRunes / MaxLocalSourceNameRunes bound source names.
	MinLocalSourceNameRunes = 1
	MaxLocalSourceNameRunes = 80
)

// NodeSourceLocal marks nodes materialized from a named local source.
// Local nodes are runtime-only: they must never be persisted into
// config.yaml's nodes array, nodes.txt or the subscription cache.
const NodeSourceLocal NodeSource = "local_source"

// LocalSourceNode is one member of a named local source: an independent
// snapshot of the raw protocol URI exactly as imported.
type LocalSourceNode struct {
	URI  string `yaml:"uri" json:"uri"`
	Port uint16 `yaml:"port,omitempty" json:"port,omitempty"`
}

// LocalSource is one named, persisted local node source.
type LocalSource struct {
	ID      string            `yaml:"id" json:"id"`
	Name    string            `yaml:"name" json:"name"`
	Enabled bool              `yaml:"enabled" json:"enabled"`
	Nodes   []LocalSourceNode `yaml:"nodes" json:"nodes"`
}

// LocalSourcesDraft is the un-applied editing state. A nil draft means "no
// draft exists"; an empty Sources slice is a valid draft that clears all
// sources on apply.
type LocalSourcesDraft struct {
	Revision     int           `yaml:"revision" json:"revision"`
	BaseRevision int           `yaml:"base_revision" json:"base_revision"`
	Sources      []LocalSource `yaml:"sources" json:"sources"`
}

// CloneLocalSources returns a deep copy of the given sources. nil stays nil
// so an absent list cannot silently become an empty one.
func CloneLocalSources(sources []LocalSource) []LocalSource {
	if sources == nil {
		return nil
	}
	out := make([]LocalSource, len(sources))
	for i, source := range sources {
		out[i] = LocalSource{
			ID:      source.ID,
			Name:    source.Name,
			Enabled: source.Enabled,
			Nodes:   CloneLocalSourceNodes(source.Nodes),
		}
	}
	return out
}

// CloneLocalSourceNodes deep-copies member snapshots.
func CloneLocalSourceNodes(nodes []LocalSourceNode) []LocalSourceNode {
	if nodes == nil {
		return nil
	}
	out := make([]LocalSourceNode, len(nodes))
	copy(out, nodes)
	return out
}

// CloneLocalSourcesDraft deep-copies a draft, preserving nil-vs-empty
// semantics of Sources.
func CloneLocalSourcesDraft(draft *LocalSourcesDraft) *LocalSourcesDraft {
	if draft == nil {
		return nil
	}
	clone := &LocalSourcesDraft{
		Revision:     draft.Revision,
		BaseRevision: draft.BaseRevision,
		Sources:      CloneLocalSources(draft.Sources),
	}
	// Preserve explicit empty slice (Sources != nil but empty) vs nil.
	if draft.Sources != nil && clone.Sources == nil {
		clone.Sources = []LocalSource{}
	}
	return clone
}

// NewLocalSourceID returns a fresh random, URL-safe source identifier.
func NewLocalSourceID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing is unrecoverable; fall back to a timestamp-free
		// constant so callers see an obviously invalid id rather than a
		// colliding one. In practice rand.Read never fails on Linux.
		panic(fmt.Sprintf("generate local source id: %v", err))
	}
	return "ls-" + hex.EncodeToString(buf)
}

// ValidateLocalSourceName checks a source display name: 1–80 runes, no
// control characters (trimmable whitespace around the name is allowed).
func ValidateLocalSourceName(name string) error {
	name = strings.TrimSpace(name)
	runes := utf8.RuneCountInString(name)
	if runes < MinLocalSourceNameRunes {
		return fmt.Errorf("local source name must be %d-%d characters", MinLocalSourceNameRunes, MaxLocalSourceNameRunes)
	}
	if runes > MaxLocalSourceNameRunes {
		return fmt.Errorf("local source name must be %d-%d characters", MinLocalSourceNameRunes, MaxLocalSourceNameRunes)
	}
	if strings.IndexFunc(name, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf) }) >= 0 {
		return errors.New("local source name must not contain control characters")
	}
	return nil
}

// LocalSourceDuplicate records an in-source duplicate that was deduplicated.
// Reports never echo URI credentials.
type LocalSourceDuplicate struct {
	Line int    `json:"line"`
	Why  string `json:"reason"`
}

// LocalSourceParseError contains only a fixed reason and the original input line.
// Internal/storage errors must never be converted to this public error type.
type LocalSourceParseError struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

func (e *LocalSourceParseError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Reason) }
func localParseError(line int, reason string) error {
	return &LocalSourceParseError{Line: line, Reason: reason}
}

// LocalSourcePreview reports unique connections shared with other owners.
// SharedCount uses the editing (draft, or active when absent) view plus non-local
// owners. Active and draft counts are separate and must not be added together.
type LocalSourcePreview struct {
	MemberCount    int                    `json:"member_count"`
	Duplicates     []LocalSourceDuplicate `json:"duplicates"`
	Protocols      map[string]int         `json:"protocols"`
	Revision       int                    `json:"revision"`
	SharedCount    int                    `json:"shared_count"`
	SharedNonLocal int                    `json:"shared_non_local"`
	SharedDraft    int                    `json:"shared_draft"`
	SharedActive   int                    `json:"shared_active"`
}

// LocalSourceParseResult is the outcome of a strict import.
type LocalSourceParseResult struct {
	Nodes      []LocalSourceNode
	Duplicates []LocalSourceDuplicate
}

// ParseLocalSourceNodes strictly parses line-delimited proxy URIs.
// Supported input: UTF-8 with optional BOM, CRLF or LF line endings, blank
// lines, and lines whose first non-space character is '#'. Any invalid line,
// unknown protocol, overlong line/body or member-cap overflow rejects the
// whole import; errors carry the line number and a sanitized reason that
// never echoes URI credentials.
func ParseLocalSourceNodes(content string) (LocalSourceParseResult, error) {
	if len(content) > MaxLocalSourceBodyBytes {
		return LocalSourceParseResult{}, fmt.Errorf("local source content exceeds %d bytes (1 MiB)", MaxLocalSourceBodyBytes)
	}
	content = strings.TrimPrefix(content, string(rune(0xFEFF)))
	result := LocalSourceParseResult{}
	schemeCounts := map[string]int{}
	lineNo := 0
	// nodeLines records the original input line number of each parsed node so
	// duplicate reports can reference the raw file (comments and blank lines
	// included), not the post-filter node index.
	var nodeLines []int
	for _, raw := range strings.Split(content, "\n") {
		lineNo++
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) > MaxLocalSourceLineBytes {
			return LocalSourceParseResult{}, localParseError(lineNo, "line exceeds 16384 bytes (16 KiB)")
		}
		if !utf8.ValidString(line) {
			return LocalSourceParseResult{}, localParseError(lineNo, "line is not valid UTF-8")
		}
		scheme, err := localSourceURIScheme(line, lineNo)
		if err != nil {
			return LocalSourceParseResult{}, err
		}
		schemeCounts[scheme]++
		if isBase64(line) {
			return LocalSourceParseResult{}, localParseError(lineNo, "base64 subscription payloads are not supported; paste one URI per line")
		}
		if err := validateLocalSourceURI(line, lineNo); err != nil {
			return LocalSourceParseResult{}, err
		}
		result.Nodes = append(result.Nodes, LocalSourceNode{URI: line})
		nodeLines = append(nodeLines, lineNo)
	}
	if len(result.Nodes) == 0 {
		return LocalSourceParseResult{}, errors.New("local source contains no proxy nodes")
	}
	if len(result.Nodes) > MaxLocalSourceNodesPerSource {
		return LocalSourceParseResult{}, fmt.Errorf("local source exceeds %d nodes per source", MaxLocalSourceNodesPerSource)
	}
	var dupNodes []LocalSourceNode
	var duplicates []LocalSourceDuplicate
	var err error
	dupNodes, duplicates, err = dedupeLocalSourceNodes(result.Nodes, nodeLines)
	if err != nil {
		return LocalSourceParseResult{}, err
	}
	result.Nodes = dupNodes
	result.Duplicates = duplicates
	return result, nil
}

// localSourceURIScheme extracts and validates the protocol scheme.
func localSourceURIScheme(line string, lineNo int) (string, error) {
	idx := strings.Index(line, "://")
	if idx <= 0 {
		return "", localParseError(lineNo, "not a protocol URI")
	}
	scheme := strings.ToLower(line[:idx])
	if !IsProxyURI(line) {
		// Fixed safe reason: the raw prefix before "://" is attacker/user
		// controlled and may contain credentials, so it must never be echoed.
		return "", localParseError(lineNo, "unsupported protocol")
	}
	return scheme, nil
}

// validateLocalSourceURI runs structural checks on a single URI. Errors are
// sanitized: the URI itself is never included.
func validateLocalSourceURI(line string, lineNo int) error {
	return validateLocalProtocol(line, lineNo)
}

// dedupeLocalSourceNodes removes in-source duplicates, keeping the first
// occurrence and reporting later ones with sanitized reasons. Two URIs are
// the same node only when their connection text (everything before the
// fragment) matches exactly — including case, since credentials and hosts
// may be case-sensitive. A NodeKey (lowercased) match with differing
// connection text is treated as a credential/key collision and rejects the
// whole import instead of silently merging possibly different endpoints.
// nodeLines carries each node's original input line number; reports use it
// so line numbers count every raw input line, including comments and blanks.
func dedupeLocalSourceNodes(nodes []LocalSourceNode, nodeLines []int) ([]LocalSourceNode, []LocalSourceDuplicate, error) {
	seenKeys := make(map[string]string, len(nodes))
	out := nodes[:0]
	var duplicates []LocalSourceDuplicate
	for i, node := range nodes {
		connectionText := node.URI
		if j := strings.Index(connectionText, "#"); j >= 0 {
			connectionText = connectionText[:j]
		}
		key := canonicalNodeURI(node.URI)
		if prevText, ok := seenKeys[key]; ok {
			if prevText == connectionText {
				line := i + 1
				if i < len(nodeLines) {
					line = nodeLines[i]
				}
				duplicates = append(duplicates, LocalSourceDuplicate{Line: line, Why: "duplicate node in source (same host, port and credentials)"})
				continue
			}
			return nil, nil, localParseError(nodeLines[i], "conflicting duplicate node (same key with different connection text)")
		}
		seenKeys[key] = connectionText
		out = append(out, node)
	}
	return out, duplicates, nil
}
