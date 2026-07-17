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
	failedUntil  map[string]time.Time
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
		failedUntil:  make(map[string]time.Time),
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
		if r := recover(); r != nil {
			s.logger.Printf("loop panic: %v", r)
		}
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
		if err := s.runOnceSafe(ctx); err != nil && ctx.Err() == nil {
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

func (s *Service) runOnceSafe(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return s.RunOnce(ctx)
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

// RunOnce executes a single promote cycle (select → prune → promote → reload → quality → demote).
func (s *Service) RunOnce(ctx context.Context) (retErr error) {
	if s == nil || s.nodes == nil || s.snaps == nil {
		return nil
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	started := s.now()
	cfg := s.cfg().Normalized()
	if !cfg.Enabled {
		return nil
	}

	var (
		active                bool
		initialStats          promotedStats
		finalStats            promotedStats
		selectedCount         int
		candidateCount        int
		cooldownSkipped       int
		createdCount          int
		staleDemotedCount     int
		failedDemotedCount    int
		rollbackPromotedCount int
	)
	defer func() {
		if !active {
			return
		}
		status := "ok"
		if retErr != nil {
			status = "error"
		}
		duration := s.now().Sub(started)
		missing := cfg.MaxPromoted - finalStats.Total
		if missing < 0 {
			missing = 0
		}
		s.logger.Printf("cycle summary status=%s duration=%s promoted_before=%d promoted_total=%d promoted_available=%d promoted_with_port=%d promoted_blacklisted=%d target=%d missing=%d candidates_selected=%d candidates_after_cooldown=%d cooldown_skipped=%d created=%d stale_demoted=%d failed_demoted=%d rollback=%d",
			status, duration, initialStats.Total, finalStats.Total, finalStats.Available, finalStats.WithPort, finalStats.Blacklisted,
			cfg.MaxPromoted, missing, selectedCount, candidateCount, cooldownSkipped, createdCount, staleDemotedCount, failedDemotedCount, rollbackPromotedCount)
		if cfg.MaxPromoted > 0 && finalStats.Total < cfg.MaxPromoted {
			s.logger.Printf("cycle warn promoted_shortfall current=%d target=%d missing=%d candidates_after_cooldown=%d created=%d failed_demoted=%d rollback=%d",
				finalStats.Total, cfg.MaxPromoted, missing, candidateCount, createdCount, failedDemotedCount, rollbackPromotedCount)
		}
	}()

	nodes, err := s.nodes.ListConfigNodes(ctx)
	if err != nil {
		return fmt.Errorf("list config nodes: %w", err)
	}
	snaps := s.snaps.ListSnapshots()
	initialStats = promotedStatsFor(snaps, nodes, cfg.NamePrefix)
	finalStats = initialStats
	active = true
	if cfg.DemoteOnFailValue() {
		staleNames, staleURIs := stalePromoted(snaps, nodes, cfg)
		if len(staleNames) > 0 {
			s.logger.Printf("reload before action=stale-demote promoted_total=%d promoted_available=%d promoted_with_port=%d promoted_blacklisted=%d demote_count=%d",
				finalStats.Total, finalStats.Available, finalStats.WithPort, finalStats.Blacklisted, len(staleNames))
			for _, uri := range staleURIs {
				s.markFailed(uri, cfg)
			}
			if err := s.demoteBatch(ctx, staleNames); err != nil {
				s.logger.Printf("demote stale promoted nodes failed: %v", err)
			} else {
				staleDemotedCount = len(staleNames)
				s.logger.Printf("demoted %d stale promoted node(s)", len(staleNames))
				nodes, err = s.nodes.ListConfigNodes(ctx)
				if err != nil {
					return fmt.Errorf("list config nodes after stale demote: %w", err)
				}
				finalStats = promotedStatsFor(snaps, nodes, cfg.NamePrefix)
				s.logger.Printf("reload after action=stale-demote promoted_total=%d promoted_available=%d promoted_with_port=%d promoted_blacklisted=%d delta_total=%d",
					finalStats.Total, finalStats.Available, finalStats.WithPort, finalStats.Blacklisted, finalStats.Total-initialStats.Total)
			}
		}
	}

	selected := SelectCandidates(snaps, nodes, cfg)
	selectedCount = len(selected)
	candidates := selected
	candidates, cooldownSkipped = s.filterCooldownWithStats(candidates)
	candidateCount = len(candidates)
	if len(candidates) == 0 {
		return nil
	}

	s.logger.Printf("promoting candidates_selected=%d candidates_after_cooldown=%d cooldown_skipped=%d", selectedCount, candidateCount, cooldownSkipped)
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
			s.markFailed(cand.URI, cfg)
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
		createdCount++
		s.logger.Printf("created promoted node %s (from free %s, latency=%dms success=%d)",
			created.Name, cand.Name, cand.LastLatencyMs, cand.SuccessCount)
	}
	if len(promotedNames) == 0 {
		return nil
	}

	preReloadNodes, err := s.nodes.ListConfigNodes(ctx)
	if err != nil {
		return fmt.Errorf("list nodes before promote reload: %w", err)
	}
	preReloadStats := promotedStatsFor(snaps, preReloadNodes, cfg.NamePrefix)
	s.logger.Printf("reload before action=promote promoted_total=%d promoted_available=%d promoted_with_port=%d promoted_blacklisted=%d created=%d delta_total=%d",
		preReloadStats.Total, preReloadStats.Available, preReloadStats.WithPort, preReloadStats.Blacklisted, createdCount, preReloadStats.Total-finalStats.Total)

	if err := s.nodes.TriggerReload(ctx); err != nil {
		for _, name := range promotedNames {
			if cand, ok := candidateByName[name]; ok {
				s.markFailed(cand.URI, cfg)
			}
		}
		if delErr := s.deletePromoted(ctx, promotedNames); delErr != nil {
			s.logger.Printf("rollback promoted nodes after reload failure failed: %v", delErr)
		}
		rollbackPromotedCount = len(promotedNames)
		nodesAfterRollback, listErr := s.nodes.ListConfigNodes(ctx)
		if listErr == nil {
			finalStats = promotedStatsFor(snaps, nodesAfterRollback, cfg.NamePrefix)
		}
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
	finalStats = promotedStatsFor(snaps, nodes, cfg.NamePrefix)
	s.logger.Printf("reload after action=promote promoted_total=%d promoted_available=%d promoted_with_port=%d promoted_blacklisted=%d delta_total=%d delta_with_port=%d",
		finalStats.Total, finalStats.Available, finalStats.WithPort, finalStats.Blacklisted,
		finalStats.Total-preReloadStats.Total, finalStats.WithPort-preReloadStats.WithPort)
	byName := make(map[string]config.NodeConfig, len(nodes))
	for _, n := range nodes {
		byName[n.Name] = n
	}

	auth := s.listen()
	host := strings.TrimSpace(auth.Host)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}

	demoteNames := make([]string, 0)
	demoteURIs := make([]string, 0)
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
			if haveQR {
				if cfg.RequireCloudflare {
					if !qr.OK || qr.Score < cfg.MinCloudflareScore {
						s.logger.Printf("quality fail %s score=%d err=%s", name, qr.Score, qr.Error)
						if cfg.DemoteOnFailValue() {
							demoteNames = append(demoteNames, name)
							demoteURIs = append(demoteURIs, cand.URI)
						}
						continue
					}
					s.logger.Printf("quality ok %s score=%d port=%d exit=%s loc=%s", name, qr.Score, node.Port, qr.ExitIP, qr.CFLoc)
				} else if cfg.PostValidateValue() && !qr.OK {
					s.logger.Printf("post-validate fail %s err=%s", name, qr.Error)
					if cfg.DemoteOnFailValue() {
						demoteNames = append(demoteNames, name)
						demoteURIs = append(demoteURIs, cand.URI)
					}
					continue
				} else if !cfg.RequireCloudflare {
					s.logger.Printf("promoted node %s port=%d (cf gate off)", name, node.Port)
				}
			}
		} else if node.Port == 0 {
			s.logger.Printf("promoted node %s has no port yet", name)
			if cfg.DemoteOnFailValue() && (cfg.RequireCloudflare || cfg.PostValidateValue()) {
				demoteNames = append(demoteNames, name)
				demoteURIs = append(demoteURIs, cand.URI)
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
	if len(demoteNames) > 0 {
		for _, uri := range demoteURIs {
			s.markFailed(uri, cfg)
		}
		preDemoteStats := finalStats
		s.logger.Printf("reload before action=failed-demote promoted_total=%d promoted_available=%d promoted_with_port=%d promoted_blacklisted=%d demote_count=%d",
			preDemoteStats.Total, preDemoteStats.Available, preDemoteStats.WithPort, preDemoteStats.Blacklisted, len(demoteNames))
		if err := s.demoteBatch(ctx, demoteNames); err != nil {
			return fmt.Errorf("demote failed promoted nodes: %w", err)
		}
		failedDemotedCount = len(demoteNames)
		s.logger.Printf("demoted %d failed promoted node(s)", len(demoteNames))
		nodes, err = s.nodes.ListConfigNodes(ctx)
		if err != nil {
			return fmt.Errorf("list nodes after failed demote: %w", err)
		}
		finalStats = promotedStatsFor(snaps, nodes, cfg.NamePrefix)
		s.logger.Printf("reload after action=failed-demote promoted_total=%d promoted_available=%d promoted_with_port=%d promoted_blacklisted=%d delta_total=%d delta_with_port=%d",
			finalStats.Total, finalStats.Available, finalStats.WithPort, finalStats.Blacklisted,
			finalStats.Total-preDemoteStats.Total, finalStats.WithPort-preDemoteStats.WithPort)
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

func (s *Service) markFailed(uri string, cfg config.FreeProxyPromoteConfig) {
	key := canonicalURI(uri)
	if key == "" || cfg.FailedCooldown <= 0 {
		return
	}
	s.mu.Lock()
	if s.failedUntil == nil {
		s.failedUntil = make(map[string]time.Time)
	}
	s.failedUntil[key] = s.now().Add(cfg.FailedCooldown)
	s.mu.Unlock()
}

type promotedStats struct {
	Total       int
	Available   int
	WithPort    int
	Blacklisted int
}

func promotedStatsFor(snaps []Snapshot, nodes []config.NodeConfig, prefix string) promotedStats {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return promotedStats{}
	}
	snapByName := make(map[string]Snapshot, len(snaps))
	for _, snap := range snaps {
		if strings.HasPrefix(snap.Name, prefix) {
			snapByName[snap.Name] = snap
		}
	}
	var out promotedStats
	for _, node := range nodes {
		if !strings.HasPrefix(node.Name, prefix) {
			continue
		}
		out.Total++
		if node.Port > 0 {
			out.WithPort++
		}
		if snap, ok := snapByName[node.Name]; ok {
			if snap.Blacklisted {
				out.Blacklisted++
			}
			if snap.Available && !snap.Blacklisted {
				out.Available++
			}
		}
	}
	return out
}

func (s *Service) filterCooldown(candidates []Candidate) []Candidate {
	out, _ := s.filterCooldownWithStats(candidates)
	return out
}

func (s *Service) filterCooldownWithStats(candidates []Candidate) ([]Candidate, int) {
	if len(candidates) == 0 {
		return candidates, 0
	}
	now := s.now()
	out := candidates[:0]
	skipped := 0
	s.mu.Lock()
	for _, cand := range candidates {
		key := canonicalURI(cand.URI)
		until, ok := s.failedUntil[key]
		if ok && now.Before(until) {
			skipped++
			s.logger.Printf("skip cooled-down candidate %s until %s", cand.Name, until.Format(time.RFC3339))
			continue
		}
		if ok && !now.Before(until) {
			delete(s.failedUntil, key)
		}
		out = append(out, cand)
	}
	s.mu.Unlock()
	return out, skipped
}

func stalePromoted(snaps []Snapshot, nodes []config.NodeConfig, cfg config.FreeProxyPromoteConfig) ([]string, []string) {
	uriByName := make(map[string]string, len(nodes))
	for _, node := range nodes {
		if strings.HasPrefix(node.Name, cfg.NamePrefix) {
			uriByName[node.Name] = node.URI
		}
	}
	if len(uriByName) == 0 {
		return nil, nil
	}
	names := make([]string, 0)
	uris := make([]string, 0)
	for _, snap := range snaps {
		if !strings.HasPrefix(snap.Name, cfg.NamePrefix) {
			continue
		}
		if _, ok := uriByName[snap.Name]; !ok {
			continue
		}
		bad := snap.Blacklisted || (snap.InitialCheckDone && !snap.Available)
		if cfg.MaxFailureCount >= 0 && snap.FailureCount > cfg.MaxFailureCount {
			bad = true
		}
		if !bad {
			continue
		}
		names = append(names, snap.Name)
		uris = append(uris, uriByName[snap.Name])
	}
	return names, uris
}

func (s *Service) deletePromoted(ctx context.Context, names []string) error {
	var first error
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		if err := s.nodes.DeleteNode(ctx, name); err != nil {
			s.logger.Printf("delete promoted %s failed: %v", name, err)
			if first == nil {
				first = err
			}
		}
	}
	return first
}

func (s *Service) demoteBatch(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	if err := s.deletePromoted(ctx, names); err != nil {
		return err
	}
	return s.nodes.TriggerReload(ctx)
}

func (s *Service) demote(ctx context.Context, name string) error {
	return s.demoteBatch(ctx, []string{name})
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
