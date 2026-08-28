package monitor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/geoip"
	M "github.com/sagernet/sing/common/metadata"
)

// Config mirrors user settings needed by the monitoring server.
type Config struct {
	Enabled        bool
	Listen         string
	ProbeTarget    string
	Password       string
	ProxyUsername  string // 代理池的用户名（用于导出）
	ProxyPassword  string // 代理池的密码（用于导出）
	ExternalIP     string // 外部 IP 地址，用于导出时替换 0.0.0.0
	SkipCertVerify bool   // 全局跳过 SSL 证书验证
	// ProbeConcurrency caps simultaneous health-check dials. 0 = auto.
	ProbeConcurrency int
	// APIKeys / CORSOrigins mirror management config for auth middleware.
	// Updated via SetConfig when YAML reloads.
	APIKeys     []config.APIKeyConfig
	CORSOrigins []string
	// Governance controls structural quarantine / zombie policy (optional).
	Governance GovernanceConfig
}

// NodeInfo is static metadata about a proxy entry.
type NodeInfo struct {
	Tag           string `json:"tag"`
	Name          string `json:"name"`
	URI           string `json:"uri"`
	Mode          string `json:"mode"`
	ListenAddress string `json:"listen_address,omitempty"`
	Port          uint16 `json:"port,omitempty"`
	Region        string `json:"region,omitempty"`       // GeoIP region code: "jp", "kr", "us", "hk", "tw", "other"
	Country       string `json:"country,omitempty"`      // Full country name from GeoIP
	Source        string `json:"source,omitempty"`       // Runtime source: inline, nodes_file, subscription, free_proxy
	Protocol      string `json:"protocol,omitempty"`     // URI scheme: vless, vmess, hysteria2, anytls, ...
	ViaUpstream   bool   `json:"via_upstream,omitempty"` // true when dialed through upstream_proxy detour
}

// TimelineEvent represents a single usage event for debug tracking.
type TimelineEvent struct {
	Time      time.Time `json:"time"`
	Success   bool      `json:"success"`
	LatencyMs int64     `json:"latency_ms"`
	Error     string    `json:"error,omitempty"`
}

const maxTimelineSize = 20

// Snapshot is a runtime view of a proxy node.
type Snapshot struct {
	NodeInfo
	FailureCount      int             `json:"failure_count"`
	SuccessCount      int64           `json:"success_count"`
	Blacklisted       bool            `json:"blacklisted"`
	BlacklistedUntil  time.Time       `json:"blacklisted_until"`
	ActiveConnections int32           `json:"active_connections"`
	LastError         string          `json:"last_error,omitempty"`
	LastErrorStage    string          `json:"last_error_stage,omitempty"` // dial | http_probe | empty on success
	QuarantineReason  string          `json:"quarantine_reason,omitempty"`
	LastFailure       time.Time       `json:"last_failure,omitempty"`
	LastSuccess       time.Time       `json:"last_success,omitempty"`
	LastProbeLatency  time.Duration   `json:"last_probe_latency,omitempty"`
	LastLatencyMs     int64           `json:"last_latency_ms"`
	LastDialMs        int64           `json:"last_dial_ms,omitempty"`
	LastHTTPMs        int64           `json:"last_http_ms,omitempty"`
	Available         bool            `json:"available"`
	InitialCheckDone  bool            `json:"initial_check_done"`
	Timeline          []TimelineEvent `json:"timeline,omitempty"`
}

type probeFunc func(ctx context.Context) (time.Duration, error)
type releaseFunc func()

type EntryHandle struct {
	ref *entry
}

// Probe stage constants for last_error_stage / diagnostics.
const (
	ProbeStageDial = "dial"
	ProbeStageHTTP = "http_probe"
)

type entry struct {
	info             NodeInfo
	failure          int
	success          int64
	timeline         []TimelineEvent
	blacklist        bool
	until            time.Time
	lastError        string
	lastErrorStage   string
	quarantineReason string
	lastFail         time.Time
	lastOK           time.Time
	lastProbe        time.Duration
	lastDial         time.Duration
	lastHTTP         time.Duration
	active           atomic.Int32
	probe            probeFunc
	release          releaseFunc
	blacklistFn      func(time.Duration)
	initialCheckDone bool
	available        bool
	probeRound       uint64 // for isolated probe thinning
	governance       GovernanceConfig
	mu               sync.RWMutex
}

// Manager aggregates all node states for the UI/API.
type Manager struct {
	cfg         Config
	probeDst    M.Socksaddr
	probeReady  bool
	mu          sync.RWMutex
	nodes       map[string]*entry
	allowedTags map[string]struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	logger      Logger

	probeInterval time.Duration // periodic sweep period, for freshness skipping
	sweeps        atomic.Int32  // in-flight sweeps; a reload can overlap the periodic tick
	// probeSem caps dials across ALL sweeps, not per sweep: a reload-triggered
	// ProbeAllNow can run alongside the periodic tick, and per-sweep limits would
	// simply add up (three overlapping sweeps at startup meant 3x the cap).
	probeSem     chan struct{}
	probeWorkers int
	// firstSweepDone closes once the boot sweep finishes, so callers that share
	// the upstream path can hold off until the startup burst is over.
	firstSweepDone chan struct{}
	firstSweepOnce sync.Once
}

// Logger interface for logging
type Logger interface {
	Info(args ...any)
	Warn(args ...any)
}

// NewManager constructs a manager and pre-validates the probe target.
func NewManager(cfg Config) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	workers := probeWorkerLimit(runtime.NumCPU())
	if cfg.ProbeConcurrency > 0 {
		workers = cfg.ProbeConcurrency
	}
	m := &Manager{
		cfg:            cfg,
		nodes:          make(map[string]*entry),
		ctx:            ctx,
		cancel:         cancel,
		probeWorkers:   workers,
		probeSem:       make(chan struct{}, workers),
		firstSweepDone: make(chan struct{}),
	}
	if cfg.ProbeTarget != "" {
		target := cfg.ProbeTarget
		// Strip URL scheme if present (e.g., "https://www.google.com:443" -> "www.google.com:443")
		if strings.HasPrefix(target, "https://") {
			target = strings.TrimPrefix(target, "https://")
		} else if strings.HasPrefix(target, "http://") {
			target = strings.TrimPrefix(target, "http://")
		}
		// Remove trailing path if present
		if idx := strings.Index(target, "/"); idx != -1 {
			target = target[:idx]
		}
		host, port, err := net.SplitHostPort(target)
		if err != nil {
			// If no port specified, use default based on original scheme
			if strings.HasPrefix(cfg.ProbeTarget, "https://") {
				host = target
				port = "443"
			} else {
				host = target
				port = "80"
			}
		}
		parsed := M.ParseSocksaddrHostPort(host, parsePort(port))
		m.probeDst = parsed
		m.probeReady = true
	}
	return m, nil
}

// SetLogger sets the logger for the manager.
func (m *Manager) SetLogger(logger Logger) {
	m.logger = logger
}

// StartPeriodicHealthCheck starts a background goroutine that periodically checks all nodes.
// interval: how often to check (e.g., 30 * time.Second)
// timeout: timeout for each probe (e.g., 10 * time.Second)
func (m *Manager) StartPeriodicHealthCheck(interval, timeout time.Duration) {
	if !m.probeReady {
		if m.logger != nil {
			m.logger.Warn("probe target not configured, periodic health check disabled")
		}
		return
	}

	m.probeInterval = interval

	go func() {
		// 启动后立即进行一次检查（此时还没有真实流量可借用，必须全量探测）
		m.probeAllNodes(timeout)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				m.sweep(timeout, true)
			}
		}
	}()

	if m.logger != nil {
		m.logger.Info("periodic health check started, interval: ", interval)
	}
}

// ProbeAllNow triggers a one-time health check on all nodes (e.g. after reload).
func (m *Manager) ProbeAllNow(timeout time.Duration) {
	m.probeAllNodes(timeout)
}

// probeAllNodes checks every registered node, skipping nothing that can be probed.
func (m *Manager) probeAllNodes(timeout time.Duration) {
	m.sweep(timeout, false)
}

// SweepInProgress reports whether a health-check sweep is currently dialing.
// Callers that share the same upstream path (e.g. the upstream health probe)
// use this to stay out of the sweep's way instead of competing with it.
func (m *Manager) SweepInProgress() bool {
	if m == nil {
		return false
	}
	return m.sweeps.Load() > 0
}

// FirstSweepDone is closed once the boot sweep has finished. It never closes
// when health checking is disabled, so callers must pair it with a deadline.
func (m *Manager) FirstSweepDone() <-chan struct{} {
	if m == nil {
		return nil
	}
	return m.firstSweepDone
}

// probeWorkerLimit derives the dial concurrency for a health-check sweep.
// Probes are network-bound, not CPU-bound, so core count is the wrong axis to
// scale on: on a 32-core host the old NumCPU()*2 opened 64 simultaneous dials,
// and when every node routes through one upstream relay that burst saturates
// the relay's selected outbound — stalling unrelated traffic (including the
// upstream health probe itself) for seconds. Keep it low and flat.
func probeWorkerLimit(cpus int) int {
	limit := cpus / 2
	if limit < minProbeWorkers {
		limit = minProbeWorkers
	}
	if limit > maxProbeWorkers {
		limit = maxProbeWorkers
	}
	return limit
}

const (
	minProbeWorkers = 4
	maxProbeWorkers = 16
	// probeLaunchStagger spaces out dial starts so a sweep ramps up instead of
	// slamming the relay with a full worker set in the same millisecond.
	probeLaunchStagger = 20 * time.Millisecond
)

// sweep checks registered nodes concurrently. When skipFresh is set, nodes that
// carried successful live traffic recently are trusted without a probe.
func (m *Manager) sweep(timeout time.Duration, skipFresh bool) {
	m.sweeps.Add(1)
	defer m.sweeps.Add(-1)

	m.mu.RLock()
	entries := make([]*entry, 0, len(m.nodes))
	for _, e := range m.nodes {
		entries = append(entries, e)
	}
	m.mu.RUnlock()

	if len(entries) == 0 {
		return
	}

	if m.logger != nil {
		m.logger.Info("starting health check for ", len(entries), " nodes")
	}

	workerLimit := m.probeWorkers
	sem := m.probeSem
	var wg sync.WaitGroup
	var availableCount atomic.Int32
	var failedCount atomic.Int32
	failureSummary := newProbeFailureSummary(5)

	var skippedIsolated atomic.Int32
	var skippedFresh atomic.Int32

	// A node that just carried real traffic has already proven the exact thing a
	// probe would test, so re-dialing it only adds load. Half the sweep period
	// keeps this from swallowing the probe's own lastOK from the previous round.
	var freshWindow time.Duration
	if skipFresh && m.probeInterval > 0 {
		freshWindow = m.probeInterval / 2
	}

	launched := 0
sweepLoop:
	for _, e := range entries {
		e.mu.Lock()
		probeFn := e.probe
		tag := e.info.Tag
		e.probeRound++
		round := e.probeRound
		g := e.governance
		reason := e.quarantineReason
		// Thin probes for structural isolates to cut noise (reason set, not failure-blacklist).
		skip := false
		if g.Enabled && reason != "" && g.IsolatedProbeEveryN > 1 {
			if int(round)%g.IsolatedProbeEveryN != 1 {
				skip = true
			}
		}
		// Failure blacklist still skips probes until expiry.
		if e.blacklist && time.Now().Before(e.until) && reason == "" {
			e.mu.Unlock()
			continue
		}
		fresh := freshWindow > 0 && e.available && e.initialCheckDone && reason == "" &&
			!e.lastOK.IsZero() && time.Since(e.lastOK) < freshWindow && e.lastFail.Before(e.lastOK)
		e.mu.Unlock()

		if probeFn == nil {
			continue
		}
		if skip {
			skippedIsolated.Add(1)
			continue
		}
		if fresh {
			skippedFresh.Add(1)
			continue
		}

		// Ramp the sweep up instead of releasing a full worker set at once.
		if launched > 0 && launched < workerLimit {
			select {
			case <-m.ctx.Done():
				break sweepLoop
			case <-time.After(probeLaunchStagger):
			}
		}
		launched++

		sem <- struct{}{}
		wg.Add(1)
		go func(entry *entry, probe probeFunc, tag string) {
			defer wg.Done()
			defer func() { <-sem }()

			ctx, cancel := context.WithTimeout(m.ctx, timeout)
			latency, err := probe(ctx)
			cancel()

			// Prefer probe func side-effects (RecordFailureStage/Success). Still mirror available flags.
			entry.mu.Lock()
			if err != nil {
				failedCount.Add(1)
				// stage-prefixed errors may already be recorded by probe func
				if entry.lastError == "" {
					entry.lastError = err.Error()
				}
				entry.lastFail = time.Now()
				entry.available = false
				entry.initialCheckDone = true
			} else {
				availableCount.Add(1)
				entry.lastOK = time.Now()
				entry.lastProbe = latency
				entry.available = true
				entry.initialCheckDone = true
				// Recovery: clear failure blacklist. Structural classes stay quarantined
				// unless classification no longer matches (e.g. host recovered / URI change).
				if entry.blacklist {
					entry.blacklist = false
					entry.until = time.Time{}
				}
				g := entry.governance
				if reason := g.ClassifyStructural(entry.info.Protocol, entry.info.URI); reason != "" {
					// Structural isolates remain out of effective pool even on rare success.
					entry.quarantineReason = reason
					entry.available = false
				} else {
					// Host quarantine is advisory: a successful probe clears it for this node.
					entry.quarantineReason = ""
				}
			}
			entry.mu.Unlock()

			if err != nil {
				failureSummary.Add(tag, err)
			}
		}(e, probeFn, tag)
	}
	wg.Wait()
	m.firstSweepOnce.Do(func() { close(m.firstSweepDone) })

	if m.logger != nil {
		m.logger.Info("health check completed: ", availableCount.Load(), " available, ", failedCount.Load(), " failed",
			", skipped_isolated=", skippedIsolated.Load(), ", skipped_fresh=", skippedFresh.Load(),
			", concurrency=", workerLimit)
		if failedCount.Load() > 0 {
			m.logger.Warn(failureSummary.String())
		}
	}
}

type probeFailureSummary struct {
	mu       sync.Mutex
	limit    int
	total    int
	order    []string
	byReason map[string]*probeFailureGroup
}

type probeFailureGroup struct {
	count     int
	sampleTag string
}

func newProbeFailureSummary(limit int) *probeFailureSummary {
	if limit <= 0 {
		limit = 5
	}
	return &probeFailureSummary{limit: limit, byReason: make(map[string]*probeFailureGroup)}
}

func (s *probeFailureSummary) Add(tag string, err error) {
	if s == nil || err == nil {
		return
	}
	reason := err.Error()
	if reason == "" {
		reason = "unknown error"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	group := s.byReason[reason]
	if group == nil {
		group = &probeFailureGroup{sampleTag: tag}
		s.byReason[reason] = group
		s.order = append(s.order, reason)
	}
	group.count++
	if group.sampleTag == "" {
		group.sampleTag = tag
	}
}

func (s *probeFailureSummary) String() string {
	if s == nil {
		return "probe failures: 0"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.total == 0 {
		return "probe failures: 0"
	}
	parts := make([]string, 0, min(len(s.order), s.limit))
	shown := 0
	for _, reason := range s.order {
		if shown >= s.limit {
			break
		}
		group := s.byReason[reason]
		if group == nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%dx %s (sample=%s)", group.count, reason, group.sampleTag))
		shown++
	}
	remainingReasons := len(s.order) - shown
	message := fmt.Sprintf("probe failures aggregated: total=%d reasons=%d", s.total, len(s.order))
	if len(parts) > 0 {
		message += ": " + strings.Join(parts, "; ")
	}
	if remainingReasons > 0 {
		message += fmt.Sprintf("; +%d more reasons", remainingReasons)
	}
	return message
}

// Stop stops the periodic health check.
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

func parsePort(value string) uint16 {
	p, err := strconv.Atoi(value)
	if err != nil || p <= 0 || p > 65535 {
		return 80
	}
	return uint16(p)
}

// Register ensures a node is tracked and returns its entry.
func (m *Manager) Register(info NodeInfo) *EntryHandle {
	tag := strings.TrimSpace(info.Tag)
	if tag == "" {
		return nil
	}
	info.Tag = tag
	if strings.TrimSpace(info.Protocol) == "" {
		if i := strings.Index(info.URI, "://"); i > 0 {
			info.Protocol = strings.ToLower(info.URI[:i])
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.allowedTags != nil {
		if _, ok := m.allowedTags[info.Tag]; !ok {
			return nil
		}
	}
	e, ok := m.nodes[info.Tag]
	if !ok {
		e = &entry{
			info:     info,
			timeline: make([]TimelineEvent, 0, maxTimelineSize),
		}
		m.nodes[info.Tag] = e
	} else {
		e.info = info
	}
	// Keep governance pointer fresh for zombie / structural rules.
	g := m.cfg.Governance
	g.Normalize()
	e.governance = g
	// Apply structural quarantine at registration.
	// Important: do NOT set blacklist=true here — blacklist is reserved for failure/zombie bans.
	// Quarantined nodes stay out of the effective pool via available=false + initialCheckDone,
	// and pool selection already skips checked-unavailable members.
	if reason := g.ClassifyStructural(info.Protocol, info.URI); reason != "" {
		e.quarantineReason = reason
		e.available = false
		e.initialCheckDone = true
		// clear accidental failure-blacklist only when reason is pure structural and no prior fails?
		// keep existing blacklist if already failure-banned.
	} else if reason := g.ClassifyHostQuarantine(info.Protocol, info.URI); reason != "" {
		e.quarantineReason = reason
		e.available = false
		e.initialCheckDone = true
	} else if e.quarantineReason != "" && (strings.HasPrefix(e.quarantineReason, "isolate_") || strings.HasPrefix(e.quarantineReason, "host_quarantine")) {
		// URI/protocol changed away from structural class: drop structural reason.
		e.quarantineReason = ""
	}
	return &EntryHandle{ref: e}
}

// ClearNodes removes all registered nodes. Call before re-registering
// during a config reload so stale entries don't persist in the dashboard.
func (m *Manager) ClearNodes() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes = make(map[string]*entry)
}

// SetAllowedTags restricts runtime node registration to the currently active
// config generation. It also prunes existing entries that are no longer present
// so stale pools from an older sing-box instance cannot repopulate the monitor
// dashboard after a reload.
func (m *Manager) SetAllowedTags(tags []string) {
	allowed := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		allowed[tag] = struct{}{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowedTags = allowed
	for tag := range m.nodes {
		if _, ok := allowed[tag]; !ok {
			delete(m.nodes, tag)
		}
	}
}

// ClearSource removes registered nodes that came from a runtime source. It is
// used after config reloads to drop stale runtime-only free-proxy entries that
// may have been registered by an older box generation.
func (m *Manager) ClearSource(source string) {
	source = strings.TrimSpace(source)
	if source == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for tag, entry := range m.nodes {
		entry.mu.RLock()
		entrySource := entry.info.Source
		entry.mu.RUnlock()
		if entrySource == source {
			delete(m.nodes, tag)
		}
	}
}

// DestinationForProbe exposes the configured destination for health checks.
func (m *Manager) DestinationForProbe() (M.Socksaddr, bool) {
	if !m.probeReady {
		return M.Socksaddr{}, false
	}
	return m.probeDst, true
}

// Snapshot returns a sorted copy of current node states.
// If onlyAvailable is true, only returns nodes that passed initial health check.
func (m *Manager) Snapshot() []Snapshot {
	return m.SnapshotFiltered(false)
}

// SnapshotFiltered returns a sorted copy of current node states.
// If onlyAvailable is true, only returns nodes that passed initial health check.
// Nodes that haven't been checked yet are also included (they will be checked on first use).
func (m *Manager) SnapshotFiltered(onlyAvailable bool) []Snapshot {
	m.mu.RLock()
	list := make([]*entry, 0, len(m.nodes))
	for _, e := range m.nodes {
		list = append(list, e)
	}
	m.mu.RUnlock()
	snapshots := make([]Snapshot, 0, len(list))
	for _, e := range list {
		snap := e.snapshot()
		// 如果只要可用节点：
		// - 跳过已完成检查但不可用的节点
		// - 保留未完成检查的节点（它们会在首次使用时被检查）
		if onlyAvailable && ((snap.InitialCheckDone && !snap.Available) || snap.Blacklisted) {
			continue
		}
		snapshots = append(snapshots, snap)
	}
	// 按延迟排序（延迟小的在前面，未测试的排在最后）
	sort.Slice(snapshots, func(i, j int) bool {
		latencyI := snapshots[i].LastLatencyMs
		latencyJ := snapshots[j].LastLatencyMs
		// -1 表示未测试，排在最后
		if latencyI < 0 && latencyJ < 0 {
			return snapshots[i].Name < snapshots[j].Name // 都未测试时按名称排序
		}
		if latencyI < 0 {
			return false // i 未测试，排在后面
		}
		if latencyJ < 0 {
			return true // j 未测试，i 排在前面
		}
		if latencyI == latencyJ {
			return snapshots[i].Name < snapshots[j].Name // 延迟相同时按名称排序
		}
		return latencyI < latencyJ
	})
	return snapshots
}

// Probe triggers a manual health check.
func (m *Manager) Probe(ctx context.Context, tag string) (time.Duration, error) {
	e, err := m.entry(tag)
	if err != nil {
		return 0, err
	}
	if e.probe == nil {
		return 0, errors.New("probe not available for this node")
	}
	latency, err := e.probe(ctx)
	if err != nil {
		e.recordProbeFailure(err)
		return 0, err
	}
	e.recordProbeSuccess(latency)
	return latency, nil
}

// Release clears blacklist state for the given node.
func (m *Manager) Release(tag string) error {
	e, err := m.entry(tag)
	if err != nil {
		return err
	}
	if e.release == nil {
		return errors.New("release not available for this node")
	}
	e.release()
	return nil
}

// ManualBlacklist manually blacklists a node for the given duration.
func (m *Manager) ManualBlacklist(tag string, duration time.Duration) error {
	e, err := m.entry(tag)
	if err != nil {
		return err
	}
	e.mu.RLock()
	fn := e.blacklistFn
	e.mu.RUnlock()

	if fn != nil {
		// Blacklist in pool shared state (affects routing)
		fn(duration)
	}
	// Also mark in monitor state (affects UI display)
	e.blacklistUntil(time.Now().Add(duration))
	return nil
}

// UpdateRegion updates the stored region/country metadata for a node.
func (m *Manager) UpdateRegion(tag, region, country string) error {
	e, err := m.entry(tag)
	if err != nil {
		return err
	}
	region = strings.ToLower(strings.TrimSpace(region))
	country = strings.TrimSpace(country)
	if region == "" {
		return fmt.Errorf("invalid region")
	}
	if country == "" {
		country = geoip.RegionName(region)
		if country == "Unknown" {
			country = strings.ToUpper(region)
		}
	}
	e.mu.Lock()
	e.info.Region = region
	e.info.Country = country
	e.mu.Unlock()
	return nil
}

// SnapshotFor returns the current snapshot for a specific node.
func (m *Manager) SnapshotFor(tag string) (Snapshot, error) {
	e, err := m.entry(tag)
	if err != nil {
		return Snapshot{}, err
	}
	return e.snapshot(), nil
}

func (m *Manager) entry(tag string) (*entry, error) {
	m.mu.RLock()
	e, ok := m.nodes[tag]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("node %s not found", tag)
	}
	return e, nil
}

func (e *entry) snapshot() Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	latencyMs := int64(-1)
	if e.lastProbe > 0 {
		latencyMs = e.lastProbe.Milliseconds()
		if latencyMs == 0 {
			latencyMs = 1
		}
	}
	dialMs := int64(0)
	if e.lastDial > 0 {
		dialMs = e.lastDial.Milliseconds()
		if dialMs == 0 {
			dialMs = 1
		}
	}
	httpMs := int64(0)
	if e.lastHTTP > 0 {
		httpMs = e.lastHTTP.Milliseconds()
		if httpMs == 0 {
			httpMs = 1
		}
	}

	var timelineCopy []TimelineEvent
	if len(e.timeline) > 0 {
		timelineCopy = make([]TimelineEvent, len(e.timeline))
		copy(timelineCopy, e.timeline)
	}

	return Snapshot{
		NodeInfo:          e.info,
		FailureCount:      e.failure,
		SuccessCount:      e.success,
		Blacklisted:       e.blacklist,
		BlacklistedUntil:  e.until,
		ActiveConnections: e.active.Load(),
		LastError:         e.lastError,
		LastErrorStage:    e.lastErrorStage,
		QuarantineReason:  e.quarantineReason,
		LastFailure:       e.lastFail,
		LastSuccess:       e.lastOK,
		LastProbeLatency:  e.lastProbe,
		LastLatencyMs:     latencyMs,
		LastDialMs:        dialMs,
		LastHTTPMs:        httpMs,
		Available:         e.available,
		InitialCheckDone:  e.initialCheckDone,
		Timeline:          timelineCopy,
	}
}

func (e *entry) recordFailure(err error) {
	e.recordFailureStage(err, "")
}

func (e *entry) recordFailureStage(err error, stage string) {
	e.mu.Lock()
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	if stage != "" && errStr != "" && !strings.HasPrefix(errStr, "[") {
		errStr = "[" + stage + "] " + errStr
	}
	e.failure++
	e.lastError = errStr
	e.lastErrorStage = stage
	e.lastFail = time.Now()
	e.available = false
	e.appendTimelineLocked(false, 0, errStr)

	// Zombie auto-blacklist: never succeeded but failed many times.
	// Skip for vmess / flaky vless-ws with history when governance enabled.
	skipZombie := false
	thresh := 10
	dur := 6 * time.Hour
	if e.governance.Enabled {
		skipZombie = e.governance.ShouldSkipZombieAutoBlacklist(e.info.Protocol, e.info.URI, e.success)
	}
	if e.governance.ZombieZeroSuccessFails > 0 {
		thresh = e.governance.ZombieZeroSuccessFails
	}
	if e.governance.ZombieDuration > 0 {
		dur = e.governance.ZombieDuration
	}
	var zombieFn func(time.Duration)
	if !skipZombie && e.success == 0 && e.failure >= thresh && !e.blacklist {
		zombieFn = e.blacklistFn
		e.blacklist = true
		e.until = time.Now().Add(dur)
		if e.quarantineReason == "" {
			e.quarantineReason = "zombie_zero_success"
		}
	}
	e.mu.Unlock()
	if zombieFn != nil {
		zombieFn(dur)
	}
}

func (e *entry) recordSuccess() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.success++
	e.lastOK = time.Now()
	e.appendTimelineLocked(true, 0, "")
}

func (e *entry) recordSuccessWithLatency(latency time.Duration) {
	e.recordSuccessWithLatencyBreakdown(latency, 0, 0)
}

func (e *entry) recordSuccessWithLatencyBreakdown(total, dial, httpPart time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.success++
	e.lastOK = time.Now()
	e.lastError = ""
	e.lastErrorStage = ""
	if total <= 0 && (dial > 0 || httpPart > 0) {
		total = dial + httpPart
	}
	e.lastProbe = total
	e.lastDial = dial
	e.lastHTTP = httpPart
	e.available = true
	e.initialCheckDone = true
	latencyMs := total.Milliseconds()
	if latencyMs == 0 && total > 0 {
		latencyMs = 1
	}
	e.appendTimelineLocked(true, latencyMs, "")
}

func (e *entry) appendTimelineLocked(success bool, latencyMs int64, errStr string) {
	evt := TimelineEvent{
		Time:      time.Now(),
		Success:   success,
		LatencyMs: latencyMs,
		Error:     errStr,
	}
	if len(e.timeline) >= maxTimelineSize {
		copy(e.timeline, e.timeline[1:])
		e.timeline[len(e.timeline)-1] = evt
	} else {
		e.timeline = append(e.timeline, evt)
	}
}

func (e *entry) blacklistUntil(until time.Time) {
	e.mu.Lock()
	e.blacklist = true
	e.until = until
	e.mu.Unlock()
}

func (e *entry) clearBlacklist() {
	e.mu.Lock()
	e.blacklist = false
	e.until = time.Time{}
	e.mu.Unlock()
}

func (e *entry) incActive() {
	e.active.Add(1)
}

func (e *entry) decActive() {
	e.active.Add(-1)
}

func (e *entry) setProbe(fn probeFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.probe = fn
}

func (e *entry) setRelease(fn releaseFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.release = fn
}

func (e *entry) recordProbeLatency(d time.Duration) {
	e.mu.Lock()
	e.lastProbe = d
	e.mu.Unlock()
}

func (e *entry) recordProbeFailure(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	errStr := err.Error()
	e.failure++
	e.lastError = errStr
	e.lastFail = time.Now()
	e.available = false
	e.initialCheckDone = true
	e.appendTimelineLocked(false, 0, errStr)
}

func (e *entry) recordProbeSuccess(latency time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.success++
	e.lastOK = time.Now()
	e.lastError = ""
	e.lastProbe = latency
	e.available = true
	e.initialCheckDone = true
	latencyMs := latency.Milliseconds()
	if latencyMs == 0 && latency > 0 {
		latencyMs = 1
	}
	e.appendTimelineLocked(true, latencyMs, "")
}

// RecordFailure updates failure counters.
func (h *EntryHandle) RecordFailure(err error) {
	if h == nil || h.ref == nil {
		return
	}
	h.ref.recordFailure(err)
}

// RecordFailureStage records a probe failure with stage=dial|http_probe.
func (h *EntryHandle) RecordFailureStage(err error, stage string) {
	if h == nil || h.ref == nil {
		return
	}
	h.ref.recordFailureStage(err, stage)
}

// RecordSuccess updates the last success timestamp.
func (h *EntryHandle) RecordSuccess() {
	if h == nil || h.ref == nil {
		return
	}
	h.ref.recordSuccess()
}

// RecordSuccessWithLatency updates the last success timestamp and latency.
func (h *EntryHandle) RecordSuccessWithLatency(latency time.Duration) {
	if h == nil || h.ref == nil {
		return
	}
	h.ref.recordSuccessWithLatency(latency)
}

// RecordSuccessWithLatencyBreakdown records total/dial/http probe timings.
// total should be dial+http wall time; dial/http may be zero when unknown.
func (h *EntryHandle) RecordSuccessWithLatencyBreakdown(total, dial, httpPart time.Duration) {
	if h == nil || h.ref == nil {
		return
	}
	h.ref.recordSuccessWithLatencyBreakdown(total, dial, httpPart)
}

// Snapshot returns a consistent copy of the current entry state.
func (h *EntryHandle) Snapshot() Snapshot {
	if h == nil || h.ref == nil {
		return Snapshot{}
	}
	return h.ref.snapshot()
}

// Blacklist marks the node unavailable until the given deadline.
func (h *EntryHandle) Blacklist(until time.Time) {
	if h == nil || h.ref == nil {
		return
	}
	h.ref.blacklistUntil(until)
}

// ClearBlacklist removes the blacklist flag.
func (h *EntryHandle) ClearBlacklist() {
	if h == nil || h.ref == nil {
		return
	}
	h.ref.clearBlacklist()
}

// IncActive increments the active connection counter.
func (h *EntryHandle) IncActive() {
	if h == nil || h.ref == nil {
		return
	}
	h.ref.incActive()
}

// DecActive decrements the active connection counter.
func (h *EntryHandle) DecActive() {
	if h == nil || h.ref == nil {
		return
	}
	h.ref.decActive()
}

// SetProbe assigns a probe function.
func (h *EntryHandle) SetProbe(fn func(ctx context.Context) (time.Duration, error)) {
	if h == nil || h.ref == nil {
		return
	}
	h.ref.setProbe(fn)
}

// SetRelease assigns a release function.
func (h *EntryHandle) SetRelease(fn func()) {
	if h == nil || h.ref == nil {
		return
	}
	h.ref.setRelease(fn)
}

// SetBlacklistFn assigns a manual blacklist function.
func (h *EntryHandle) SetBlacklistFn(fn func(time.Duration)) {
	if h == nil || h.ref == nil {
		return
	}
	h.ref.mu.Lock()
	h.ref.blacklistFn = fn
	h.ref.mu.Unlock()
}

// MarkInitialCheckDone marks the initial health check as completed.
func (h *EntryHandle) MarkInitialCheckDone(available bool) {
	if h == nil || h.ref == nil {
		return
	}
	h.ref.mu.Lock()
	h.ref.initialCheckDone = true
	h.ref.available = available
	h.ref.mu.Unlock()
}

// MarkAvailable updates the availability status.
func (h *EntryHandle) MarkAvailable(available bool) {
	if h == nil || h.ref == nil {
		return
	}
	h.ref.mu.Lock()
	h.ref.available = available
	h.ref.mu.Unlock()
}
