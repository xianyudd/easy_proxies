package config

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const subscriptionCachePrefix = "# easy-proxies-subscription "

func subscriptionID(rawURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(rawURL)))
	return hex.EncodeToString(sum[:])
}

func safeSubscriptionName(name string) string {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 100 ||
		strings.ContainsAny(name, `/\?&#=@%<>`) || strings.IndexFunc(name, unicode.IsControl) >= 0 ||
		strings.IndexFunc(name, func(r rune) bool { return unicode.In(r, unicode.Cf) }) >= 0 {
		return ""
	}
	return name
}

func safeSubscriptionHeaderName(name string, source *url.URL) string {
	name = safeSubscriptionName(name)
	if name == "" {
		return ""
	}
	// Avoid echoing subscription credentials even if a provider includes them in a title.
	for _, values := range source.Query() {
		for _, value := range values {
			if value == name || (len(value) >= 8 && strings.Contains(name, value)) {
				return ""
			}
		}
	}
	if source.User != nil {
		password, _ := source.User.Password()
		for _, value := range []string{source.User.Username(), password} {
			if value != "" && strings.Contains(name, value) {
				return ""
			}
		}
	}
	for _, part := range strings.Split(source.Path, "/") {
		if len(part) >= 12 && strings.Contains(name, part) {
			return ""
		}
	}
	return name
}

func subscriptionResponseName(header http.Header, source *url.URL) string {
	title := strings.TrimSpace(header.Get("Profile-Title"))
	if len(title) >= len("base64:") && strings.EqualFold(title[:len("base64:")], "base64:") {
		encoded := title[len("base64:"):]
		title = ""
		for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if decoded, err := encoding.DecodeString(encoded); err == nil {
				title = string(decoded)
				break
			}
		}
	}
	if name := safeSubscriptionHeaderName(title, source); name != "" {
		return name
	}
	if _, params, err := mime.ParseMediaType(header.Get("Content-Disposition")); err == nil {
		filename := params["filename"]
		if decoded, err := new(mime.WordDecoder).DecodeHeader(filename); err == nil {
			filename = decoded
		}
		switch strings.ToLower(filepath.Ext(filename)) {
		case ".yaml", ".yml", ".txt", ".json", ".conf":
			filename = strings.TrimSuffix(filename, filepath.Ext(filename))
		}
		if name := safeSubscriptionHeaderName(filename, source); name != "" {
			return name
		}
	}
	return source.Hostname()
}

// SubscriptionNames returns display names in the same order as Subscriptions.
func (c *Config) SubscriptionNames() []string {
	return SubscriptionNamesForNodes(c.Subscriptions, c.Nodes)
}

// SubscriptionNamesForNodes resolves labels from a snapshot of the current nodes.
func SubscriptionNamesForNodes(urls []string, nodes []NodeConfig) []string {
	byID := make(map[string]string)
	for _, node := range nodes {
		if node.Source == NodeSourceInline || node.Source == NodeSourceFreeProxy || node.Source == NodeSourceFile {
			continue
		}
		if node.SubscriptionID != "" && node.SubscriptionName != "" {
			byID[node.SubscriptionID] = node.SubscriptionName
		}
	}
	names := make([]string, 0, len(urls))
	for _, rawURL := range urls {
		name := byID[subscriptionID(rawURL)]
		if name == "" {
			if source, err := url.Parse(strings.TrimSpace(rawURL)); err == nil {
				name = source.Hostname()
			}
		}
		names = append(names, name)
	}
	return names
}

type subscriptionCacheEntry struct {
	Node string `json:"node"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

func subscriptionNodeDigest(uri string) string {
	uri, _, _ = strings.Cut(strings.TrimSpace(uri), "#")
	sum := sha256.Sum256([]byte(uri))
	return hex.EncodeToString(sum[:])
}

func subscriptionCacheComment(node NodeConfig, uri string) string {
	if node.SubscriptionID == "" || node.SubscriptionName == "" {
		return ""
	}
	entry := subscriptionCacheEntry{Node: subscriptionNodeDigest(uri), ID: node.SubscriptionID, Name: node.SubscriptionName}
	data, _ := json.Marshal(entry) // entry contains only strings
	return subscriptionCachePrefix + string(data)
}

func applySubscriptionCache(content string, nodes []NodeConfig) {
	var pending subscriptionCacheEntry
	index := 0
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, subscriptionCachePrefix) {
			pending = subscriptionCacheEntry{}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, subscriptionCachePrefix)), &pending); err != nil {
				pending = subscriptionCacheEntry{}
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") || !IsProxyURI(line) {
			continue
		}
		if index >= len(nodes) {
			return
		}
		id, err := hex.DecodeString(pending.ID)
		if name := safeSubscriptionName(pending.Name); name != "" && err == nil && len(id) == sha256.Size && pending.Node == subscriptionNodeDigest(line) {
			nodes[index].SubscriptionID = pending.ID
			nodes[index].SubscriptionName = name
		}
		index++
		pending = subscriptionCacheEntry{}
	}
}

// MergeSubscriptionNames updates known nodes when a partial fetch falls back
// to the aggregate cache. Unmatched cached nodes retain their last known source.
func MergeSubscriptionNames(cached, fetched []NodeConfig) (changed bool) {
	byNode := make(map[string][]NodeConfig, len(fetched))
	for _, node := range fetched {
		if node.SubscriptionID != "" {
			key := subscriptionNodeDigest(node.URI)
			byNode[key] = append(byNode[key], node)
		}
	}
	for i := range cached {
		old := &cached[i]
		matches := byNode[subscriptionNodeDigest(old.URI)]
		for _, node := range matches {
			if old.SubscriptionID != node.SubscriptionID && (old.SubscriptionID != "" || len(matches) != 1) {
				continue
			}
			if old.SubscriptionID != node.SubscriptionID || old.SubscriptionName != node.SubscriptionName {
				old.SubscriptionID = node.SubscriptionID
				old.SubscriptionName = node.SubscriptionName
				changed = true
			}
			break
		}
	}
	return changed
}
