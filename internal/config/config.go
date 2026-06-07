package config

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy_proxies/internal/nodesource"

	"gopkg.in/yaml.v3"
)

// Config describes the high level settings for the proxy pool server.
type Config struct {
	Mode                string                    `yaml:"mode"`
	Listener            ListenerConfig            `yaml:"listener"`
	MultiPort           MultiPortConfig           `yaml:"multi_port"`
	AndroidProxy        AndroidProxyConfig        `yaml:"android_proxy"`
	Pool                PoolConfig                `yaml:"pool"`
	Management          ManagementConfig          `yaml:"management"`
	SubscriptionRefresh SubscriptionRefreshConfig `yaml:"subscription_refresh"`
	QualityCheck        QualityCheckConfig        `yaml:"quality_check"`
	GeoIP               GeoIPConfig               `yaml:"geoip"`
	Log                 LogConfig                 `yaml:"log"`
	Nodes               []NodeConfig              `yaml:"nodes"`
	FreeProxySources    []nodesource.SourceConfig `yaml:"free_proxy_sources"`
	FreeProxyMaxNodes   int                       `yaml:"free_proxy_max_nodes"`
	FreeProxyFilter     nodesource.FilterConfig   `yaml:"free_proxy_filter"`
	FreeProxyCache      FreeProxyCacheConfig      `yaml:"free_proxy_cache"`
	NodesFile           string                    `yaml:"nodes_file"`    // 节点文件路径，每行一个 URI
	Subscriptions       []string                  `yaml:"subscriptions"` // 订阅链接列表
	ExternalIP          string                    `yaml:"external_ip"`   // 外部 IP 地址，用于导出时替换 0.0.0.0
	LogLevel            string                    `yaml:"log_level"`
	SkipCertVerify      bool                      `yaml:"skip_cert_verify"` // 全局跳过 SSL 证书验证
	UpstreamProxy       string                    `yaml:"upstream_proxy"`   // Optional SOCKS/HTTP proxy used as sing-box outbound detour

	filePath string `yaml:"-"` // 配置文件路径，用于保存
}

// LogConfig controls log output and rotation.
type LogConfig struct {
	Output     string `yaml:"output"`      // 日志输出: "stdout", "file", 默认 "stdout"
	File       string `yaml:"file"`        // 日志文件路径，默认 "logs/easy_proxies.log"
	MaxSize    int    `yaml:"max_size"`    // 单个日志文件最大 MB，默认 50
	MaxBackups int    `yaml:"max_backups"` // 保留旧日志文件个数，默认 3
	MaxAge     int    `yaml:"max_age"`     // 保留旧日志文件天数，默认 7
	Compress   bool   `yaml:"compress"`    // 是否压缩旧日志，默认 false
}

// GeoIPConfig controls GeoIP-based region routing.
type GeoIPConfig struct {
	Enabled            bool          `yaml:"enabled"`              // 是否启用 GeoIP 地域分区
	DatabasePath       string        `yaml:"database_path"`        // GeoLite2-Country.mmdb 文件路径
	Listen             string        `yaml:"listen"`               // GeoIP 路由监听地址，默认使用 listener 配置
	Port               uint16        `yaml:"port"`                 // GeoIP 路由监听端口，默认 1221
	AutoUpdateEnabled  bool          `yaml:"auto_update_enabled"`  // 是否启用自动更新数据库
	AutoUpdateInterval time.Duration `yaml:"auto_update_interval"` // 自动更新间隔，默认 24 小时
}

// ListenerConfig defines how the HTTP/SOCKS5 mixed proxy should listen for clients.
type ListenerConfig struct {
	Address  string `yaml:"address"`
	Port     uint16 `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// PoolConfig configures scheduling + failure handling.
type PoolConfig struct {
	Mode              string        `yaml:"mode"`
	FailureThreshold  int           `yaml:"failure_threshold"`
	BlacklistDuration time.Duration `yaml:"blacklist_duration"`
}

// MultiPortConfig defines address/credential defaults for multi-port mode.
type MultiPortConfig struct {
	Address  string `yaml:"address"`
	BasePort uint16 `yaml:"base_port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// AndroidProxyConfig defines unauthenticated, country-specific HTTP proxy ports for Android global proxy.
type AndroidProxyConfig struct {
	Enabled     bool              `yaml:"enabled"`
	Listen      string            `yaml:"listen"`
	BasePort    uint16            `yaml:"base_port"`
	RegionPorts map[string]uint16 `yaml:"region_ports"`
}

// ManagementConfig controls the monitoring HTTP endpoint.
type ManagementConfig struct {
	Enabled        *bool  `yaml:"enabled"`
	Listen         string `yaml:"listen"`
	ProbeTarget    string `yaml:"probe_target"`
	Password       string `yaml:"password"`         // WebUI 访问密码，为空则不需要密码
	ClashAPIListen string `yaml:"clash_api_listen"` // sing-box Clash API 监听地址
}

// SubscriptionRefreshConfig controls subscription auto-refresh and reload settings.
type SubscriptionRefreshConfig struct {
	Enabled            bool          `yaml:"enabled"`              // 是否启用定时刷新
	Interval           time.Duration `yaml:"interval"`             // 刷新间隔，默认 1 小时
	Timeout            time.Duration `yaml:"timeout"`              // 获取订阅的超时时间
	HealthCheckTimeout time.Duration `yaml:"health_check_timeout"` // 新节点健康检查超时
	DrainTimeout       time.Duration `yaml:"drain_timeout"`        // 旧实例排空超时时间
	MinAvailableNodes  int           `yaml:"min_available_nodes"`  // 最少可用节点数，低于此值不切换
}

const (
	DefaultFreeProxyCacheWorkers = 8
	MaxFreeProxyCacheWorkers     = 32
)

const (
	DefaultQualityCheckInterval = 24 * time.Hour
	MinQualityCheckInterval     = time.Hour
	DefaultQualityCheckRegion   = "all"
	DefaultQualityCheckCount    = 500
	MaxQualityCheckCount        = 500
)

// FreeProxyCacheConfig controls cache-first loading for remote free-proxy sources.
type FreeProxyCacheConfig struct {
	Enabled        *bool         `yaml:"enabled" json:"enabled"`
	Path           string        `yaml:"path" json:"path"`
	RefreshOnStart *bool         `yaml:"refresh_on_start" json:"refresh_on_start"`
	AutoReload     *bool         `yaml:"auto_reload" json:"auto_reload"`
	Workers        int           `yaml:"workers" json:"workers"`
	MaxAge         time.Duration `yaml:"max_age" json:"max_age"`
}

func (f FreeProxyCacheConfig) EnabledValue() bool {
	return f.Enabled == nil || *f.Enabled
}

func (f FreeProxyCacheConfig) RefreshOnStartValue() bool {
	return f.RefreshOnStart == nil || *f.RefreshOnStart
}

func (f FreeProxyCacheConfig) AutoReloadValue() bool {
	return f.AutoReload == nil || *f.AutoReload
}

func (f FreeProxyCacheConfig) Normalized(configPath string, hasSources bool) FreeProxyCacheConfig {
	if f.Enabled == nil {
		enabled := hasSources
		f.Enabled = &enabled
	}
	if f.RefreshOnStart == nil {
		refresh := true
		f.RefreshOnStart = &refresh
	}
	if f.AutoReload == nil {
		autoReload := true
		f.AutoReload = &autoReload
	}
	if f.Workers <= 0 {
		f.Workers = DefaultFreeProxyCacheWorkers
	}
	if f.Workers > MaxFreeProxyCacheWorkers {
		f.Workers = MaxFreeProxyCacheWorkers
	}
	if f.MaxAge <= 0 {
		f.MaxAge = 6 * time.Hour
	}
	if strings.TrimSpace(f.Path) == "" && configPath != "" {
		f.Path = filepath.Join(filepath.Dir(configPath), ".cache", "free-proxies.txt")
	}
	return f
}

// QualityCheckConfig controls scheduled node quality checks.
type QualityCheckConfig struct {
	Enabled            bool          `yaml:"enabled"`             // 是否启用节点质量定时检测
	Interval           time.Duration `yaml:"interval"`            // 检测间隔，默认 24 小时
	Region             string        `yaml:"region"`              // 检测地区范围，默认 all
	Count              int           `yaml:"count"`               // 单次检测数量，默认 500
	IncludeUnavailable bool          `yaml:"include_unavailable"` // 是否包含不可用节点，默认 true
	RetryFailed        bool          `yaml:"retry_failed"`        // 是否优先重试失败节点，默认 false
}

func (q QualityCheckConfig) Normalized() QualityCheckConfig {
	if q == (QualityCheckConfig{}) {
		q.IncludeUnavailable = true
	}
	if q.Interval <= 0 {
		q.Interval = DefaultQualityCheckInterval
	}
	if q.Interval < MinQualityCheckInterval {
		q.Interval = MinQualityCheckInterval
	}
	q.Region = strings.ToLower(strings.TrimSpace(q.Region))
	if q.Region == "" {
		q.Region = DefaultQualityCheckRegion
	}
	if q.Count <= 0 {
		q.Count = DefaultQualityCheckCount
	}
	if q.Count > MaxQualityCheckCount {
		q.Count = MaxQualityCheckCount
	}
	return q
}

// NodeSource indicates where a node configuration originated from.
type NodeSource string

const (
	NodeSourceInline       NodeSource = "inline"       // Defined directly in config.yaml nodes array
	NodeSourceFile         NodeSource = "nodes_file"   // Loaded from external nodes file
	NodeSourceSubscription NodeSource = "subscription" // Fetched from subscription URL
	NodeSourceFreeProxy    NodeSource = "free_proxy"   // Loaded from configured free proxy source
)

// NodeConfig describes a single upstream proxy endpoint expressed as URI.
type NodeConfig struct {
	Name     string     `yaml:"name" json:"name"`
	URI      string     `yaml:"uri" json:"uri"`
	Port     uint16     `yaml:"port,omitempty" json:"port,omitempty"`
	Username string     `yaml:"username,omitempty" json:"username,omitempty"`
	Password string     `yaml:"password,omitempty" json:"password,omitempty"`
	Source   NodeSource `yaml:"-" json:"source,omitempty"` // Runtime only, not persisted
}

// NodeKey returns a unique identifier for the node based on its URI.
// This is used to preserve port assignments across reloads.
func (n *NodeConfig) NodeKey() string {
	return n.URI
}

// Load reads YAML config from disk and applies defaults/validation.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	cfg.filePath = path

	// Resolve local source paths relative to config file directory
	configDir := filepath.Dir(path)
	if cfg.NodesFile != "" && !filepath.IsAbs(cfg.NodesFile) {
		cfg.NodesFile = filepath.Join(configDir, cfg.NodesFile)
	}
	for idx := range cfg.FreeProxySources {
		if cfg.FreeProxySources[idx].File != "" && !filepath.IsAbs(cfg.FreeProxySources[idx].File) {
			cfg.FreeProxySources[idx].File = filepath.Join(configDir, cfg.FreeProxySources[idx].File)
		}
	}
	if cfg.FreeProxyCache.Path != "" && !filepath.IsAbs(cfg.FreeProxyCache.Path) {
		cfg.FreeProxyCache.Path = filepath.Join(configDir, cfg.FreeProxyCache.Path)
	}

	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func hasEnabledFreeProxySource(sources []nodesource.SourceConfig) bool {
	for _, source := range sources {
		if source.EnabledValue() {
			return true
		}
	}
	return false
}

func (c *Config) appendFreeProxyNodes() error {
	if c == nil || len(c.FreeProxySources) == 0 {
		return nil
	}
	if c.FreeProxyCache.EnabledValue() && strings.TrimSpace(c.FreeProxyCache.Path) != "" {
		return c.appendCachedFreeProxyNodes(c.FreeProxyCache.Path)
	}
	return c.appendRemoteFreeProxyNodes()
}

func (c *Config) appendCachedFreeProxyNodes(path string) error {
	cacheSource := nodesource.SourceConfig{
		Name:          "free-proxy-cache",
		File:          path,
		Format:        "txt",
		DefaultScheme: "http",
		MaxNodes:      c.FreeProxyMaxNodes,
	}
	provider := nodesource.NewProvider(cacheSource)
	sourceNodes, err := provider.LoadLimited(c.FreeProxyMaxNodes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("ℹ️ Free proxy cache %q does not exist yet; skipping remote free sources during startup", path)
			return nil
		}
		log.Printf("⚠️ Failed to load free proxy cache %q: %v (skipping)", path, err)
		return nil
	}
	added := c.appendFreeProxyNodeCandidates(sourceNodes, c.FreeProxyMaxNodes)
	if added > 0 {
		log.Printf("✅ Loaded %d/%d nodes from free proxy cache %q", added, len(sourceNodes), path)
	}
	return nil
}

func (c *Config) appendRemoteFreeProxyNodes() error {
	filter := c.FreeProxyFilter.Normalized()
	seen := c.freeProxySeenNodeURIs()

	totalAdded := 0
	for _, source := range c.FreeProxySources {
		if c.FreeProxyMaxNodes > 0 && totalAdded >= c.FreeProxyMaxNodes {
			break
		}
		provider := nodesource.NewProvider(source)
		remaining := 0
		if c.FreeProxyMaxNodes > 0 {
			remaining = c.FreeProxyMaxNodes - totalAdded
			if remaining <= 0 {
				break
			}
			// Fetch enough source entries to still fill the remaining global cap
			// when earlier inline/subscription nodes are de-duplicated out.
			remaining += len(seen)
		}
		sourceNodes, err := provider.LoadLimited(filter.LoadLimit(remaining))
		if err != nil {
			name := strings.TrimSpace(source.Name)
			if name == "" {
				name = firstNonEmptyString(source.File, source.URL, "unnamed")
			}
			log.Printf("⚠️ Failed to load free proxy source %q: %v (skipping)", name, err)
			continue
		}
		if filter.Enabled {
			before := len(sourceNodes)
			result := nodesource.FilterNodes(sourceNodes, filter)
			sourceNodes = result.Accepted
			if before > 0 {
				log.Printf("🔎 Free proxy source %q prefilter kept %d/%d nodes (min_tier=%s)", source.Name, len(sourceNodes), before, filter.MinTier)
			}
		}
		addedFromSource := c.appendFreeProxyNodeCandidatesWithSeen(sourceNodes, c.FreeProxyMaxNodes-totalAdded, seen)
		totalAdded += addedFromSource
		if addedFromSource > 0 {
			log.Printf("✅ Loaded %d/%d nodes from free proxy source %q", addedFromSource, len(sourceNodes), source.Name)
		}
	}
	return nil
}

func (c *Config) freeProxySeenNodeURIs() map[string]struct{} {
	seen := make(map[string]struct{}, len(c.Nodes))
	for _, node := range c.Nodes {
		if key := canonicalNodeURI(node.URI); key != "" {
			seen[key] = struct{}{}
		}
	}
	return seen
}

func (c *Config) appendFreeProxyNodeCandidates(sourceNodes []nodesource.Node, limit int) int {
	return c.appendFreeProxyNodeCandidatesWithSeen(sourceNodes, limit, c.freeProxySeenNodeURIs())
}

func (c *Config) appendFreeProxyNodeCandidatesWithSeen(sourceNodes []nodesource.Node, limit int, seen map[string]struct{}) int {
	if limit <= 0 {
		limit = len(sourceNodes)
	}
	added := 0
	for _, sourceNode := range sourceNodes {
		key := canonicalNodeURI(sourceNode.URI)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		if added >= limit {
			break
		}
		seen[key] = struct{}{}
		c.Nodes = append(c.Nodes, NodeConfig{
			Name:   sourceNode.Name,
			URI:    strings.TrimSpace(sourceNode.URI),
			Source: NodeSourceFreeProxy,
		})
		added++
	}
	return added
}

func canonicalNodeURI(uri string) string {
	return strings.ToLower(strings.TrimSpace(uri))
}

// ExtractNodeName extracts a human-readable name from a proxy URI.
// For standard URIs (vless://, ss://, trojan://), it extracts from the URL fragment (#name).
// For vmess:// URIs, it base64-decodes the payload and extracts the "ps" field.
func ExtractNodeName(uri string) string {
	uri = strings.TrimSpace(uri)

	// Handle vmess:// specially - it's base64-encoded JSON, not a standard URL
	if strings.HasPrefix(uri, "vmess://") {
		payload := strings.TrimPrefix(uri, "vmess://")
		// Remove any fragment that might be appended
		if idx := strings.Index(payload, "#"); idx != -1 {
			payload = payload[:idx]
		}
		payload = strings.TrimSpace(payload)
		// Try standard base64 first, then raw/URL-safe variants
		var decoded []byte
		var err error
		decoded, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(payload)
		}
		if err != nil {
			decoded, err = base64.RawURLEncoding.DecodeString(payload)
		}
		if err == nil {
			var vmess struct {
				PS string `json:"ps"`
			}
			if json.Unmarshal(decoded, &vmess) == nil && vmess.PS != "" {
				return strings.TrimSpace(vmess.PS)
			}
		}
		return ""
	}

	// For standard URIs, extract from URL fragment (#name)
	if idx := strings.LastIndex(uri, "#"); idx != -1 && idx < len(uri)-1 {
		fragment := uri[idx+1:]
		if decoded, err := url.QueryUnescape(fragment); err == nil && decoded != "" {
			return strings.TrimSpace(decoded)
		}
		return strings.TrimSpace(fragment)
	}

	return ""
}

func (c *Config) normalize() error {
	if c.Mode == "" {
		c.Mode = "pool"
	}
	// Normalize mode name: support both multi-port and multi_port
	if c.Mode == "multi_port" {
		c.Mode = "multi-port"
	}
	switch c.Mode {
	case "pool", "multi-port", "hybrid":
	default:
		return fmt.Errorf("unsupported mode %q (use 'pool', 'multi-port', or 'hybrid')", c.Mode)
	}
	if c.Listener.Address == "" {
		c.Listener.Address = "0.0.0.0"
	}
	if c.Listener.Port == 0 {
		c.Listener.Port = 2323
	}
	if c.Pool.Mode == "" {
		c.Pool.Mode = "sequential"
	}
	if c.Pool.FailureThreshold <= 0 {
		c.Pool.FailureThreshold = 3
	}
	if c.Pool.BlacklistDuration <= 0 {
		c.Pool.BlacklistDuration = 24 * time.Hour
	}
	if c.MultiPort.Address == "" {
		c.MultiPort.Address = "0.0.0.0"
	}
	if c.MultiPort.BasePort == 0 {
		c.MultiPort.BasePort = 24000
	}
	if c.AndroidProxy.BasePort == 0 {
		c.AndroidProxy.BasePort = 13001
	}
	if c.AndroidProxy.BasePort == 0 {
		c.AndroidProxy.BasePort = 13001
	}
	if c.Management.Listen == "" {
		c.Management.Listen = "127.0.0.1:9091"
	}
	if c.Management.ProbeTarget == "" {
		c.Management.ProbeTarget = "www.apple.com:80"
	}
	if c.Management.ClashAPIListen == "" {
		c.Management.ClashAPIListen = "127.0.0.1:9092"
	}
	if c.Management.Enabled == nil {
		defaultEnabled := true
		c.Management.Enabled = &defaultEnabled
	}

	if c.SubscriptionRefresh.Interval <= 0 {
		c.SubscriptionRefresh.Interval = 1 * time.Hour
	}
	c.QualityCheck = c.QualityCheck.Normalized()
	if c.SubscriptionRefresh.Timeout <= 0 {
		c.SubscriptionRefresh.Timeout = 30 * time.Second
	}
	if c.SubscriptionRefresh.HealthCheckTimeout <= 0 {
		c.SubscriptionRefresh.HealthCheckTimeout = 60 * time.Second
	}
	if c.SubscriptionRefresh.DrainTimeout <= 0 {
		c.SubscriptionRefresh.DrainTimeout = 30 * time.Second
	}
	if c.SubscriptionRefresh.MinAvailableNodes <= 0 {
		c.SubscriptionRefresh.MinAvailableNodes = 1
	}
	c.FreeProxyFilter = c.FreeProxyFilter.Normalized()
	c.FreeProxyCache = c.FreeProxyCache.Normalized(c.filePath, hasEnabledFreeProxySource(c.FreeProxySources))
	// Drop previously materialized runtime-only free proxy nodes before
	// recomposing sources. This makes repeated normalize/reload calls idempotent.
	baseNodes := c.Nodes[:0]
	for idx := range c.Nodes {
		if c.Nodes[idx].Source == NodeSourceFreeProxy {
			continue
		}
		if c.Nodes[idx].Source == "" {
			c.Nodes[idx].Source = NodeSourceInline
		}
		baseNodes = append(baseNodes, c.Nodes[idx])
	}
	c.Nodes = baseNodes

	// Load nodes from file if specified (but NOT if subscriptions exist - subscription takes priority)
	if c.NodesFile != "" && len(c.Subscriptions) == 0 {
		fileNodes, err := loadNodesFromFile(c.NodesFile)
		if err != nil {
			return fmt.Errorf("load nodes from file %q: %w", c.NodesFile, err)
		}
		for idx := range fileNodes {
			fileNodes[idx].Source = NodeSourceFile
		}
		c.Nodes = append(c.Nodes, fileNodes...)
	}

	// Load nodes from subscriptions (highest priority - writes to nodes.txt)
	if len(c.Subscriptions) > 0 {
		var subNodes []NodeConfig
		subTimeout := c.SubscriptionRefresh.Timeout
		for _, subURL := range c.Subscriptions {
			nodes, err := loadNodesFromSubscription(subURL, subTimeout)
			if err != nil {
				log.Printf("⚠️ Failed to load subscription %q: %v (skipping)", subURL, err)
				continue
			}
			log.Printf("✅ Loaded %d nodes from subscription", len(nodes))
			subNodes = append(subNodes, nodes...)
		}
		// Mark subscription nodes and write to nodes.txt
		for idx := range subNodes {
			subNodes[idx].Source = NodeSourceSubscription
		}
		if len(subNodes) > 0 {
			// Determine nodes.txt path
			nodesFilePath := c.NodesFile
			if nodesFilePath == "" {
				nodesFilePath = filepath.Join(filepath.Dir(c.filePath), "nodes.txt")
				c.NodesFile = nodesFilePath
			}
			// Write subscription nodes to nodes.txt
			if err := writeNodesToFile(nodesFilePath, subNodes); err != nil {
				log.Printf("⚠️ Failed to write nodes to %q: %v", nodesFilePath, err)
			} else {
				log.Printf("✅ Written %d subscription nodes to %s", len(subNodes), nodesFilePath)
			}
		}
		c.Nodes = append(c.Nodes, subNodes...)
		// Fallback: if all subscriptions failed, try loading cached nodes.txt
		if len(subNodes) == 0 && c.NodesFile != "" {
			cachedNodes, err := loadNodesFromFile(c.NodesFile)
			if err == nil && len(cachedNodes) > 0 {
				log.Printf("⚠️  All subscriptions failed, using %d cached nodes from %s", len(cachedNodes), c.NodesFile)
				for idx := range cachedNodes {
					cachedNodes[idx].Source = NodeSourceSubscription
				}
				c.Nodes = append(c.Nodes, cachedNodes...)
			}
		}
	}

	// Load nodes from configured free proxy sources after inline/file/subscription
	// nodes. These are bounded, deduplicated runtime-only inputs and are
	// intentionally not persisted back to nodes.txt or config.yaml.
	if err := c.appendFreeProxyNodes(); err != nil {
		return err
	}

	if len(c.Nodes) == 0 {
		return errors.New("config.nodes cannot be empty (configure nodes in config or use nodes_file)")
	}
	portCursor := c.MultiPort.BasePort
	for idx := range c.Nodes {
		c.Nodes[idx].Name = strings.TrimSpace(c.Nodes[idx].Name)
		c.Nodes[idx].URI = strings.TrimSpace(c.Nodes[idx].URI)

		if c.Nodes[idx].URI == "" {
			return fmt.Errorf("node %d is missing uri", idx)
		}

		// Auto-extract name from URI if not provided
		if c.Nodes[idx].Name == "" {
			c.Nodes[idx].Name = ExtractNodeName(c.Nodes[idx].URI)
		}
		// Fallback to default name if still empty
		if c.Nodes[idx].Name == "" {
			c.Nodes[idx].Name = fmt.Sprintf("node-%d", idx)
		}

		// Auto-assign port in multi-port/hybrid mode, skip occupied ports
		if c.Nodes[idx].Port == 0 && (c.Mode == "multi-port" || c.Mode == "hybrid") {
			for !IsPortAvailable(c.MultiPort.Address, portCursor) {
				log.Printf("⚠️  Port %d is in use, trying next port", portCursor)
				portCursor++
				if portCursor > 65535 {
					return fmt.Errorf("no available ports found starting from %d", c.MultiPort.BasePort)
				}
			}
			c.Nodes[idx].Port = portCursor
			portCursor++
		} else if c.Nodes[idx].Port == 0 {
			c.Nodes[idx].Port = portCursor
			portCursor++
		}

		if c.Mode == "multi-port" || c.Mode == "hybrid" {
			if c.Nodes[idx].Username == "" {
				c.Nodes[idx].Username = c.MultiPort.Username
				c.Nodes[idx].Password = c.MultiPort.Password
			}
		}
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}

	// Log config defaults
	c.normalizeLogConfig()

	// Auto-fix port conflicts in hybrid mode (pool port vs multi-port)
	if c.Mode == "hybrid" {
		poolPort := c.Listener.Port
		usedPorts := make(map[uint16]bool)
		usedPorts[poolPort] = true
		for idx := range c.Nodes {
			usedPorts[c.Nodes[idx].Port] = true
		}
		for idx := range c.Nodes {
			if c.Nodes[idx].Port == poolPort {
				// Find next available port
				newPort := c.Nodes[idx].Port + 1
				for usedPorts[newPort] || !IsPortAvailable(c.MultiPort.Address, newPort) {
					newPort++
					if newPort > 65535 {
						return fmt.Errorf("no available port for node %q after conflict with pool port %d", c.Nodes[idx].Name, poolPort)
					}
				}
				log.Printf("⚠️  Node %q port %d conflicts with pool port, reassigned to %d", c.Nodes[idx].Name, poolPort, newPort)
				usedPorts[newPort] = true
				c.Nodes[idx].Port = newPort
			}
		}
	}

	return nil
}

// BuildPortMap creates a mapping from node URI to port for existing nodes.
// This is used to preserve port assignments when reloading configuration.
func (c *Config) BuildPortMap() map[string]uint16 {
	portMap := make(map[string]uint16)
	for _, node := range c.Nodes {
		if node.Port > 0 {
			portMap[node.NodeKey()] = node.Port
		}
	}
	return portMap
}

// NormalizeWithPortMap applies defaults and validation, preserving port assignments
// for nodes that exist in the provided port map.
func (c *Config) NormalizeWithPortMap(portMap map[string]uint16) error {
	if c.Mode == "" {
		c.Mode = "pool"
	}
	if c.Mode == "multi_port" {
		c.Mode = "multi-port"
	}
	switch c.Mode {
	case "pool", "multi-port", "hybrid":
	default:
		return fmt.Errorf("unsupported mode %q (use 'pool', 'multi-port', or 'hybrid')", c.Mode)
	}
	if c.Listener.Address == "" {
		c.Listener.Address = "0.0.0.0"
	}
	if c.Listener.Port == 0 {
		c.Listener.Port = 2323
	}
	if c.Pool.Mode == "" {
		c.Pool.Mode = "sequential"
	}
	if c.Pool.FailureThreshold <= 0 {
		c.Pool.FailureThreshold = 3
	}
	if c.Pool.BlacklistDuration <= 0 {
		c.Pool.BlacklistDuration = 24 * time.Hour
	}
	if c.MultiPort.Address == "" {
		c.MultiPort.Address = "0.0.0.0"
	}
	if c.MultiPort.BasePort == 0 {
		c.MultiPort.BasePort = 24000
	}
	if c.Management.Listen == "" {
		c.Management.Listen = "127.0.0.1:9091"
	}
	if c.Management.ProbeTarget == "" {
		c.Management.ProbeTarget = "www.apple.com:80"
	}
	if c.Management.Enabled == nil {
		defaultEnabled := true
		c.Management.Enabled = &defaultEnabled
	}
	if c.SubscriptionRefresh.Interval <= 0 {
		c.SubscriptionRefresh.Interval = 1 * time.Hour
	}
	if c.SubscriptionRefresh.Timeout <= 0 {
		c.SubscriptionRefresh.Timeout = 30 * time.Second
	}
	if c.SubscriptionRefresh.HealthCheckTimeout <= 0 {
		c.SubscriptionRefresh.HealthCheckTimeout = 60 * time.Second
	}
	if c.SubscriptionRefresh.DrainTimeout <= 0 {
		c.SubscriptionRefresh.DrainTimeout = 30 * time.Second
	}
	if c.SubscriptionRefresh.MinAvailableNodes <= 0 {
		c.SubscriptionRefresh.MinAvailableNodes = 1
	}
	c.FreeProxyFilter = c.FreeProxyFilter.Normalized()
	c.FreeProxyCache = c.FreeProxyCache.Normalized(c.filePath, hasEnabledFreeProxySource(c.FreeProxySources))

	// Drop previously materialized runtime-only free proxy nodes before
	// recomposing sources. This makes repeated reload calls idempotent.
	baseNodes := c.Nodes[:0]
	for idx := range c.Nodes {
		if c.Nodes[idx].Source == NodeSourceFreeProxy {
			continue
		}
		if c.Nodes[idx].Source == "" {
			c.Nodes[idx].Source = NodeSourceInline
		}
		baseNodes = append(baseNodes, c.Nodes[idx])
	}
	c.Nodes = baseNodes
	if err := c.appendFreeProxyNodes(); err != nil {
		return err
	}

	if len(c.Nodes) == 0 {
		return errors.New("config.nodes cannot be empty")
	}

	// Build set of ports already assigned from portMap
	usedPorts := make(map[uint16]bool)
	if c.Mode == "hybrid" {
		usedPorts[c.Listener.Port] = true
	}

	// First pass: assign ports from portMap for existing nodes
	for idx := range c.Nodes {
		c.Nodes[idx].Name = strings.TrimSpace(c.Nodes[idx].Name)
		c.Nodes[idx].URI = strings.TrimSpace(c.Nodes[idx].URI)
		if c.Nodes[idx].URI == "" {
			return fmt.Errorf("node %d is missing uri", idx)
		}

		// Auto-extract name from URI if not provided
		if c.Nodes[idx].Name == "" {
			c.Nodes[idx].Name = ExtractNodeName(c.Nodes[idx].URI)
		}
		if c.Nodes[idx].Name == "" {
			c.Nodes[idx].Name = fmt.Sprintf("node-%d", idx)
		}

		// Check if this node has a preserved port from portMap
		if c.Mode == "multi-port" || c.Mode == "hybrid" {
			nodeKey := c.Nodes[idx].NodeKey()
			if existingPort, ok := portMap[nodeKey]; ok && existingPort > 0 {
				c.Nodes[idx].Port = existingPort
				usedPorts[existingPort] = true
				log.Printf("✅ Preserved port %d for node %q", existingPort, c.Nodes[idx].Name)
			}
		}
	}

	// Second pass: assign new ports for nodes without preserved ports
	portCursor := c.MultiPort.BasePort
	for idx := range c.Nodes {
		if c.Nodes[idx].Port == 0 && (c.Mode == "multi-port" || c.Mode == "hybrid") {
			// Find next available port that's not used
			for usedPorts[portCursor] || !IsPortAvailable(c.MultiPort.Address, portCursor) {
				portCursor++
				if portCursor > 65535 {
					return fmt.Errorf("no available ports found starting from %d", c.MultiPort.BasePort)
				}
			}
			c.Nodes[idx].Port = portCursor
			usedPorts[portCursor] = true
			log.Printf("📌 Assigned new port %d for node %q", portCursor, c.Nodes[idx].Name)
			portCursor++
		} else if c.Nodes[idx].Port == 0 {
			c.Nodes[idx].Port = portCursor
			portCursor++
		}

		// Apply default credentials
		if c.Mode == "multi-port" || c.Mode == "hybrid" {
			if c.Nodes[idx].Username == "" {
				c.Nodes[idx].Username = c.MultiPort.Username
				c.Nodes[idx].Password = c.MultiPort.Password
			}
		}
	}

	if c.LogLevel == "" {
		c.LogLevel = "info"
	}

	c.normalizeLogConfig()

	return nil
}

// normalizeLogConfig applies defaults to the log config.
func (c *Config) normalizeLogConfig() {
	if c.Log.Output == "" {
		c.Log.Output = "stdout"
	}
	if c.Log.File == "" {
		c.Log.File = "logs/easy_proxies.log"
	}
	// Resolve relative log file path against config dir
	if c.filePath != "" && !filepath.IsAbs(c.Log.File) {
		c.Log.File = filepath.Join(filepath.Dir(c.filePath), c.Log.File)
	}
	if c.Log.MaxSize <= 0 {
		c.Log.MaxSize = 50
	}
	if c.Log.MaxBackups <= 0 {
		c.Log.MaxBackups = 3
	}
	if c.Log.MaxAge <= 0 {
		c.Log.MaxAge = 7
	}
}

// ManagementEnabled reports whether the monitoring endpoint should run.
func (c *Config) ManagementEnabled() bool {
	if c.Management.Enabled == nil {
		return true
	}
	return *c.Management.Enabled
}

// loadNodesFromFile reads a nodes file where each line is a proxy URI
// Lines starting with # are comments, empty lines are ignored
func loadNodesFromFile(path string) ([]NodeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseNodesFromContent(string(data))
}

// loadNodesFromSubscription fetches and parses nodes from a subscription URL
// Supports multiple formats: base64 encoded, plain text, clash yaml, etc.
func loadNodesFromSubscription(subURL string, timeout time.Duration) ([]NodeConfig, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{
		Timeout: timeout,
	}

	req, err := http.NewRequest("GET", subURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Use clash-compatible User-Agent to get Clash YAML format from subscription servers
	// This ensures we receive structured YAML with all proxy types (AnyTLS, TUIC, etc.)
	// instead of base64-encoded content that may only contain basic SS nodes
	req.Header.Set("User-Agent", "clash-verge/v2.2.3")
	req.Header.Set("Accept", "*/*")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch subscription: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subscription returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	content := string(body)

	// Try to detect and parse different formats
	return parseSubscriptionContent(content)
}

// parseSubscriptionContent tries to parse subscription content in various formats (optimized)
func parseSubscriptionContent(content string) ([]NodeConfig, error) {
	content = strings.TrimSpace(content)

	// Quick check for YAML format (check first 16384 chars for "proxies:")
	sampleSize := 16384
	if len(content) < sampleSize {
		sampleSize = len(content)
	}
	if strings.Contains(content[:sampleSize], "proxies:") {
		return parseClashYAML(content)
	}

	// Check if it's base64 encoded (common for v2ray subscriptions)
	if isBase64(content) {
		decoded, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			// Try URL-safe base64
			decoded, err = base64.RawStdEncoding.DecodeString(content)
			if err != nil {
				// Not base64, try as plain text
				return parseSubscriptionNodesFromContent(content)
			}
		}
		content = string(decoded)
	}

	// Parse as plain text (one URI per line)
	return parseSubscriptionNodesFromContent(content)
}

// ParseSubscriptionContent parses subscription content in various formats (base64, plain text, Clash YAML).
// This is exported for use by the subscription manager.
func ParseSubscriptionContent(content string) ([]NodeConfig, error) {
	return parseSubscriptionContent(content)
}

func parseNodesFromContent(content string) ([]NodeConfig, error) {
	return parseNodesFromContentWith(content, IsProxyURI)
}

func plainTextSubscriptionProxyURI(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return false
	}
	return IsProxyURI(line)
}

func parseSubscriptionNodesFromContent(content string) ([]NodeConfig, error) {
	return parseNodesFromContentWith(content, plainTextSubscriptionProxyURI)
}

// parseNodesFromContent parses nodes from plain text content (one URI per line)
func parseNodesFromContentWith(content string, validURI func(string) bool) ([]NodeConfig, error) {
	var nodes []NodeConfig
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Check if it's a valid proxy URI
		if validURI(line) {
			nodes = append(nodes, NodeConfig{
				URI: line,
			})
		}
	}

	return nodes, nil
}

// isBase64 checks if a string looks like base64 encoded content (optimized version)
func isBase64(s string) bool {
	// Remove whitespace
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return false
	}

	// Remove newlines for checking
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")

	// Quick check: if it contains proxy URI schemes, it's not base64
	if strings.Contains(s, "://") {
		return false
	}

	// Check character set - base64 only contains A-Za-z0-9+/=
	// This is much faster than trying to decode
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=') {
			return false
		}
	}

	// Length must be multiple of 4 (with padding)
	return len(s)%4 == 0
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// IsProxyURI checks if a string is a valid proxy URI
func IsProxyURI(s string) bool {
	schemes := []string{"vmess://", "vless://", "trojan://", "ss://", "ssr://", "hysteria://", "hysteria2://", "hy2://", "tuic://", "socks5://", "socks://", "http://", "https://", "anytls://"}
	lower := strings.ToLower(s)
	for _, scheme := range schemes {
		if strings.HasPrefix(lower, scheme) {
			return true
		}
	}
	return false
}

// clashConfig represents a minimal Clash configuration for parsing proxies
// flexInt handles YAML values that may be either int or quoted string.
type flexInt int

func (fi *flexInt) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var intVal int
	if err := unmarshal(&intVal); err == nil {
		*fi = flexInt(intVal)
		return nil
	}
	var strVal string
	if err := unmarshal(&strVal); err != nil {
		return fmt.Errorf("cannot unmarshal port: expected int or string")
	}
	parsed, err := strconv.Atoi(strVal)
	if err != nil {
		return fmt.Errorf("cannot parse port %q as int: %w", strVal, err)
	}
	*fi = flexInt(parsed)
	return nil
}

type clashConfig struct {
	Proxies []clashProxy `yaml:"proxies"`
}

type clashProxy struct {
	Name              string                 `yaml:"name"`
	Type              string                 `yaml:"type"`
	Server            string                 `yaml:"server"`
	Port              flexInt                `yaml:"port"`
	Ports             string                 `yaml:"ports"`
	UUID              string                 `yaml:"uuid"`
	Password          string                 `yaml:"password"`
	Cipher            string                 `yaml:"cipher"`
	AlterId           int                    `yaml:"alterId"`
	Network           string                 `yaml:"network"`
	TLS               bool                   `yaml:"tls"`
	SkipCertVerify    bool                   `yaml:"skip-cert-verify"`
	ServerName        string                 `yaml:"servername"`
	SNI               string                 `yaml:"sni"`
	Flow              string                 `yaml:"flow"`
	UDP               bool                   `yaml:"udp"`
	WSOpts            *clashWSOptions        `yaml:"ws-opts"`
	GrpcOpts          *clashGrpcOptions      `yaml:"grpc-opts"`
	RealityOpts       *clashRealityOptions   `yaml:"reality-opts"`
	ClientFingerprint string                 `yaml:"client-fingerprint"`
	Obfs              string                 `yaml:"obfs"`
	ObfsPassword      string                 `yaml:"obfs-password"`
	Plugin            string                 `yaml:"plugin"`
	PluginOpts        map[string]interface{} `yaml:"plugin-opts"`
	// TUIC-specific fields
	ALPN                 []string `yaml:"alpn"`
	CongestionController string   `yaml:"congestion-controller"`
	UDPRelayMode         string   `yaml:"udp-relay-mode"`
}

type clashWSOptions struct {
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers"`
}

type clashGrpcOptions struct {
	GrpcServiceName string `yaml:"grpc-service-name"`
}

type clashRealityOptions struct {
	PublicKey string `yaml:"public-key"`
	ShortID   string `yaml:"short-id"`
}

// parseClashYAML parses Clash YAML format and converts to NodeConfig
func parseClashYAML(content string) ([]NodeConfig, error) {
	var clash clashConfig
	if err := yaml.Unmarshal([]byte(content), &clash); err != nil {
		return nil, fmt.Errorf("parse clash yaml: %w", err)
	}

	var nodes []NodeConfig
	for _, proxy := range clash.Proxies {
		uri := convertClashProxyToURI(proxy)
		if uri != "" {
			nodes = append(nodes, NodeConfig{
				Name: proxy.Name,
				URI:  uri,
			})
		}
	}

	return nodes, nil
}

// convertClashProxyToURI converts a Clash proxy config to a standard URI
func convertClashProxyToURI(p clashProxy) string {
	switch strings.ToLower(p.Type) {
	case "vmess":
		return buildVMessURI(p)
	case "vless":
		return buildVLESSURI(p)
	case "trojan":
		return buildTrojanURI(p)
	case "anytls":
		return buildAnyTLSURI(p)
	case "ss", "shadowsocks":
		return buildShadowsocksURI(p)
	case "hysteria2", "hy2":
		return buildHysteria2URI(p)
	case "tuic":
		return buildTUICURI(p)
	default:
		return ""
	}
}

func buildVMessURI(p clashProxy) string {
	params := url.Values{}
	if p.Network != "" && p.Network != "tcp" {
		params.Set("type", p.Network)
	}
	if p.TLS {
		params.Set("security", "tls")
		if p.ServerName != "" {
			params.Set("sni", p.ServerName)
		} else if p.SNI != "" {
			params.Set("sni", p.SNI)
		}
	}
	if p.WSOpts != nil {
		if p.WSOpts.Path != "" {
			params.Set("path", p.WSOpts.Path)
		}
		if host, ok := p.WSOpts.Headers["Host"]; ok {
			params.Set("host", host)
		}
	}
	if p.ClientFingerprint != "" {
		params.Set("fp", p.ClientFingerprint)
	}

	query := ""
	if len(params) > 0 {
		query = "?" + params.Encode()
	}

	return fmt.Sprintf("vmess://%s@%s:%d%s#%s", p.UUID, p.Server, int(p.Port), query, url.QueryEscape(p.Name))
}

func buildVLESSURI(p clashProxy) string {
	params := url.Values{}
	params.Set("encryption", "none")

	if p.Network != "" && p.Network != "tcp" {
		params.Set("type", p.Network)
	}
	if p.Flow != "" {
		params.Set("flow", p.Flow)
	}
	if p.TLS {
		params.Set("security", "tls")
		if p.ServerName != "" {
			params.Set("sni", p.ServerName)
		} else if p.SNI != "" {
			params.Set("sni", p.SNI)
		}
	}
	if p.RealityOpts != nil {
		params.Set("security", "reality")
		if p.RealityOpts.PublicKey != "" {
			params.Set("pbk", p.RealityOpts.PublicKey)
		}
		if p.RealityOpts.ShortID != "" {
			params.Set("sid", p.RealityOpts.ShortID)
		}
		if p.ServerName != "" {
			params.Set("sni", p.ServerName)
		}
	}
	if p.WSOpts != nil {
		if p.WSOpts.Path != "" {
			params.Set("path", p.WSOpts.Path)
		}
		if host, ok := p.WSOpts.Headers["Host"]; ok {
			params.Set("host", host)
		}
	}
	if p.GrpcOpts != nil && p.GrpcOpts.GrpcServiceName != "" {
		params.Set("serviceName", p.GrpcOpts.GrpcServiceName)
	}
	if p.ClientFingerprint != "" {
		params.Set("fp", p.ClientFingerprint)
	}

	return fmt.Sprintf("vless://%s@%s:%d?%s#%s", p.UUID, p.Server, int(p.Port), params.Encode(), url.QueryEscape(p.Name))
}

func buildTrojanURI(p clashProxy) string {
	params := url.Values{}
	if p.ServerName != "" {
		params.Set("sni", p.ServerName)
	} else if p.SNI != "" {
		params.Set("sni", p.SNI)
	}
	if p.SkipCertVerify {
		params.Set("allowInsecure", "1")
	}
	if p.Network != "" && p.Network != "tcp" {
		params.Set("type", p.Network)
	}
	if p.WSOpts != nil {
		if p.WSOpts.Path != "" {
			params.Set("path", p.WSOpts.Path)
		}
		if host, ok := p.WSOpts.Headers["Host"]; ok {
			params.Set("host", host)
		}
	}
	if p.ClientFingerprint != "" {
		params.Set("fp", p.ClientFingerprint)
	}

	query := ""
	if len(params) > 0 {
		query = "?" + params.Encode()
	}

	return fmt.Sprintf("trojan://%s@%s:%d%s#%s", p.Password, p.Server, int(p.Port), query, url.QueryEscape(p.Name))
}

func buildAnyTLSURI(p clashProxy) string {
	params := url.Values{}
	if p.ServerName != "" {
		params.Set("sni", p.ServerName)
	} else if p.SNI != "" {
		params.Set("sni", p.SNI)
	}
	if p.SkipCertVerify {
		params.Set("allowInsecure", "1")
	}
	if p.ClientFingerprint != "" {
		params.Set("fp", p.ClientFingerprint)
	}

	query := ""
	if len(params) > 0 {
		query = "?" + params.Encode()
	}

	return fmt.Sprintf("anytls://%s@%s:%d%s#%s", p.Password, p.Server, int(p.Port), query, url.QueryEscape(p.Name))
}

func buildShadowsocksURI(p clashProxy) string {
	// Encode method:password in base64
	userInfo := base64.StdEncoding.EncodeToString([]byte(p.Cipher + ":" + p.Password))
	return fmt.Sprintf("ss://%s@%s:%d#%s", userInfo, p.Server, int(p.Port), url.QueryEscape(p.Name))
}

func buildHysteria2URI(p clashProxy) string {
	params := url.Values{}
	if p.ServerName != "" {
		params.Set("sni", p.ServerName)
	} else if p.SNI != "" {
		params.Set("sni", p.SNI)
	}
	if p.SkipCertVerify {
		params.Set("insecure", "1")
	}
	if p.Obfs != "" {
		params.Set("obfs", p.Obfs)
		if p.ObfsPassword != "" {
			params.Set("obfs-password", p.ObfsPassword)
		}
	}
	if strings.TrimSpace(p.Ports) != "" {
		params.Set("ports", normalizeHysteria2PortsValue(strings.TrimSpace(p.Ports)))
	}

	query := ""
	if len(params) > 0 {
		query = "?" + params.Encode()
	}

	port := int(p.Port)
	if port <= 0 {
		port = 443
	}

	return fmt.Sprintf("hysteria2://%s@%s:%d%s#%s", p.Password, p.Server, port, query, url.QueryEscape(p.Name))
}

func normalizeHysteria2PortsValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	parts := strings.Split(value, ",")
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, ":") {
			normalized = append(normalized, part)
			continue
		}
		if strings.Count(part, "-") == 1 {
			normalized = append(normalized, strings.Replace(part, "-", ":", 1))
			continue
		}
		normalized = append(normalized, part)
	}

	return strings.Join(normalized, ",")
}

func buildTUICURI(p clashProxy) string {
	params := url.Values{}
	if p.ServerName != "" {
		params.Set("sni", p.ServerName)
	} else if p.SNI != "" {
		params.Set("sni", p.SNI)
	}
	if p.SkipCertVerify {
		params.Set("allowInsecure", "1")
	}
	if p.CongestionController != "" {
		params.Set("congestion_control", p.CongestionController)
	}
	if p.UDPRelayMode != "" {
		params.Set("udp_relay_mode", p.UDPRelayMode)
	}
	if len(p.ALPN) > 0 {
		params.Set("alpn", strings.Join(p.ALPN, ","))
	}

	query := ""
	if len(params) > 0 {
		query = "?" + params.Encode()
	}

	// TUIC URI format: tuic://uuid:password@server:port?params#name
	return fmt.Sprintf("tuic://%s:%s@%s:%d%s#%s", p.UUID, p.Password, p.Server, int(p.Port), query, url.QueryEscape(p.Name))
}

// FilePath returns the config file path.
func (c *Config) FilePath() string {
	if c == nil {
		return ""
	}
	return c.filePath
}

// SetFilePath sets the config file path (used when creating config programmatically).
func (c *Config) SetFilePath(path string) {
	if c != nil {
		c.filePath = path
	}
}

// writeNodesToFile writes nodes to a file (one URI per line) with file locking.
func writeNodesToFile(path string, nodes []NodeConfig) error {
	var lines []string
	for _, node := range nodes {
		lines = append(lines, node.URI)
	}
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	// Use file locking for safe concurrent writes
	return writeFileWithLock(path, []byte(content), 0o644)
}

// SaveNodes persists nodes to their appropriate locations based on source.
// - subscription/nodes_file nodes → nodes.txt (or configured nodes_file)
// - inline nodes → config.yaml nodes array
// Config.yaml structure (subscriptions, nodes_file) is preserved.
func (c *Config) SaveNodes() error {
	if c == nil {
		return errors.New("config is nil")
	}
	if c.filePath == "" {
		return errors.New("config file path is unknown")
	}

	// Separate nodes by source
	var inlineNodes []NodeConfig
	var fileNodes []NodeConfig

	for _, node := range c.Nodes {
		// Create a clean copy without runtime fields for saving
		cleanNode := NodeConfig{
			Name:     node.Name,
			URI:      node.URI,
			Port:     node.Port,
			Username: node.Username,
			Password: node.Password,
		}
		switch node.Source {
		case NodeSourceInline:
			inlineNodes = append(inlineNodes, cleanNode)
		case NodeSourceFile, NodeSourceSubscription:
			fileNodes = append(fileNodes, cleanNode)
		case NodeSourceFreeProxy:
			// Free proxy source nodes are loaded from external sources at runtime.
			// Do not persist them into config.yaml or nodes.txt.
			continue
		default:
			// Default to file nodes for unknown source
			fileNodes = append(fileNodes, cleanNode)
		}
	}

	// Write file-based nodes to nodes.txt
	if len(fileNodes) > 0 || c.NodesFile != "" {
		nodesFilePath := c.NodesFile
		if nodesFilePath == "" {
			nodesFilePath = filepath.Join(filepath.Dir(c.filePath), "nodes.txt")
		}
		if err := writeNodesToFile(nodesFilePath, fileNodes); err != nil {
			return fmt.Errorf("write nodes file %q: %w", nodesFilePath, err)
		}
	}

	// Update config.yaml nodes array (including clearing it when all inline nodes are deleted)
	{
		// Read original config to preserve structure
		data, err := os.ReadFile(c.filePath)
		if err != nil {
			return fmt.Errorf("read config: %w", err)
		}
		var saveCfg Config
		if err := yaml.Unmarshal(data, &saveCfg); err != nil {
			return fmt.Errorf("decode config: %w", err)
		}
		// Update only the inline nodes
		saveCfg.Nodes = inlineNodes

		newData, err := yaml.Marshal(&saveCfg)
		if err != nil {
			return fmt.Errorf("encode config: %w", err)
		}
		// Use file locking for safe concurrent writes
		if err := writeFileWithLock(c.filePath, newData, 0o644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
	}

	return nil
}

// Save is deprecated, use SaveNodes instead.
// This method is kept for backward compatibility but now delegates to SaveNodes.
func (c *Config) Save() error {
	return c.SaveNodes()
}

// SaveSettings persists only config settings (external_ip, probe_target, skip_cert_verify)
// without touching nodes.txt. Use this for settings API updates.
func (c *Config) SaveSettings() error {
	if c == nil {
		return errors.New("config is nil")
	}
	if c.filePath == "" {
		return errors.New("config file path is unknown")
	}

	data, err := os.ReadFile(c.filePath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var saveCfg Config
	if err := yaml.Unmarshal(data, &saveCfg); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}

	saveCfg.ExternalIP = c.ExternalIP
	saveCfg.Management.ProbeTarget = c.Management.ProbeTarget
	saveCfg.SkipCertVerify = c.SkipCertVerify
	saveCfg.Log = c.Log
	saveCfg.Subscriptions = c.Subscriptions
	saveCfg.SubscriptionRefresh = c.SubscriptionRefresh
	saveCfg.QualityCheck = c.QualityCheck
	saveCfg.FreeProxySources = c.FreeProxySources
	saveCfg.FreeProxyMaxNodes = c.FreeProxyMaxNodes
	saveCfg.FreeProxyFilter = c.FreeProxyFilter
	saveCfg.FreeProxyCache = c.FreeProxyCache
	saveCfg.GeoIP = c.GeoIP
	saveCfg.Mode = c.Mode
	saveCfg.Listener = c.Listener
	saveCfg.MultiPort = c.MultiPort
	saveCfg.Pool = c.Pool
	saveCfg.Management = c.Management

	newData, err := yaml.Marshal(&saveCfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	// Use file locking for safe concurrent writes
	if err := writeFileWithLock(c.filePath, newData, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// IsPortAvailable checks if a port is available for binding.
func IsPortAvailable(address string, port uint16) bool {
	addr := fmt.Sprintf("%s:%d", address, port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// writeFileWithLock writes data to a file with exclusive locking.
func writeFileWithLock(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	// Acquire exclusive lock
	if err := lockFile(f); err != nil {
		return fmt.Errorf("lock file: %w", err)
	}
	defer unlockFile(f)

	// Write data
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	// Ensure data is written to disk
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync file: %w", err)
	}

	return nil
}

// RefreshFreeProxyCache downloads configured free-proxy sources, applies the
// optional prefilter, and atomically writes accepted proxy URIs into the cache.
// Runtime startup/reload can then use the cache without blocking on network IO.
func (c *Config) RefreshFreeProxyCache(ctx context.Context) (int, error) {
	count, _, err := c.RefreshFreeProxyCacheDetailed(ctx)
	return count, err
}

// FreeProxySourceRefreshResult records one source refresh outcome.
type FreeProxySourceRefreshResult struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Candidates int    `json:"candidates"`
	Accepted   int    `json:"accepted"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type freeProxySourceRefreshInternalResult struct {
	idx        int
	name       string
	candidates int
	nodes      []nodesource.Node
	durationMS int64
	err        error
}

// RefreshFreeProxyCacheDetailed is RefreshFreeProxyCache with per-source
// telemetry suitable for API/UI status reporting.
func (c *Config) RefreshFreeProxyCacheDetailed(ctx context.Context) (int, []FreeProxySourceRefreshResult, error) {
	if c == nil {
		return 0, nil, errors.New("config is nil")
	}
	cache := c.FreeProxyCache.Normalized(c.filePath, hasEnabledFreeProxySource(c.FreeProxySources))
	if !cache.EnabledValue() || strings.TrimSpace(cache.Path) == "" || len(c.FreeProxySources) == 0 {
		return 0, nil, nil
	}

	filter := c.FreeProxyFilter.Normalized()
	workers := cache.Workers
	if workers <= 0 {
		workers = 1
	}
	if workers > len(c.FreeProxySources) {
		workers = len(c.FreeProxySources)
	}
	if workers <= 0 {
		return 0, nil, nil
	}

	jobs := make(chan int)
	results := make(chan freeProxySourceRefreshInternalResult, len(c.FreeProxySources))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				select {
				case <-ctx.Done():
					results <- freeProxySourceRefreshInternalResult{idx: idx, err: ctx.Err()}
					continue
				default:
				}
				source := c.FreeProxySources[idx]
				started := time.Now()
				name := strings.TrimSpace(source.Name)
				if name == "" {
					name = firstNonEmptyString(source.File, source.URL, "unnamed")
				}
				provider := nodesource.NewProvider(source)
				// SourceConfig.MaxNodes is an explicit per-source parse cap.
				// When it is unset/0, load the whole source into the background
				// candidate pipeline; only the final cache/runtime activation is
				// bounded by free_proxy_max_nodes.
				nodes, err := provider.LoadLimited(source.MaxNodes)
				candidates := len(nodes)
				if err == nil && filter.Enabled {
					before := len(nodes)
					filtered := nodesource.FilterNodes(nodes, filter)
					nodes = filtered.Accepted
					if before > 0 {
						log.Printf("🔎 Free proxy source %q background prefilter kept %d/%d nodes (min_tier=%s)", name, len(nodes), before, filter.MinTier)
					}
				}
				results <- freeProxySourceRefreshInternalResult{idx: idx, name: name, candidates: candidates, nodes: nodes, durationMS: time.Since(started).Milliseconds(), err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for idx := range c.FreeProxySources {
			select {
			case <-ctx.Done():
				return
			case jobs <- idx:
			}
		}
	}()
	wg.Wait()
	close(results)

	byIndex := make(map[int]freeProxySourceRefreshInternalResult, len(c.FreeProxySources))
	for result := range results {
		byIndex[result.idx] = result
		if result.err != nil && !errors.Is(result.err, context.Canceled) {
			log.Printf("⚠️ Failed to refresh free proxy source %q: %v", result.name, result.err)
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, sourceRefreshResults(c.FreeProxySources, byIndex), err
	}

	seen := make(map[string]struct{})
	accepted := make([]string, 0)
	limit := c.FreeProxyMaxNodes
	if limit <= 0 {
		limit = 1 << 30
	}
	for idx := range c.FreeProxySources {
		result := byIndex[idx]
		for _, node := range result.nodes {
			key := canonicalNodeURI(node.URI)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			accepted = append(accepted, strings.TrimSpace(node.URI))
			if len(accepted) >= limit {
				break
			}
		}
		if len(accepted) >= limit {
			break
		}
	}
	if err := os.MkdirAll(filepath.Dir(cache.Path), 0o755); err != nil {
		return 0, sourceRefreshResults(c.FreeProxySources, byIndex), fmt.Errorf("create free proxy cache dir: %w", err)
	}
	if len(accepted) == 0 {
		if err := writeFileWithLock(cache.Path, nil, 0o644); err != nil {
			return 0, sourceRefreshResults(c.FreeProxySources, byIndex), fmt.Errorf("clear stale free proxy cache: %w", err)
		}
		return 0, sourceRefreshResults(c.FreeProxySources, byIndex), errors.New("no free proxy candidates accepted; stale cache cleared")
	}
	content := strings.Join(accepted, "\n") + "\n"
	if err := writeFileWithLock(cache.Path, []byte(content), 0o644); err != nil {
		return 0, sourceRefreshResults(c.FreeProxySources, byIndex), fmt.Errorf("write free proxy cache: %w", err)
	}
	log.Printf("✅ Refreshed free proxy cache %q with %d accepted nodes", cache.Path, len(accepted))
	return len(accepted), sourceRefreshResults(c.FreeProxySources, byIndex), nil
}

func sourceRefreshResults(sources []nodesource.SourceConfig, byIndex map[int]freeProxySourceRefreshInternalResult) []FreeProxySourceRefreshResult {
	out := make([]FreeProxySourceRefreshResult, 0, len(sources))
	for idx, source := range sources {
		name := strings.TrimSpace(source.Name)
		if name == "" {
			name = firstNonEmptyString(source.File, source.URL, "unnamed")
		}
		result, ok := byIndex[idx]
		item := FreeProxySourceRefreshResult{Name: name, Enabled: source.EnabledValue()}
		if ok {
			item.Candidates = result.candidates
			item.Accepted = len(result.nodes)
			item.DurationMS = result.durationMS
			if result.err != nil {
				item.Error = result.err.Error()
			}
		}
		out = append(out, item)
	}
	return out
}
