package nodesource

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const DefaultMaxBytes int64 = 2 * 1024 * 1024

// SourceConfig configures one external free-proxy source.
type SourceConfig struct {
	Name     string        `yaml:"name" json:"name"`
	URL      string        `yaml:"url" json:"url"`
	File     string        `yaml:"file" json:"file"`
	Format   string        `yaml:"format" json:"format"`
	Enabled  *bool         `yaml:"enabled" json:"enabled"`
	Timeout  time.Duration `yaml:"timeout" json:"timeout"`
	MaxNodes int           `yaml:"max_nodes" json:"max_nodes"`
	MaxBytes int64         `yaml:"max_bytes" json:"max_bytes"`
}

// EnabledValue reports whether this source should be loaded.
func (c SourceConfig) EnabledValue() bool {
	return c.Enabled == nil || *c.Enabled
}

// Node is the source-layer representation of one proxy endpoint.
type Node struct {
	Name       string
	URI        string
	Region     string
	SourceName string
}

// Provider loads nodes from a configured source.
type Provider struct {
	cfg SourceConfig
}

func NewProvider(cfg SourceConfig) *Provider {
	return &Provider{cfg: cfg}
}

func (p *Provider) Load() ([]Node, error) {
	return p.LoadLimited(0)
}

// LoadLimited loads nodes with an optional caller cap. The lower positive cap
// between SourceConfig.MaxNodes and maxNodes is used; <=0 means uncapped.
func (p *Provider) LoadLimited(maxNodes int) ([]Node, error) {
	if p == nil {
		return nil, fmt.Errorf("provider is nil")
	}
	if !p.cfg.EnabledValue() {
		return nil, nil
	}

	effectiveMaxNodes := effectiveNodeLimit(p.cfg.MaxNodes, maxNodes)
	maxBytes := p.cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}

	var body []byte
	var err error
	switch {
	case strings.TrimSpace(p.cfg.File) != "":
		body, err = readFileLimited(p.cfg.File, maxBytes)
		if err != nil {
			return nil, fmt.Errorf("read source file: %w", err)
		}
	case strings.TrimSpace(p.cfg.URL) != "":
		body, err = fetch(p.cfg.URL, p.cfg.Timeout, maxBytes)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("source %q must set file or url", p.cfg.Name)
	}

	nodes, err := ParseFreeProxyContentLimited(p.cfg.Format, body, effectiveMaxNodes)
	if err != nil {
		return nil, err
	}
	for i := range nodes {
		nodes[i].SourceName = p.cfg.Name
	}
	return nodes, nil
}

func effectiveNodeLimit(a, b int) int {
	switch {
	case a > 0 && b > 0 && a < b:
		return a
	case a > 0 && b > 0:
		return b
	case a > 0:
		return a
	default:
		return b
	}
}

func readFileLimited(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readLimited(f, maxBytes)
}

func readLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	limited := io.LimitReader(r, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("source exceeds max_bytes %d", maxBytes)
	}
	return body, nil
}

func fetch(rawURL string, timeout time.Duration, maxBytes int64) ([]byte, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("source returned status %d", resp.StatusCode)
	}
	body, err := readLimited(resp.Body, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("read source response: %w", err)
	}
	return body, nil
}

// ParseFreeProxyContent parses a free-proxy source in txt or simple JSON formats.
func ParseFreeProxyContent(format string, data []byte) ([]Node, error) {
	return ParseFreeProxyContentLimited(format, data, 0)
}

// ParseFreeProxyContentLimited parses a free-proxy source and stops after
// maxNodes valid entries when maxNodes > 0.
func ParseFreeProxyContentLimited(format string, data []byte, maxNodes int) ([]Node, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	content := strings.TrimSpace(string(data))
	if format == "" || format == "auto" {
		if strings.HasPrefix(content, "[") || strings.HasPrefix(content, "{") {
			format = "json"
		} else {
			format = "txt"
		}
	}

	switch format {
	case "txt", "text", "plain":
		return parseTextLimited(data, maxNodes), nil
	case "json":
		return parseJSONLimited(data, maxNodes)
	default:
		return nil, fmt.Errorf("unsupported free proxy source format %q", format)
	}
}

func parseText(content string) []Node {
	return parseTextLimited([]byte(content), 0)
}

func parseTextLimited(data []byte, maxNodes int) []Node {
	var nodes []Node
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if maxNodes > 0 && len(nodes) >= maxNodes {
			break
		}
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if uri := normalizeProxyURI(line, ""); uri != "" {
			nodes = append(nodes, Node{URI: uri})
		}
	}
	return nodes
}

type jsonProxy struct {
	URI      string      `json:"uri"`
	URL      string      `json:"url"`
	Name     string      `json:"name"`
	IP       string      `json:"ip"`
	Host     string      `json:"host"`
	Address  string      `json:"address"`
	Port     interface{} `json:"port"`
	Protocol string      `json:"protocol"`
	Type     string      `json:"type"`
	Country  string      `json:"country"`
	Region   string      `json:"region"`
}

func parseJSON(data []byte) ([]Node, error) {
	return parseJSONLimited(data, 0)
}

func parseJSONLimited(data []byte, maxNodes int) ([]Node, error) {
	var arr []jsonProxy
	if err := json.Unmarshal(data, &arr); err != nil {
		var wrapped struct {
			Proxies []jsonProxy `json:"proxies"`
			Data    []jsonProxy `json:"data"`
			Items   []jsonProxy `json:"items"`
		}
		if err2 := json.Unmarshal(data, &wrapped); err2 != nil {
			return nil, fmt.Errorf("parse json source: %w", err)
		}
		switch {
		case len(wrapped.Proxies) > 0:
			arr = wrapped.Proxies
		case len(wrapped.Data) > 0:
			arr = wrapped.Data
		default:
			arr = wrapped.Items
		}
	}

	var nodes []Node
	for _, item := range arr {
		if maxNodes > 0 && len(nodes) >= maxNodes {
			break
		}
		uri := firstNonEmpty(item.URI, item.URL)
		if uri == "" {
			host := firstNonEmpty(item.IP, item.Host, item.Address)
			port := stringifyPort(item.Port)
			if host != "" && port != "" {
				uri = net.JoinHostPort(host, port)
			}
		}
		uri = normalizeProxyURI(uri, firstNonEmpty(item.Protocol, item.Type))
		if uri == "" {
			continue
		}
		nodes = append(nodes, Node{
			Name:   strings.TrimSpace(item.Name),
			URI:    uri,
			Region: firstNonEmpty(item.Region, item.Country),
		})
	}
	return nodes, nil
}

func normalizeProxyURI(value, scheme string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return ""
		}
		return value
	}
	if scheme == "" {
		scheme = "http"
	}
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme == "" {
		scheme = "http"
	}
	if _, _, err := net.SplitHostPort(value); err != nil {
		return ""
	}
	return scheme + "://" + value
}

func stringifyPort(port interface{}) string {
	switch v := port.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v <= 0 {
			return ""
		}
		return strconv.Itoa(int(v))
	case int:
		if v <= 0 {
			return ""
		}
		return strconv.Itoa(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
