package freepromote

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"easy_proxies/internal/cloudflarecheck"
	"easy_proxies/internal/config"
	"easy_proxies/internal/geoip"
)

// NodeManager is the subset of boxmgr used by free-proxy promotion.
type NodeManager interface {
	ListConfigNodes(ctx context.Context) ([]config.NodeConfig, error)
	CreateNode(ctx context.Context, node config.NodeConfig) (config.NodeConfig, error)
	DeleteNode(ctx context.Context, name string) error
	TriggerReload(ctx context.Context) error
	// UpdateNodeRegion updates runtime monitor region metadata for a node tag/name.
	UpdateNodeRegion(ctx context.Context, name, region, country string) error
	// PersistRegionOverride stores a durable URI->region override (manual_region_overrides).
	PersistRegionOverride(uri, region string) error
}

// SnapshotSource exposes runtime health snapshots.
type SnapshotSource interface {
	ListSnapshots() []Snapshot
}

// QualityResult is the outcome of a post-promotion quality check.
type QualityResult struct {
	Score   int
	OK      bool
	Error   string
	Skipped bool
	ExitIP  string
	CFLoc   string
}

// QualityChecker validates a promoted multi-port listener.
type QualityChecker interface {
	Check(ctx context.Context, proxyURL, nodeName string) QualityResult
}

// CloudflareChecker adapts cloudflarecheck.Checker to QualityChecker.
type CloudflareChecker struct {
	Checker *cloudflarecheck.Checker
}

func (c CloudflareChecker) Check(ctx context.Context, proxyURL, nodeName string) QualityResult {
	if c.Checker == nil {
		return QualityResult{Error: "cloudflare checker is nil", OK: false}
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return QualityResult{Error: "invalid proxy url", OK: false}
	}
	host := parsed.Hostname()
	port := uint16(0)
	if p := parsed.Port(); p != "" {
		var n int
		fmt.Sscanf(p, "%d", &n)
		if n > 0 && n <= 65535 {
			port = uint16(n)
		}
	}
	res := c.Checker.CheckTarget(ctx, cloudflarecheck.ProxyTarget{
		NodeName: nodeName,
		NodeTag:  nodeName,
		Host:     host,
		Port:     port,
		ProxyURL: proxyURL,
	})
	ok := res.Error == "" && res.Level != "failed"
	return QualityResult{
		Score:  res.Score,
		OK:     ok,
		Error:  res.Error,
		ExitIP: res.ExitIP,
		CFLoc:  res.CFLoc,
	}
}

// ListenAuth supplies multi-port credentials and listen host for quality checks.
type ListenAuth struct {
	Host     string
	Username string
	Password string
}

const defaultDisabledPollInterval = 30 * time.Second

// Service periodically promotes healthy free_proxy nodes to dedicated ports.
type Service struct {
	cfg          func() config.FreeProxyPromoteConfig
	nodes        NodeManager
	snaps        SnapshotSource
	quality      QualityChecker
	listen       func() ListenAuth
	logger       *log.Logger
	now          func() time.Time
	settleWait   time.Duration
	startupWait  time.Duration
	disabledPoll time.Duration
	mu           sync.Mutex
	running      bool
}

// NewService constructs a free-proxy promote service.
func NewService(
	cfg func() config.FreeProxyPromoteConfig,
	nodes NodeManager,
	snaps SnapshotSource,
	quality QualityChecker,
	listen func() ListenAuth,
	logger *log.Logger,
) *Service {
	if logger == nil {
		logger = log.New(os.Stdout, "[free-proxy-promote] ", log.LstdFlags|log.Lmsgprefix)
	}
	if cfg == nil {
		cfg = func() config.FreeProxyPromoteConfig { return config.FreeProxyPromoteConfig{} }
	}
	if listen == nil {
		listen = func() ListenAuth { return ListenAuth{Host: "127.0.0.1"} }
	}
	return &Service{
		cfg:          cfg,
		nodes:        nodes,
		snaps:        snaps,
		quality:      quality,
		listen:       listen,
		logger:       logger,
		now:          time.Now,
		settleWait:   5 * time.Second,
		startupWait:  15 * time.Second,
		disabledPoll: defaultDisabledPollInterval,
	}
}

// Start launches the background promote loop. It stays alive while disabled so runtime settings can enable it.
func (s *Service) Start(ctx context.Context) {
	if s == nil || s.nodes == nil || s.snaps == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	go s.loop(ctx)
}

func (s *Service) loop(ctx context.Context) {
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	// Small startup delay so health probes can finish first.
	if !sleepContext(ctx, maxDuration(s.startupWait, 0)) {
		return
	}

	enabled := false
	for {
		cfg := s.cfg().Normalized()
		if !cfg.Enabled {
			if enabled {
				s.logger.Printf("disabled, pausing loop")
				enabled = false
			}
			if !sleepContext(ctx, s.disabledPollInterval()) {
				return
			}
			continue
		}
		if !enabled {
			s.logger.Printf("enabled: interval=%s batch=%d max=%d cf=%v",
				cfg.Interval, cfg.BatchSize, cfg.MaxPromoted, cfg.RequireCloudflare)
			enabled = true
		}
		if err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
			s.logger.Printf("cycle error: %v", err)
		}
		if !sleepContext(ctx, cfg.Interval) {
			return
		}
	}
}

func (s *Service) disabledPollInterval() time.Duration {
	if s == nil || s.disabledPoll <= 0 {
		return defaultDisabledPollInterval
	}
	return s.disabledPoll
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if d <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// RunOnce executes a single promote cycle (select → promote → reload → quality → demote).
func (s *Service) RunOnce(ctx context.Context) error {
	if s == nil || s.nodes == nil || s.snaps == nil {
		return nil
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	cfg := s.cfg().Normalized()
	if !cfg.Enabled {
		return nil
	}

	nodes, err := s.nodes.ListConfigNodes(ctx)
	if err != nil {
		return fmt.Errorf("list config nodes: %w", err)
	}
	candidates := SelectCandidates(s.snaps.ListSnapshots(), nodes, cfg)
	if len(candidates) == 0 {
		return nil
	}

	s.logger.Printf("promoting %d candidate(s)", len(candidates))
	promotedNames := make([]string, 0, len(candidates))
	candidateByName := make(map[string]Candidate, len(candidates))
	for _, cand := range candidates {
		name := PromotedNodeName(cfg.NamePrefix, cand.URI)
		uri := strings.TrimSpace(cand.URI)
		// Embed promote name in URI fragment so nodes_file persistence keeps it.
		if config.ExtractNodeName(uri) == "" {
			uri = uri + "#" + url.QueryEscape(name)
		}
		created, err := s.nodes.CreateNode(ctx, config.NodeConfig{
			Name: name,
			URI:  uri,
		})
		if err != nil {
			s.logger.Printf("create %s failed: %v", name, err)
			continue
		}
		// Drop runtime free_proxy twin so reload does not keep a duplicate URI.
		if freeName := findFreeProxyName(nodes, cand.URI); freeName != "" && freeName != created.Name {
			if err := s.nodes.DeleteNode(ctx, freeName); err != nil {
				s.logger.Printf("remove free twin %s: %v", freeName, err)
			}
		}
		promotedNames = append(promotedNames, created.Name)
		candidateByName[created.Name] = cand
		s.logger.Printf("created promoted node %s (from free %s, latency=%dms success=%d)",
			created.Name, cand.Name, cand.LastLatencyMs, cand.SuccessCount)
	}
	if len(promotedNames) == 0 {
		return nil
	}

	if err := s.nodes.TriggerReload(ctx); err != nil {
		return fmt.Errorf("reload after promote: %w", err)
	}

	// Allow multi-port listeners and health registration to settle.
	wait := s.settleWait
	if wait < 0 {
		wait = 0
	}
	if wait > 0 {
		settle := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			settle.Stop()
			return ctx.Err()
		case <-settle.C:
		}
	}

	nodes, err = s.nodes.ListConfigNodes(ctx)
	if err != nil {
		return fmt.Errorf("list nodes after reload: %w", err)
	}
	byName := make(map[string]config.NodeConfig, len(nodes))
	for _, n := range nodes {
		byName[n.Name] = n
	}

	auth := s.listen()
	host := strings.TrimSpace(auth.Host)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}

	for _, name := range promotedNames {
		node, ok := byName[name]
		if !ok {
			s.logger.Printf("promoted node %s missing after reload", name)
			continue
		}
		cand := candidateByName[name]

		// Prefer exit-region from quality probe when available; otherwise inherit free-node region.
		var qr QualityResult
		haveQR := false
		if s.quality != nil && node.Port != 0 {
			user := node.Username
			pass := node.Password
			if user == "" {
				user = auth.Username
				pass = auth.Password
			}
			proxyURL := buildSocksURL(host, node.Port, user, pass)
			qr = s.quality.Check(ctx, proxyURL, name)
			haveQR = !qr.Skipped
			if cfg.RequireCloudflare && haveQR {
				if node.Port == 0 {
					// unreachable due to check above
				} else if !qr.OK || qr.Score < cfg.MinCloudflareScore {
					s.logger.Printf("quality fail %s score=%d err=%s", name, qr.Score, qr.Error)
					if cfg.DemoteOnFailValue() {
						if err := s.demote(ctx, name); err != nil {
							s.logger.Printf("demote %s failed: %v", name, err)
						} else {
							s.logger.Printf("demoted %s", name)
						}
					}
					continue
				}
				s.logger.Printf("quality ok %s score=%d port=%d exit=%s loc=%s", name, qr.Score, node.Port, qr.ExitIP, qr.CFLoc)
			} else if !cfg.RequireCloudflare {
				s.logger.Printf("promoted node %s port=%d (cf gate off)", name, node.Port)
			}
		} else if node.Port == 0 {
			s.logger.Printf("promoted node %s has no port yet", name)
			if cfg.RequireCloudflare && cfg.DemoteOnFailValue() {
				_ = s.demote(ctx, name)
			}
			continue
		} else if cfg.RequireCloudflare && s.quality == nil {
			s.logger.Printf("quality checker missing, skip cf for %s", name)
		}

		region, country := resolvePromotedRegion(cand, qr, haveQR)
		if (region == "" || region == geoip.RegionOther) && node.Port != 0 {
			user := node.Username
			pass := node.Password
			if user == "" {
				user = auth.Username
				pass = auth.Password
			}
			proxyURL := buildHTTPProxyURL(host, node.Port, user, pass)
			if tr := probeExitTrace(ctx, proxyURL); tr.CFLoc != "" || tr.ExitIP != "" {
				qr.CFLoc = firstNonEmpty(qr.CFLoc, tr.CFLoc)
				qr.ExitIP = firstNonEmpty(qr.ExitIP, tr.ExitIP)
				haveQR = true
				region, country = resolvePromotedRegion(cand, qr, true)
				s.logger.Printf("exit-trace %s exit=%s loc=%s err=%s", name, tr.ExitIP, tr.CFLoc, tr.Error)
			}
		}
		if region == "" || region == geoip.RegionOther {
			s.logger.Printf("region unresolved %s (cfLoc=%q exitIP=%q freeRegion=%q qualityErr=%q)",
				name, qr.CFLoc, qr.ExitIP, cand.Region, qr.Error)
			continue
		}
		if err := s.nodes.UpdateNodeRegion(ctx, name, region, country); err != nil {
			s.logger.Printf("update region %s -> %s: %v", name, region, err)
		} else {
			s.logger.Printf("region %s -> %s (%s)", name, region, country)
		}
		if err := s.nodes.PersistRegionOverride(node.URI, region); err != nil {
			s.logger.Printf("persist region override %s: %v", name, err)
		}
	}
	return nil
}

// resolvePromotedRegion picks the best known region for a promoted free proxy.
// Priority: CFLoc / ExitIP geo -> inherited free-node region.
func resolvePromotedRegion(cand Candidate, qr QualityResult, haveQR bool) (region, country string) {
	if haveQR {
		if loc := strings.ToLower(strings.TrimSpace(qr.CFLoc)); loc != "" {
			loc = strings.ToLower(loc)
			if loc != geoip.RegionOther {
				return loc, geoip.RegionName(loc)
			}
		}
		if ip := strings.TrimSpace(qr.ExitIP); ip != "" && net.ParseIP(ip) != nil {
			// Best-effort local GeoIP of the observed exit IP. Optional: no DB => skip.
			if info := lookupExitIPRegion(ip); info.Code != "" && info.Code != geoip.RegionOther {
				return info.Code, info.Country
			}
		}
	}
	region = strings.ToLower(strings.TrimSpace(cand.Region))
	country = strings.TrimSpace(cand.Country)
	if region == "" || region == geoip.RegionOther {
		return "", ""
	}
	if country == "" {
		country = geoip.RegionName(region)
	}
	return region, country
}

// lookupExitIPRegion resolves an observed exit IP to a region code.
// Tests may override this. Default is a no-op; CFLoc is preferred when present.
var lookupExitIPRegion = func(ip string) geoip.RegionInfo {
	_ = ip
	return geoip.RegionInfo{}
}

// SetExitIPRegionLookup installs a GeoIP exit-IP resolver used when CFLoc is empty.
func SetExitIPRegionLookup(fn func(ip string) geoip.RegionInfo) {
	if fn == nil {
		lookupExitIPRegion = func(ip string) geoip.RegionInfo { return geoip.RegionInfo{} }
		return
	}
	lookupExitIPRegion = fn
}

func (s *Service) demote(ctx context.Context, name string) error {
	if err := s.nodes.DeleteNode(ctx, name); err != nil {
		return err
	}
	return s.nodes.TriggerReload(ctx)
}

func findFreeProxyName(nodes []config.NodeConfig, uri string) string {
	key := canonicalURI(uri)
	for _, node := range nodes {
		if node.Source != config.NodeSourceFreeProxy {
			continue
		}
		if canonicalURI(node.URI) == key {
			return node.Name
		}
	}
	return ""
}

func buildSocksURL(host string, port uint16, username, password string) string {
	return buildProxyURL("socks5", host, port, username, password)
}

func buildHTTPProxyURL(host string, port uint16, username, password string) string {
	return buildProxyURL("http", host, port, username, password)
}

func buildProxyURL(scheme, host string, port uint16, username, password string) string {
	u := &url.URL{
		Scheme: scheme,
		Host:   fmt.Sprintf("%s:%d", host, port),
	}
	if username != "" {
		u.User = url.UserPassword(username, password)
	}
	return u.String()
}

type exitTrace struct {
	ExitIP string
	CFLoc  string
	Error  string
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// probeExitTrace fetches Cloudflare /cdn-cgi/trace through a promoted multi-port HTTP proxy.
func probeExitTrace(ctx context.Context, proxyURL string) exitTrace {
	out := exitTrace{}
	parsed, err := url.Parse(strings.TrimSpace(proxyURL))
	if err != nil || parsed.Host == "" {
		out.Error = "invalid proxy url"
		return out
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(parsed)
	client := &http.Client{Timeout: 12 * time.Second, Transport: transport}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cloudflarecheck.DefaultTraceURL, nil)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	resp, err := client.Do(req)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		out.Error = fmt.Sprintf("trace status %d", resp.StatusCode)
		return out
	}
	trace := cloudflarecheck.ParseTrace(string(body))
	out.ExitIP = strings.TrimSpace(trace.IP)
	out.CFLoc = strings.TrimSpace(trace.LOC)
	if out.ExitIP == "" && out.CFLoc == "" {
		out.Error = "empty trace"
	}
	return out
}
