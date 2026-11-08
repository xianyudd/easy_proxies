package boxmgr

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy_proxies/internal/androidports"
	"easy_proxies/internal/builder"
	"easy_proxies/internal/config"
	"easy_proxies/internal/geoip"
	"easy_proxies/internal/monitor"
	"easy_proxies/internal/outbound/pool"

	"github.com/sagernet/sing-box"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
)

// Ensure Manager implements monitor.NodeManager.
var _ monitor.NodeManager = (*Manager)(nil)

const (
	defaultDrainTimeout       = 10 * time.Second
	defaultHealthCheckTimeout = 30 * time.Second
	healthCheckPollInterval   = 500 * time.Millisecond
	periodicHealthInterval    = 5 * time.Minute
	periodicHealthTimeout     = 10 * time.Second
	// defaultPortReleaseTimeout 是重载时等待旧实例释放监听端口的上限。
	// 旧实例 Close 返回后端口可能仍被内核短暂占用，先等它真正可绑再构建新实例，
	// 避免把"释放延迟"误判成端口冲突而永久搬移节点端口。
	defaultPortReleaseTimeout = 5 * time.Second
	// portReleasePollInterval 是端口释放轮询间隔。
	portReleasePollInterval = 50 * time.Millisecond
)

// ManagerState 描述 manager 的生命周期状态。
// 状态是并发可观察的：重载/启动/关闭必须按状态机串行推进，避免后台刷新
// 在实例尚未就绪或正在重建时进入。
type ManagerState int

const (
	StateNotStarted ManagerState = iota
	StateStarting
	StateRunning
	StateReloading
	StateClosed
)

func (s ManagerState) String() string {
	switch s {
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateReloading:
		return "reloading"
	case StateClosed:
		return "closed"
	default:
		return "not_started"
	}
}

var (
	// ErrManagerNotStarted 表示尚未成功启动过实例（或启动/回滚失败后没有可用实例）。
	// 明确返回该错误而不是静默失败，调用方可以据此决定是否重试启动。
	ErrManagerNotStarted = errors.New("manager not started")
	// ErrBoxManagerClosed 表示 manager 已关闭，不再接受重载。
	ErrBoxManagerClosed = errors.New("box manager closed")
)

// Logger defines logging interface for the manager.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// Option configures the Manager.
type Option func(*Manager)

type boxInstance interface {
	Start() error
	Close() error
}

// WithLogger sets a custom logger.
func WithLogger(l Logger) Option {
	return func(m *Manager) { m.logger = l }
}

// WithPortReleaseTimeout overrides how long a reload waits for the previous
// instance's listening ports to become bindable again before building the new
// box. Tests use it to keep the wait window short.
func WithPortReleaseTimeout(d time.Duration) Option {
	return func(m *Manager) { m.portReleaseTimeout = d }
}

// Manager owns the lifecycle of the active sing-box instance.
type Manager struct {
	// lifecycleMu 串行化 Start / Reload / Close。
	// 没有它时，两次重载会互相看到 currentBox == nil（第一次重建期间把
	// currentBox 清空），后到者直接报 "manager not started"；Close 与
	// Reload 重叠还会让关闭完成后又被装回一个新实例（泄漏）。
	lifecycleMu sync.Mutex

	mu sync.RWMutex

	// state 由 mu 保护，配合 lifecycleMu 构成明确的启动/重载/关闭状态机。
	state ManagerState

	currentBox     boxInstance
	monitorMgr     *monitor.Manager
	monitorServer  *monitor.Server
	geoRouter      *geoip.Router
	androidRouters []*geoip.Router
	cfg            *config.Config
	monitorCfg     monitor.Config

	// upstreamPreferred is the configured/primary upstream_proxy that should be
	// persisted and shown in settings. upstreamRuntimeOverride is a temporary
	// failover value applied only to sing-box reloads; it must never stick on
	// m.cfg or be written by SaveSettings.
	upstreamPreferred       string
	upstreamRuntimeOverride string

	drainTimeout       time.Duration
	portReleaseTimeout time.Duration
	minAvailableNodes  int
	logger             Logger
	boxFactory         func(context.Context, *config.Config) (boxInstance, error)

	baseCtx            context.Context
	healthCheckStarted bool
	freeGeoIPCancel    context.CancelFunc
}

// New creates a BoxManager with the given config.
func New(cfg *config.Config, monitorCfg monitor.Config, opts ...Option) *Manager {
	m := &Manager{
		cfg:        cfg,
		monitorCfg: monitorCfg,
	}
	if cfg != nil {
		m.upstreamPreferred = strings.TrimSpace(cfg.UpstreamProxy)
	}
	m.applyConfigSettings(cfg)
	for _, opt := range opts {
		opt(m)
	}
	if m.logger == nil {
		m.logger = defaultLogger{}
	}
	if m.drainTimeout <= 0 {
		m.drainTimeout = defaultDrainTimeout
	}
	if m.portReleaseTimeout <= 0 {
		m.portReleaseTimeout = defaultPortReleaseTimeout
	}
	return m
}

// State reports the current lifecycle state. A manager that never started (or
// whose start/rollback failed) reports StateNotStarted instead of failing
// silently.
func (m *Manager) State() ManagerState {
	if m == nil {
		return StateNotStarted
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Manager) setState(state ManagerState) {
	m.mu.Lock()
	m.state = state
	m.mu.Unlock()
}

// setStateAfterFailedStart 启动失败后回到 StateNotStarted：后续重载会得到
// 明确的 ErrManagerNotStarted（而不是静默沿用不存在的实例）。
func (m *Manager) setStateAfterFailedStart() {
	m.mu.Lock()
	m.state = StateNotStarted
	m.mu.Unlock()
}

// Start creates and starts the initial sing-box instance.
func (m *Manager) Start(ctx context.Context) error {
	m.lifecycleMu.Lock()
	// Start 与 Reload/Close 串行。锁握到函数返回，保证任何并发重载/关闭
	// 都看到"已启动且可用"的实例，而不是启动中途的临时状态。
	defer m.lifecycleMu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if m.state == StateClosed {
		m.mu.Unlock()
		return ErrBoxManagerClosed
	}
	if m.state == StateStarting || m.state == StateRunning {
		m.mu.Unlock()
		return errors.New("sing-box already running")
	}
	m.state = StateStarting
	m.mu.Unlock()
	if err := m.ensureMonitor(ctx); err != nil {
		m.setStateAfterFailedStart()
		return err
	}

	m.mu.Lock()
	if m.cfg == nil {
		m.mu.Unlock()
		m.setStateAfterFailedStart()
		return errors.New("box manager requires config")
	}
	if m.currentBox != nil {
		m.mu.Unlock()
		m.setStateAfterFailedStart()
		return errors.New("sing-box already running")
	}
	m.applyConfigSettings(m.cfg)
	m.baseCtx = ctx
	cfg := m.cfg
	m.mu.Unlock()

	// Try to start, with automatic port conflict resolution
	var instance boxInstance
	maxRetries := 10
	for retry := 0; retry < maxRetries; retry++ {
		var err error
		instance, err = m.newBox(ctx, cfg)
		if err != nil {
			m.setStateAfterFailedStart()
			return err
		}
		if err = instance.Start(); err != nil {
			_ = instance.Close()
			// Check if it's a port conflict error
			if conflictPort := extractPortFromBindError(err); conflictPort > 0 {
				m.logger.Warnf("port %d is in use, reassigning and retrying...", conflictPort)
				if reassigned := reassignConflictingPort(cfg, conflictPort); reassigned {
					pool.ResetSharedStateStore() // Reset shared state for rebuild
					continue
				}
			}
			m.setStateAfterFailedStart()
			return fmt.Errorf("start sing-box: %w", err)
		}
		break // Success
	}

	m.mu.Lock()
	m.currentBox = instance
	m.state = StateRunning
	m.mu.Unlock()

	// Start periodic health check after nodes are registered
	m.mu.Lock()
	if m.monitorMgr != nil && !m.healthCheckStarted {
		m.monitorMgr.StartPeriodicHealthCheck(periodicHealthInterval, periodicHealthTimeout)
		m.healthCheckStarted = true
	}
	m.mu.Unlock()

	// Wait for initial health check if min nodes configured
	if cfg.SubscriptionRefresh.MinAvailableNodes > 0 {
		timeout := cfg.SubscriptionRefresh.HealthCheckTimeout
		if timeout <= 0 {
			timeout = defaultHealthCheckTimeout
		}
		if err := m.waitForHealthCheck(timeout); err != nil {
			m.logger.Warnf("initial health check warning: %v", err)
			// Don't fail startup, just warn
		}
	}

	m.logger.Infof("sing-box instance started with %d nodes", len(cfg.Nodes))

	// Start GeoIP router if enabled
	if cfg.GeoIP.Enabled {
		m.startGeoIPRouter(ctx, cfg)
	}
	if cfg.AndroidProxy.Enabled {
		m.startAndroidProxyRouters(ctx, cfg)
	}

	m.startFreeProxyGeoIPScan(cfg)

	return nil
}

// Reload gracefully switches to a new configuration.
// For multi-port mode, we must stop the old instance first to release ports.
func (m *Manager) Reload(newCfg *config.Config) error {
	if newCfg == nil {
		return errors.New("new config is nil")
	}

	// 重载串行化：与 Start/Close 共用 lifecycleMu，避免两个重载互相看到
	// currentBox == nil（上次重载清空后尚未装回）而误报 manager not started。
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	m.mu.Lock()
	if m.state == StateClosed {
		m.mu.Unlock()
		return ErrBoxManagerClosed
	}
	if m.state != StateRunning || m.currentBox == nil {
		m.mu.Unlock()
		return ErrManagerNotStarted
	}
	ctx := m.baseCtx
	oldBox := m.currentBox
	oldCfg := m.cfg
	m.currentBox = nil // Mark as reloading
	m.state = StateReloading
	// Resolve which upstream the new box should actually use, while tracking
	// the persistable primary separately from any temporary failover override.
	m.applyUpstreamForBoxBuildLocked(newCfg)
	m.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		// 启动上下文已被取消（进程即将退出）：不重建，把旧实例装回并恢复状态。
		m.mu.Lock()
		m.currentBox = oldBox
		m.state = StateRunning
		m.mu.Unlock()
		return err
	}

	m.logger.Infof("reloading with %d nodes", len(newCfg.Nodes))

	// For multi-port mode, we must close old instance first to release ports
	// This causes a brief interruption but avoids port conflicts
	if oldBox != nil {
		m.logger.Infof("stopping old instance to release ports...")
		if err := oldBox.Close(); err != nil {
			m.logger.Warnf("error closing old instance: %v", err)
		}
	}

	// Stop GeoIP / Android routers before starting new box to release ports
	m.mu.Lock()
	if m.geoRouter != nil {
		m.geoRouter.Stop()
		m.geoRouter = nil
	}
	for _, router := range m.androidRouters {
		if router != nil {
			_ = router.Stop()
		}
	}
	m.androidRouters = nil
	for _, router := range m.androidRouters {
		if router != nil {
			_ = router.Stop()
		}
	}
	m.androidRouters = nil
	m.mu.Unlock()

	// 旧实例 Close 返回后，监听端口可能仍被内核短暂占用（TIME_WAIT / close
	// 完成前的窗口）。把"端口真正可绑"作为构建新实例的前置条件：轮询等待，
	// 而不是固定 sleep 500ms；超时后仍占用才交给下面的重试/重分配逻辑，
	// 避免把释放延迟误判为第三方端口冲突而永久搬移节点端口。
	if stuck := waitForListenPortsReleased(ctx, newCfg, m.portReleaseTimeout); len(stuck) > 0 {
		m.logger.Warnf("port(s) %v still held after %s, will retry/reassign during start", stuck, m.portReleaseTimeout)
	}

	// Reset shared state store to ensure clean state for new config
	pool.ResetSharedStateStore()

	// During reload, keep old monitor snapshots visible until the new instance
	// is created and started. Allow both generations to register while the new
	// box is being built; prune to the new generation only after start succeeds.
	if m.monitorMgr != nil {
		m.monitorMgr.SetAllowedTags(mergeNodeTags(configNodeTags(oldCfg), configNodeTags(newCfg)))
	}

	// Create and start new box instance with automatic port conflict resolution
	var instance boxInstance
	maxRetries := 10
	for retry := 0; retry < maxRetries; retry++ {
		var err error
		instance, err = m.newBox(ctx, newCfg)
		if err != nil {
			// 启动上下文取消时不回滚（旧实例还在/进程在退出），直接返回。
			if ctx.Err() != nil {
				return fmt.Errorf("create new box: %w", err)
			}
			m.recoverAfterFailedReload(ctx, oldCfg)
			return fmt.Errorf("create new box: %w", err)
		}
		if err = instance.Start(); err != nil {
			_ = instance.Close()
			// Check if it's a port conflict error. During reload the old sing-box
			// process has just been closed; port release was already awaited above,
			// so a bind error here is usually a genuine third-party conflict.
			if conflictPort := extractPortFromBindError(err); conflictPort > 0 {
				if shouldRetryTransientPortConflict(retry) {
					m.logger.Warnf("port %d is still being released, retrying before reassignment...", conflictPort)
					time.Sleep(transientPortConflictBackoff)
					pool.ResetSharedStateStore()
					continue
				}
				m.logger.Warnf("port %d is still in use after retries, reassigning and retrying...", conflictPort)
				if reassigned := reassignConflictingPort(newCfg, conflictPort); reassigned {
					pool.ResetSharedStateStore()
					continue
				}
			}
			if ctx.Err() != nil {
				return fmt.Errorf("start new box: %w", err)
			}
			m.recoverAfterFailedReload(ctx, oldCfg)
			return fmt.Errorf("start new box: %w", err)
		}
		break // Success
	}

	m.applyConfigSettings(newCfg)

	m.mu.Lock()
	if m.state == StateClosed {
		// Close 已抢先完成（本应被 lifecycleMu 排除，这里作为兜底防泄漏），
		// 不再把新实例装回去，直接关闭它。
		m.mu.Unlock()
		_ = instance.Close()
		return ErrBoxManagerClosed
	}
	m.currentBox = instance
	m.cfg = newCfg
	m.state = StateRunning
	// Box may have been built with a temporary failover upstream; the
	// authoritative config object always keeps the preferred primary so
	// SaveSettings / WebUI never persist the override.
	m.restorePreferredUpstreamLocked()
	m.mu.Unlock()

	// Sync config to monitor server so future WebUI settings changes target the current config pointer
	if m.monitorServer != nil {
		m.monitorServer.SetConfig(m.cfg)
	}

	if m.monitorMgr != nil {
		m.monitorMgr.SetAllowedTags(configNodeTags(newCfg))
	}

	if m.monitorMgr != nil && !configHasSource(newCfg, config.NodeSourceFreeProxy) {
		m.monitorMgr.ClearSource(string(config.NodeSourceFreeProxy))
	}

	// Trigger initial health check for newly registered nodes
	if m.monitorMgr != nil {
		go m.monitorMgr.ProbeAllNow(periodicHealthTimeout)
	}

	m.logger.Infof("reload completed successfully with %d nodes", len(newCfg.Nodes))

	// Restart GeoIP router with new pools
	if newCfg.GeoIP.Enabled {
		m.startGeoIPRouter(ctx, newCfg)
	} else {
		m.mu.Lock()
		if m.geoRouter != nil {
			m.geoRouter.Stop()
			m.geoRouter = nil
		}
		m.mu.Unlock()
	}
	if newCfg.AndroidProxy.Enabled {
		m.startAndroidProxyRouters(ctx, newCfg)
	} else {
		m.mu.Lock()
		for _, router := range m.androidRouters {
			if router != nil {
				_ = router.Stop()
			}
		}
		m.androidRouters = nil
		m.mu.Unlock()
	}

	m.startFreeProxyGeoIPScan(newCfg)

	return nil
}

// recoverAfterFailedReload 在重载失败后尝试恢复旧配置。回滚成功则回到
// StateRunning；回滚也失败则标记 StateNotStarted，后续重载会得到明确的
// ErrManagerNotStarted，而不是静默使用不存在的实例。
func (m *Manager) recoverAfterFailedReload(ctx context.Context, oldCfg *config.Config) {
	if err := m.rollbackToOldConfig(ctx, oldCfg); err != nil {
		m.logger.Errorf("rollback failed: %v", err)
		m.setState(StateNotStarted)
	}
}

// rollbackToOldConfig attempts to restart with the previous configuration.
func (m *Manager) rollbackToOldConfig(ctx context.Context, oldCfg *config.Config) error {
	if oldCfg == nil {
		return errors.New("no previous config to roll back to")
	}
	m.logger.Warnf("attempting rollback to previous config...")
	if m.monitorMgr != nil {
		m.monitorMgr.SetAllowedTags(configNodeTags(oldCfg))
		m.monitorMgr.ClearNodes()
	}
	instance, err := m.newBox(ctx, oldCfg)
	if err != nil {
		m.logger.Errorf("rollback failed to create box: %v", err)
		return fmt.Errorf("create old box: %w", err)
	}
	if err := instance.Start(); err != nil {
		_ = instance.Close()
		m.logger.Errorf("rollback failed to start box: %v", err)
		return fmt.Errorf("start old box: %w", err)
	}
	m.mu.Lock()
	m.currentBox = instance
	m.cfg = oldCfg
	m.state = StateRunning
	m.mu.Unlock()
	// Sync config pointer to monitor server after rollback
	if m.monitorServer != nil {
		m.monitorServer.SetConfig(m.cfg)
	}
	m.logger.Infof("rollback successful")
	return nil
}

// Close terminates the active instance and auxiliary components.
// 与 Start/Reload 串行：若重载正在进行，先等它结束再关闭，保证 Close 返回后
// 不会有重建把新实例装回来（旧实现会在 Close 之后把 currentBox 重新赋值，
// 泄漏一个仍在运行的实例）。
func (m *Manager) Close() error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	m.mu.Lock()
	if m.state == StateClosed {
		m.mu.Unlock()
		return nil
	}
	m.state = StateClosed
	m.mu.Unlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	var err error
	if m.currentBox != nil {
		err = m.currentBox.Close()
		m.currentBox = nil
	}
	if m.monitorServer != nil {
		m.monitorServer.Shutdown(context.Background())
		m.monitorServer = nil
	}
	if m.monitorMgr != nil {
		m.monitorMgr.Stop()
		m.monitorMgr = nil
		m.healthCheckStarted = false
	}
	if m.geoRouter != nil {
		m.geoRouter.Stop()
		m.geoRouter = nil
	}
	for _, router := range m.androidRouters {
		if router != nil {
			_ = router.Stop()
		}
	}
	m.androidRouters = nil
	if m.freeGeoIPCancel != nil {
		m.freeGeoIPCancel()
		m.freeGeoIPCancel = nil
	}
	pool.ResetSharedStateStore()
	m.baseCtx = nil
	return err
}

// UpdateNodeRegion writes runtime region metadata for a node (monitor only).
func (m *Manager) UpdateNodeRegion(ctx context.Context, name, region, country string) error {
	_ = ctx
	if m == nil {
		return errConfigUnavailable
	}
	mon := m.MonitorManager()
	if mon == nil {
		return fmt.Errorf("monitor unavailable")
	}
	return mon.UpdateRegion(name, region, country)
}

// PersistRegionOverride stores a durable URI region override and saves settings.
func (m *Manager) PersistRegionOverride(uri, region string) error {
	if m == nil {
		return errConfigUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg == nil {
		return errConfigUnavailable
	}
	m.cfg.SetRegionOverride(uri, region)
	if err := m.cfg.SaveSettings(); err != nil {
		return fmt.Errorf("save region override: %w", err)
	}
	return nil
}

// FreeProxyPromoteSettings returns the current free-proxy promote settings.
func (m *Manager) FreeProxyPromoteSettings() config.FreeProxyPromoteConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg == nil {
		return config.FreeProxyPromoteConfig{}
	}
	return m.cfg.FreeProxyPromote
}

// MultiPortListenAuth returns multi-port listen host and default credentials.
func (m *Manager) MultiPortListenAuth() (host, username, password string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg == nil {
		return "127.0.0.1", "", ""
	}
	host = m.cfg.MultiPort.Address
	if host == "" {
		host = "127.0.0.1"
	}
	return host, m.cfg.MultiPort.Username, m.cfg.MultiPort.Password
}

// MonitorManager returns the shared monitor manager.
func (m *Manager) MonitorManager() *monitor.Manager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.monitorMgr
}

// MonitorServer returns the monitor HTTP server.
func (m *Manager) MonitorServer() *monitor.Server {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.monitorServer
}

// startFreeProxyGeoIPScan 在后台异步对免费代理节点做 GeoIP 地区识别。
// 启动和重载后调用一次；reload 时会取消上一轮未完成的扫描。
func (m *Manager) startFreeProxyGeoIPScan(cfg *config.Config) {
	dbPath := cfg.GeoIP.DatabasePath
	if dbPath == "" {
		return
	}

	// 取消上一轮扫描（reload 场景）
	m.mu.Lock()
	if m.freeGeoIPCancel != nil {
		m.freeGeoIPCancel()
	}
	baseCtx := m.baseCtx
	ctx, cancel := context.WithCancel(baseCtx)
	m.freeGeoIPCancel = cancel
	m.mu.Unlock()

	// 从 monitor 快照中提取免费代理节点的 tag→uri 映射
	// 需要等 monitor 注册完成，直接从 Snapshot 获取
	monMgr := m.MonitorManager()
	if monMgr == nil {
		cancel()
		return
	}

	// 快照免费代理节点
	type freeNode struct{ tag, uri string }
	var nodes []freeNode
	for _, snap := range monMgr.Snapshot() {
		if snap.Source == string(config.NodeSourceFreeProxy) && snap.URI != "" {
			nodes = append(nodes, freeNode{tag: snap.Tag, uri: snap.URI})
		}
	}

	if len(nodes) == 0 {
		cancel()
		return
	}

	logger := m.logger

	go func() {
		defer cancel()
		lkp, err := geoip.OpenExisting(dbPath)
		if err != nil {
			logger.Warnf("free-proxy geoip scan: open db: %v", err)
			return
		}
		defer lkp.Close()

		const workers = 12
		jobs := make(chan freeNode, len(nodes))
		for _, n := range nodes {
			jobs <- n
		}
		close(jobs)

		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for n := range jobs {
					if ctx.Err() != nil {
						return
					}
					info := lkp.LookupURI(n.uri)
					if info.Code == "" || info.Code == "other" {
						continue
					}
					_ = monMgr.UpdateRegion(n.tag, info.Code, info.Country)
				}
			}()
		}
		wg.Wait()
		logger.Infof("free-proxy geoip scan done: %d nodes", len(nodes))
	}()
}

// startGeoIPRouter starts the GeoIP region-routing HTTP proxy server.
func (m *Manager) startGeoIPRouter(ctx context.Context, cfg *config.Config) {
	// Stop existing router if any
	m.mu.Lock()
	if m.geoRouter != nil {
		m.geoRouter.Stop()
		m.geoRouter = nil
	}
	m.mu.Unlock()

	geoipPort := cfg.GeoIP.Port
	if geoipPort == 0 {
		geoipPort = 1221 // Default GeoIP router port
	}
	// Avoid conflict with the pool listener port
	if geoipPort == cfg.Listener.Port {
		geoipPort = 1221
		if geoipPort == cfg.Listener.Port {
			geoipPort = cfg.Listener.Port + 1
		}
		log.Printf("⚠️  GeoIP port conflicts with listener port %d, using %d instead", cfg.Listener.Port, geoipPort)
	}
	geoipListen := cfg.GeoIP.Listen
	if geoipListen == "" {
		geoipListen = cfg.Listener.Address
	}

	routerCfg := geoip.RouterConfig{
		Listen:   geoipListen,
		Port:     geoipPort,
		Username: cfg.Listener.Username,
		Password: cfg.Listener.Password,
	}

	router := geoip.NewRouter(routerCfg, nil)

	// Register region pool dialers
	for _, region := range geoip.AllRegions() {
		poolTag := fmt.Sprintf("pool-%s", region)
		if dialer, ok := pool.GetDialer(poolTag); ok {
			router.SetPool(region, dialer)
			log.Printf("   GeoIP: registered pool %s for region /%s", poolTag, region)
		}
	}

	// Register global pool dialer (for requests without region path)
	if dialer, ok := pool.GetDialer(pool.Tag); ok {
		router.SetGlobalPool(dialer)
	}

	if err := router.Start(ctx); err != nil {
		m.logger.Warnf("failed to start GeoIP router: %v", err)
		return
	}

	m.mu.Lock()
	m.geoRouter = router
	m.mu.Unlock()
}

// callWithBoxNew runs fn and converts a panic into an error. 部分畸形订阅节点
// 会让 sing-box 在出站初始化期间 panic（例如在格式化自身错误信息时对 *string
// 做字符串化），而不是返回错误。没有这层保护，单个坏节点就会使整个进程崩溃、
// 服务永远无法启动。
func callWithBoxNew(fn func() (*box.Box, error)) (instance *box.Box, err error) {
	defer func() {
		if r := recover(); r != nil {
			instance = nil
			err = fmt.Errorf("sing-box panicked during initialization: %v", r)
		}
	}()
	return fn()
}

// newBoxRecover wraps box.New with the panic-to-error guard.
func newBoxRecover(opts box.Options) (*box.Box, error) {
	return callWithBoxNew(func() (*box.Box, error) { return box.New(opts) })
}

// createBox builds a sing-box instance from config.
// It retries automatically when individual outbounds fail sing-box validation,
// removing the offending outbound each time.
func (m *Manager) createBox(ctx context.Context, cfg *config.Config) (*box.Box, error) {
	if cfg == nil {
		return nil, errors.New("config is nil")
	}
	if m.monitorMgr == nil {
		return nil, errors.New("monitor manager not initialized")
	}

	opts, err := builder.Build(cfg)
	if err != nil {
		return nil, fmt.Errorf("build sing-box options: %w", err)
	}

	maxRetries := len(cfg.Nodes)*3 + 50 // Dynamically scale retries to configuration size
	outboundErrRe := regexp.MustCompile(`initialize outbound\[(\d+)\]`)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		inboundRegistry := include.InboundRegistry()
		outboundRegistry := include.OutboundRegistry()
		pool.Register(outboundRegistry)
		endpointRegistry := include.EndpointRegistry()
		dnsRegistry := include.DNSTransportRegistry()
		serviceRegistry := include.ServiceRegistry()

		boxCtx := box.Context(ctx, inboundRegistry, outboundRegistry, endpointRegistry, dnsRegistry, serviceRegistry)
		boxCtx = monitor.ContextWith(boxCtx, m.monitorMgr)

		instance, err := newBoxRecover(box.Options{Context: boxCtx, Options: opts})
		if err == nil {
			if attempt > 0 {
				log.Printf("✅ sing-box instance created after removing %d invalid outbound(s)", attempt)
			}
			return instance, nil
		}

		// Check if this is an outbound initialization error we can recover from
		matches := outboundErrRe.FindStringSubmatch(err.Error())
		if matches == nil {
			return nil, fmt.Errorf("create sing-box instance: %w", err)
		}

		idx, convErr := strconv.Atoi(matches[1])
		if convErr != nil || idx < 0 || idx >= len(opts.Outbounds) {
			return nil, fmt.Errorf("create sing-box instance: %w", err)
		}

		badTag := opts.Outbounds[idx].Tag
		log.Printf("⚠️  Outbound '%s' failed sing-box validation: %v (removing and retrying)", badTag, err)

		// Remove the offending outbound
		opts.Outbounds = append(opts.Outbounds[:idx], opts.Outbounds[idx+1:]...)

		// Clean up pool outbounds that contained this tag
		var newOutbounds []option.Outbound
		var removedPoolTags []string
		for _, ob := range opts.Outbounds {
			if ob.Type == pool.Type {
				if poolOpts, ok := ob.Options.(*pool.Options); ok {
					poolOpts.Members = removeFromSlice(poolOpts.Members, badTag)
					delete(poolOpts.Metadata, badTag)

					// If the pool is now empty, remove it to avoid another validation error
					if len(poolOpts.Members) == 0 {
						log.Printf("⚠️  Removing empty pool '%s'", ob.Tag)
						removedPoolTags = append(removedPoolTags, ob.Tag)
						continue // skip adding this empty pool
					}
				}
			}
			newOutbounds = append(newOutbounds, ob)
		}
		opts.Outbounds = newOutbounds

		// Also remove any routes that pointed to the removed pools or the badTag
		if (len(removedPoolTags) > 0 || badTag != "") && opts.Route != nil {
			removedSet := make(map[string]bool)
			for _, t := range removedPoolTags {
				removedSet[t] = true
			}
			removedSet[badTag] = true

			var newRules []option.Rule
			for _, r := range opts.Route.Rules {
				// We expect DefaultRules in our builder
				if r.Type == C.RuleTypeDefault {
					outboundTarget := r.DefaultOptions.RuleAction.RouteOptions.Outbound
					if !removedSet[outboundTarget] {
						newRules = append(newRules, r)
					} else {
						// Remove this rule since it points to a deleted outbound
					}
				} else {
					newRules = append(newRules, r)
				}
			}
			opts.Route.Rules = newRules
		}
	}

	return nil, fmt.Errorf("create sing-box instance: too many invalid outbounds (exceeded %d retries)", maxRetries)
}

func (m *Manager) newBox(ctx context.Context, cfg *config.Config) (boxInstance, error) {
	if m.boxFactory != nil {
		return m.boxFactory(ctx, cfg)
	}
	return m.createBox(ctx, cfg)
}

// gracefulSwitch swaps the current box with a new one.
func (m *Manager) gracefulSwitch(newBox boxInstance) error {
	if newBox == nil {
		return errors.New("new box is nil")
	}

	m.mu.Lock()
	old := m.currentBox
	m.currentBox = newBox
	drainTimeout := m.drainTimeout
	m.mu.Unlock()

	if old != nil {
		go m.drainOldBox(old, drainTimeout)
	}

	m.logger.Infof("switched to new instance, draining old for %s", drainTimeout)
	return nil
}

// drainOldBox waits for drain timeout then closes the old box.
func (m *Manager) drainOldBox(oldBox boxInstance, timeout time.Duration) {
	if oldBox == nil {
		return
	}
	if timeout > 0 {
		time.Sleep(timeout)
	}
	if err := oldBox.Close(); err != nil {
		m.logger.Errorf("failed to close old instance: %v", err)
		return
	}
	m.logger.Infof("old instance closed after %s drain", timeout)
}

// removeFromSlice removes an element from a string slice.
func removeFromSlice(slice []string, element string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != element {
			result = append(result, s)
		}
	}
	return result
}

// waitForHealthCheck polls until enough nodes are available or timeout.
// ctx 取消（进程正在退出）时提前返回，避免关闭被启动健康检查拖住。
func (m *Manager) waitForHealthCheck(timeout time.Duration) error {
	if m.monitorMgr == nil || m.minAvailableNodes <= 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = defaultHealthCheckTimeout
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(healthCheckPollInterval)
	defer ticker.Stop()
	ctx := m.baseCtx

	for {
		available, total := m.availableNodeCount()
		if available >= m.minAvailableNodes {
			m.logger.Infof("health check passed: %d/%d nodes available", available, total)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout: %d/%d nodes available (need >= %d)", available, total, m.minAvailableNodes)
		}
		if ctx != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				continue
			}
		}
		<-ticker.C
	}
}

// availableNodeCount returns (available, total) node counts.
func (m *Manager) availableNodeCount() (int, int) {
	if m.monitorMgr == nil {
		return 0, 0
	}
	snapshots := m.monitorMgr.Snapshot()
	total := len(snapshots)
	available := 0
	for _, snap := range snapshots {
		if snap.InitialCheckDone && snap.Available {
			available++
		}
	}
	return available, total
}

// ensureMonitor initializes monitor manager and server if needed.
func (m *Manager) ensureMonitor(ctx context.Context) error {
	m.mu.Lock()
	if m.monitorMgr != nil {
		m.mu.Unlock()
		return nil
	}

	monitorMgr, err := monitor.NewManager(m.monitorCfg)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("init monitor manager: %w", err)
	}
	monitorMgr.SetLogger(monitorLoggerAdapter{logger: m.logger})
	m.monitorMgr = monitorMgr

	var serverToStart *monitor.Server
	if m.monitorCfg.Enabled {
		if m.monitorServer == nil {
			serverToStart = monitor.NewServer(m.monitorCfg, monitorMgr, log.Default())
			m.monitorServer = serverToStart
		}
		// Set config early so WebUI has data before Start() completes
		if m.monitorServer != nil && m.cfg != nil {
			m.monitorServer.SetConfig(m.cfg)
		}
		// Set NodeManager for config CRUD endpoints
		if m.monitorServer != nil {
			m.monitorServer.SetNodeManager(m)
		}
		// Note: StartPeriodicHealthCheck is called after nodes are registered in Start()
	}
	m.mu.Unlock()

	if serverToStart != nil {
		serverToStart.Start(ctx)
	}
	return nil
}

// applyConfigSettings extracts runtime settings from config.
func (m *Manager) applyConfigSettings(cfg *config.Config) {
	if cfg == nil {
		return
	}
	if cfg.SubscriptionRefresh.DrainTimeout > 0 {
		m.drainTimeout = cfg.SubscriptionRefresh.DrainTimeout
	} else if m.drainTimeout == 0 {
		m.drainTimeout = defaultDrainTimeout
	}
	m.minAvailableNodes = cfg.SubscriptionRefresh.MinAvailableNodes
}

// defaultLogger is the fallback logger using standard log.
type defaultLogger struct{}

func (defaultLogger) Infof(format string, args ...any) {
	log.Printf("[boxmgr] "+format, args...)
}

func (defaultLogger) Warnf(format string, args ...any) {
	log.Printf("[boxmgr] WARN: "+format, args...)
}

func (defaultLogger) Errorf(format string, args ...any) {
	log.Printf("[boxmgr] ERROR: "+format, args...)
}

// monitorLoggerAdapter adapts Logger to monitor.Logger interface.
type monitorLoggerAdapter struct {
	logger Logger
}

func (a monitorLoggerAdapter) Info(args ...any) {
	if a.logger != nil {
		a.logger.Infof("%s", fmt.Sprint(args...))
	}
}

func (a monitorLoggerAdapter) Warn(args ...any) {
	if a.logger != nil {
		a.logger.Warnf("%s", fmt.Sprint(args...))
	}
}

// --- NodeManager interface implementation ---

var errConfigUnavailable = errors.New("config is not initialized")

// ListConfigNodes returns a copy of all configured nodes.
func (m *Manager) ListConfigNodes(ctx context.Context) ([]config.NodeConfig, error) {
	_ = ctx
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.cfg == nil {
		return nil, errConfigUnavailable
	}
	return cloneNodes(m.cfg.Nodes), nil
}

// CreateNode adds a new node to the config and saves it.
func (m *Manager) CreateNode(ctx context.Context, node config.NodeConfig) (config.NodeConfig, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return config.NodeConfig{}, err
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cfg == nil {
		return config.NodeConfig{}, errConfigUnavailable
	}

	normalized, err := m.prepareNodeLocked(node, "")
	if err != nil {
		return config.NodeConfig{}, err
	}

	// Determine source: if subscriptions exist, new nodes go to nodes.txt (subscription source)
	// Otherwise, if nodes_file exists, use file source; else inline
	if len(m.cfg.Subscriptions) > 0 {
		normalized.Source = config.NodeSourceSubscription
	} else if m.cfg.NodesFile != "" {
		normalized.Source = config.NodeSourceFile
	} else {
		normalized.Source = config.NodeSourceInline
	}

	m.cfg.Nodes = append(m.cfg.Nodes, normalized)
	if err := m.cfg.Save(); err != nil {
		m.cfg.Nodes = m.cfg.Nodes[:len(m.cfg.Nodes)-1]
		return config.NodeConfig{}, fmt.Errorf("save config: %w", err)
	}
	return normalized, nil
}

// UpdateNode updates an existing node by name and saves the config.
func (m *Manager) UpdateNode(ctx context.Context, name string, node config.NodeConfig) (config.NodeConfig, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return config.NodeConfig{}, err
		}
	}

	name = strings.TrimSpace(name)
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cfg == nil {
		return config.NodeConfig{}, errConfigUnavailable
	}

	idx := m.nodeIndexLocked(name)
	if idx == -1 {
		return config.NodeConfig{}, monitor.ErrNodeNotFound
	}

	normalized, err := m.prepareNodeLocked(node, name)
	if err != nil {
		return config.NodeConfig{}, err
	}

	// Preserve the original source
	normalized.Source = m.cfg.Nodes[idx].Source

	prev := m.cfg.Nodes[idx]
	prevOverrides := cloneRegionOverrides(m.cfg.ManualRegionOverrides)
	m.cfg.Nodes[idx] = normalized
	if !strings.EqualFold(strings.TrimSpace(prev.URI), strings.TrimSpace(normalized.URI)) {
		m.cfg.RemoveRegionOverride(prev.URI)
	}
	if err := m.cfg.Save(); err != nil {
		m.cfg.Nodes[idx] = prev
		m.cfg.ManualRegionOverrides = prevOverrides
		return config.NodeConfig{}, fmt.Errorf("save config: %w", err)
	}
	return normalized, nil
}

// DeleteNode removes a node by name and saves the config.
func (m *Manager) DeleteNode(ctx context.Context, name string) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}

	name = strings.TrimSpace(name)
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cfg == nil {
		return errConfigUnavailable
	}

	idx := m.nodeIndexLocked(name)
	if idx == -1 {
		return monitor.ErrNodeNotFound
	}

	backup := cloneNodes(m.cfg.Nodes)
	overrideBackup := cloneRegionOverrides(m.cfg.ManualRegionOverrides)
	removed := m.cfg.Nodes[idx]
	m.cfg.Nodes = append(m.cfg.Nodes[:idx], m.cfg.Nodes[idx+1:]...)
	m.cfg.RemoveRegionOverride(removed.URI)
	if err := m.cfg.Save(); err != nil {
		m.cfg.Nodes = backup
		m.cfg.ManualRegionOverrides = overrideBackup
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

// TriggerReload reloads the sing-box instance with current config.
func (m *Manager) TriggerReload(ctx context.Context) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}

	m.mu.RLock()
	cfgCopy := m.copyConfigLocked()
	if cfgCopy == nil {
		m.mu.RUnlock()
		return errConfigUnavailable
	}
	portMap := m.cfg.BuildPortMap() // Preserve existing port assignments
	m.mu.RUnlock()
	return m.ReloadWithPortMap(cfgCopy, portMap)
}

// ApplyUpstreamProxy applies a runtime-only upstream_proxy override and reloads
// sing-box. Preferred (primary) config is preserved for Save/settings; only the
// box build uses the temporary value. Passing the preferred (or empty after
// preferred was set) clears the override and recovers to primary.
func (m *Manager) ApplyUpstreamProxy(ctx context.Context, upstream string) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	m.mu.Lock()
	if m.cfg == nil {
		m.mu.Unlock()
		return errConfigUnavailable
	}
	// Capture preferred once from current authoritative config if unset.
	if strings.TrimSpace(m.upstreamPreferred) == "" {
		m.upstreamPreferred = strings.TrimSpace(m.cfg.UpstreamProxy)
	}
	// If preferred is still empty, treat the first Apply as preferred capture
	// from the requested value so subsequent failover still has a primary.
	if strings.TrimSpace(m.upstreamPreferred) == "" {
		m.upstreamPreferred = strings.TrimSpace(upstream)
	}

	up := strings.TrimSpace(upstream)
	pref := strings.TrimSpace(m.upstreamPreferred)
	if up == "" || up == pref {
		m.upstreamRuntimeOverride = ""
	} else {
		m.upstreamRuntimeOverride = up
	}

	// Authoritative cfg always holds preferred primary.
	if pref != "" {
		m.cfg.UpstreamProxy = pref
	}
	cfgCopy := m.copyConfigLocked()
	portMap := m.cfg.BuildPortMap()
	m.mu.Unlock()
	return m.ReloadWithPortMap(cfgCopy, portMap)
}

// EffectiveUpstreamProxy returns the upstream actually used by the running box
// (runtime override if set, else preferred / cfg value).
func (m *Manager) EffectiveUpstreamProxy() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if o := strings.TrimSpace(m.upstreamRuntimeOverride); o != "" {
		return o
	}
	if p := strings.TrimSpace(m.upstreamPreferred); p != "" {
		return p
	}
	if m.cfg != nil {
		return strings.TrimSpace(m.cfg.UpstreamProxy)
	}
	return ""
}

// PreferredUpstreamProxy returns the configured primary upstream (persistable).
func (m *Manager) PreferredUpstreamProxy() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p := strings.TrimSpace(m.upstreamPreferred); p != "" {
		return p
	}
	if m.cfg != nil {
		return strings.TrimSpace(m.cfg.UpstreamProxy)
	}
	return ""
}

// SetPreferredUpstreamProxy updates the persistable primary upstream and clears
// any runtime failover override that no longer applies. Call when settings
// intentionally change upstream_proxy.
func (m *Manager) SetPreferredUpstreamProxy(upstream string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	up := strings.TrimSpace(upstream)
	m.upstreamPreferred = up
	if m.upstreamRuntimeOverride != "" && m.upstreamRuntimeOverride == up {
		m.upstreamRuntimeOverride = ""
	}
	if m.cfg != nil {
		m.cfg.UpstreamProxy = up
	}
}

// ReloadWithPortMap gracefully switches to a new configuration, preserving port assignments.
func (m *Manager) ReloadWithPortMap(newCfg *config.Config, portMap map[string]uint16) error {
	if newCfg == nil {
		return errors.New("new config is nil")
	}

	// 端口表合并：以 manager 当前端口表（CurrentPortMap）为准进行覆盖，调用方
	// 传入的旧表只补充当前表未知的条目。运行时重分配（reassignConflictingPort）
	// 只写进内存 newCfg，若不合并，上游持有一份搬移前端口写死的旧表（例如 app
	// 启动时持有的原始 cfg 副本），下一次重载又会退回原端口、再次撞冲突。
	merged := make(map[string]uint16, len(portMap))
	for k, v := range portMap {
		merged[k] = v
	}
	for k, v := range m.CurrentPortMap() {
		merged[k] = v // current wins
	}
	if len(merged) > 0 {
		if err := newCfg.NormalizeWithPortMap(merged); err != nil {
			return fmt.Errorf("normalize config with port map: %w", err)
		}
	}

	return m.Reload(newCfg)
}

// CurrentPortMap returns the current port mapping from the active configuration.
func (m *Manager) CurrentPortMap() map[string]uint16 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg == nil {
		return nil
	}
	return m.cfg.BuildPortMap()
}

// --- Helper functions ---

// portBindErrorRegex matches "listen tcp4 0.0.0.0:24282: bind: address already in use"
var portBindErrorRegex = regexp.MustCompile(`listen tcp[46]? [^:]+:(\d+): bind: address already in use`)

const transientPortConflictRetries = 3

var transientPortConflictBackoff = 200 * time.Millisecond

func shouldRetryTransientPortConflict(retry int) bool {
	return retry < transientPortConflictRetries
}

// extractPortFromBindError extracts the port number from a bind error message.
func extractPortFromBindError(err error) uint16 {
	if err == nil {
		return 0
	}
	matches := portBindErrorRegex.FindStringSubmatch(err.Error())
	if len(matches) < 2 {
		return 0
	}
	var port int
	fmt.Sscanf(matches[1], "%d", &port)
	if port > 0 && port <= 65535 {
		return uint16(port)
	}
	return 0
}

// reassignConflictingPort finds every node using the conflicting port and
// moves it to an available port. The same port can legitimately be shared by
// several nodes when duplicate URIs were collapsed onto one preserved port
// (NormalizeWithPortMap) or when the caller supplied a hand-written conflict;
// moving only the first match leaves the box failing on the same port forever.
func reassignConflictingPort(cfg *config.Config, conflictPort uint16) bool {
	// Build set of used ports
	usedPorts := make(map[uint16]bool)
	if cfg.Mode == "hybrid" {
		usedPorts[cfg.Listener.Port] = true
	}
	for _, node := range cfg.Nodes {
		usedPorts[node.Port] = true
	}

	address := cfg.MultiPort.Address
	if address == "" {
		address = "0.0.0.0"
	}
	reassigned := false
	for idx := range cfg.Nodes {
		if cfg.Nodes[idx].Port != conflictPort {
			continue
		}
		// Find next available port
		newPort := conflictPort + 1
		for usedPorts[newPort] || !config.IsPortAvailable(address, newPort) {
			newPort++
			if newPort > 65535 {
				log.Printf("❌ No available port found for node %q", cfg.Nodes[idx].Name)
				return reassigned
			}
		}
		log.Printf("⚠️  Port %d in use, reassigning node %q to port %d", conflictPort, cfg.Nodes[idx].Name, newPort)
		cfg.Nodes[idx].Port = newPort
		usedPorts[newPort] = true
		reassigned = true
	}
	return reassigned
}

// listenPort 描述一个将要被绑定的监听端口。
type listenPort struct {
	address string
	port    uint16
}

func (p listenPort) String() string {
	return fmt.Sprintf("%s:%d", p.address, p.port)
}

// configListenPorts 返回 newCfg 在 multi-port/hybrid 模式下会绑定的监听端口。
// 宁可多等也不漏：凡 Port>0 的节点都计入（含 free_proxy 池节点 —— builder 的
// 每节点 inbound 循环会跳过它们，但配置若给了 Port>0，多等一次无害；漏掉一个
// 真实监听端口则会重演"wait 无告警但 Start 撞端口"）。
func configListenPorts(cfg *config.Config) []listenPort {
	if cfg == nil {
		return nil
	}
	var ports []listenPort
	address := cfg.MultiPort.Address
	if address == "" {
		address = "0.0.0.0"
	}
	if cfg.Mode == "hybrid" && cfg.Listener.Port > 0 {
		listen := cfg.Listener.Address
		if listen == "" {
			listen = address
		}
		ports = append(ports, listenPort{address: listen, port: cfg.Listener.Port})
	}
	if cfg.Mode == "multi-port" || cfg.Mode == "hybrid" {
		seen := make(map[uint16]bool)
		for _, node := range cfg.Nodes {
			if node.Port == 0 {
				continue
			}
			if seen[node.Port] {
				continue
			}
			seen[node.Port] = true
			ports = append(ports, listenPort{address: address, port: node.Port})
		}
	}
	return ports
}

// waitForListenPortsReleased 轮询等待新配置要绑定的端口全部可绑，直到全部
// 释放、超时或 ctx 取消。返回超时/取消时仍未释放的端口；空表示端口已就绪。
// 这取代了"固定 sleep 500ms"的盲等，是构建新实例前的真实前置条件：
// 释放延迟不再被误判为端口冲突，也就不再触发无谓的端口重分配。
func waitForListenPortsReleased(ctx context.Context, cfg *config.Config, timeout time.Duration) []listenPort {
	ports := configListenPorts(cfg)
	if len(ports) == 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = defaultPortReleaseTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		remaining := portsStillBound(ports)
		if len(remaining) == 0 {
			return nil
		}
		if !time.Now().Before(deadline) {
			return remaining
		}
		select {
		case <-ctx.Done():
			return remaining
		case <-time.After(portReleasePollInterval):
		}
	}
}

// portsStillBound 返回当前仍不可绑定的端口（被其他进程/内核短暂占用）。
func portsStillBound(ports []listenPort) []listenPort {
	var remaining []listenPort
	for _, p := range ports {
		if !config.IsPortAvailable(p.address, p.port) {
			remaining = append(remaining, p)
		}
	}
	return remaining
}

func cloneNodes(nodes []config.NodeConfig) []config.NodeConfig {
	if len(nodes) == 0 {
		return []config.NodeConfig{} // Return empty slice, not nil, for proper JSON serialization
	}
	out := make([]config.NodeConfig, len(nodes))
	copy(out, nodes)
	return out
}

func cloneRegionOverrides(overrides map[string]string) map[string]string {
	if len(overrides) == 0 {
		return nil
	}
	out := make(map[string]string, len(overrides))
	for key, value := range overrides {
		out[key] = value
	}
	return out
}

func configHasSource(cfg *config.Config, source config.NodeSource) bool {
	if cfg == nil {
		return false
	}
	for _, node := range cfg.Nodes {
		if node.Source == source {
			return true
		}
	}
	return false
}

func configNodeTags(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	usedTags := make(map[string]int, len(cfg.Nodes))
	tags := make([]string, 0, len(cfg.Nodes))
	for _, node := range cfg.Nodes {
		baseTag := runtimeNodeTagBase(node.Name)
		if baseTag == "" {
			baseTag = fmt.Sprintf("node-%d", len(tags)+1)
		}
		tag := baseTag
		if count, exists := usedTags[baseTag]; exists {
			usedTags[baseTag] = count + 1
			tag = fmt.Sprintf("%s-%d", baseTag, count+1)
		} else {
			usedTags[baseTag] = 1
		}
		if tag == "" {
			continue
		}
		tags = append(tags, tag)
	}
	return tags
}

func mergeNodeTags(groups ...[]string) []string {
	seen := make(map[string]struct{})
	merged := make([]string, 0)
	for _, group := range groups {
		for _, tag := range group {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			if _, ok := seen[tag]; ok {
				continue
			}
			seen[tag] = struct{}{}
			merged = append(merged, tag)
		}
	}
	return merged
}

func runtimeNodeTagBase(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return ""
	}
	segments := strings.FieldsFunc(lower, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	return strings.Trim(strings.Join(segments, "-"), "-")
}

func (m *Manager) copyConfigLocked() *config.Config {
	if m.cfg == nil {
		return nil
	}
	cloned := *m.cfg
	cloned.Nodes = cloneNodes(m.cfg.Nodes)
	// Clone Subscriptions slice to avoid shared backing array issues
	if len(m.cfg.Subscriptions) > 0 {
		cloned.Subscriptions = make([]string, len(m.cfg.Subscriptions))
		copy(cloned.Subscriptions, m.cfg.Subscriptions)
	}
	cloned.SetFilePath(m.cfg.FilePath())
	// Box builds should use the effective (possibly failed-over) upstream.
	if o := strings.TrimSpace(m.upstreamRuntimeOverride); o != "" {
		cloned.UpstreamProxy = o
	} else if p := strings.TrimSpace(m.upstreamPreferred); p != "" {
		cloned.UpstreamProxy = p
	}
	return &cloned
}

// applyUpstreamForBoxBuildLocked mutates newCfg.UpstreamProxy to the effective
// (possibly failed-over) value used to build sing-box, and updates preferred
// when no override is active. Caller must hold m.mu.
func (m *Manager) applyUpstreamForBoxBuildLocked(newCfg *config.Config) {
	if newCfg == nil {
		return
	}
	cur := strings.TrimSpace(newCfg.UpstreamProxy)
	override := strings.TrimSpace(m.upstreamRuntimeOverride)
	if override == "" {
		if cur != "" {
			m.upstreamPreferred = cur
		} else if pref := strings.TrimSpace(m.upstreamPreferred); pref != "" {
			newCfg.UpstreamProxy = pref
		}
		return
	}
	// Failover active: adopt a new preferred if the incoming config carries a
	// primary that is not the temporary override (settings change mid-failover).
	if cur != "" && cur != override {
		m.upstreamPreferred = cur
	} else if strings.TrimSpace(m.upstreamPreferred) == "" && cur != "" {
		m.upstreamPreferred = cur
	}
	newCfg.UpstreamProxy = override
}

// restorePreferredUpstreamLocked ensures m.cfg always holds the persistable
// primary upstream after a reload that may have used a temporary override.
// Caller must hold m.mu.
func (m *Manager) restorePreferredUpstreamLocked() {
	if m.cfg == nil {
		return
	}
	if strings.TrimSpace(m.upstreamRuntimeOverride) == "" {
		if cur := strings.TrimSpace(m.cfg.UpstreamProxy); cur != "" {
			m.upstreamPreferred = cur
		}
	}
	if pref := strings.TrimSpace(m.upstreamPreferred); pref != "" {
		m.cfg.UpstreamProxy = pref
	}
}

func (m *Manager) nodeIndexLocked(name string) int {
	for idx, node := range m.cfg.Nodes {
		if node.Name == name {
			return idx
		}
	}
	return -1
}

func (m *Manager) portInUseLocked(port uint16, currentName string) bool {
	if port == 0 {
		return false
	}
	for _, node := range m.cfg.Nodes {
		if node.Name == currentName {
			continue
		}
		if node.Port == port {
			return true
		}
	}
	return false
}

func (m *Manager) nextAvailablePortLocked() uint16 {
	base := m.cfg.MultiPort.BasePort
	if base == 0 {
		base = 24000
	}
	used := make(map[uint16]struct{}, len(m.cfg.Nodes))
	for _, node := range m.cfg.Nodes {
		if node.Port > 0 {
			used[node.Port] = struct{}{}
		}
	}
	port := base
	for i := 0; i < 1<<16; i++ {
		if _, ok := used[port]; !ok && port != 0 {
			return port
		}
		port++
		if port == 0 {
			port = 1
		}
	}
	return base
}

func (m *Manager) prepareNodeLocked(node config.NodeConfig, currentName string) (config.NodeConfig, error) {
	node.Name = strings.TrimSpace(node.Name)
	node.URI = strings.TrimSpace(node.URI)

	if node.URI == "" {
		return config.NodeConfig{}, fmt.Errorf("%w: URI 不能为空", monitor.ErrInvalidNode)
	}
	if !isValidNodeURI(node.URI) {
		return config.NodeConfig{}, fmt.Errorf("%w: URI 格式无效", monitor.ErrInvalidNode)
	}

	// Extract name from URI if not provided
	if node.Name == "" {
		if currentName != "" {
			node.Name = currentName
		} else {
			node.Name = config.ExtractNodeName(node.URI)
		}
		// Fallback to auto-generated name
		if node.Name == "" {
			node.Name = fmt.Sprintf("node-%d", len(m.cfg.Nodes)+1)
		}
	}

	// Check for name conflict (excluding current node when updating)
	if idx := m.nodeIndexLocked(node.Name); idx != -1 {
		if currentName == "" || m.cfg.Nodes[idx].Name != currentName {
			return config.NodeConfig{}, fmt.Errorf("%w: 节点 %s 已存在", monitor.ErrNodeConflict, node.Name)
		}
	}

	// Handle multi-port / hybrid mode: assign dedicated listener ports.
	if m.cfg.Mode == "multi-port" || m.cfg.Mode == "hybrid" {
		if node.Port == 0 {
			node.Port = m.nextAvailablePortLocked()
		} else if m.portInUseLocked(node.Port, currentName) {
			return config.NodeConfig{}, fmt.Errorf("%w: 端口 %d 已被占用", monitor.ErrNodeConflict, node.Port)
		}
		if node.Username == "" {
			node.Username = m.cfg.MultiPort.Username
			node.Password = m.cfg.MultiPort.Password
		}
	}

	return node, nil
}

func isValidNodeURI(raw string) bool {
	raw = strings.TrimSpace(raw)
	if !config.IsProxyURI(raw) {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks", "socks5", "socks5h", "ss", "ssr", "vmess", "vless", "trojan", "hysteria", "hysteria2", "hy2", "tuic", "anytls":
		return parsed.Host != "" || parsed.Opaque != ""
	default:
		return false
	}
}

// startAndroidProxyRouters starts unauthenticated per-region HTTP proxies for Android global proxy use.
func (m *Manager) startAndroidProxyRouters(ctx context.Context, cfg *config.Config) {
	m.mu.Lock()
	for _, router := range m.androidRouters {
		if router != nil {
			_ = router.Stop()
		}
	}
	m.androidRouters = nil
	m.mu.Unlock()

	listen := cfg.AndroidProxy.Listen
	if listen == "" {
		listen = cfg.Listener.Address
	}

	routers := make([]*geoip.Router, 0)
	skippedMissingPools := 0
	regionPorts := androidports.RegionPortsAvoiding(cfg.AndroidProxy, m.runtimeNodePorts(cfg))
	allocatedPorts := make(map[uint16]struct{}, len(regionPorts))
	actualRegionPorts := make(map[string]uint16, len(regionPorts))
	for _, region := range androidports.RegionOrder() {
		poolTag := fmt.Sprintf("pool-%s", region)
		port := regionPorts[region]
		if port == 0 {
			skippedMissingPools++
			continue
		}
		dialer, ok := pool.GetDialer(poolTag)
		if !ok {
			skippedMissingPools++
			continue
		}
		port = nextAndroidListenPort(listen, port, allocatedPorts)
		if port == 0 {
			m.logger.Warnf("failed to find available android proxy port for region %s", region)
			continue
		}
		routerCfg := geoip.RouterConfig{Listen: listen, Port: port}
		router := geoip.NewRouter(routerCfg, nil)
		router.SetGlobalPool(dialer)
		if err := router.Start(ctx); err != nil {
			m.logger.Warnf("failed to start android proxy for region %s on %d: %v", region, port, err)
			continue
		}
		m.logger.Infof("android proxy region %s started on http://%s:%d", region, listen, port)
		allocatedPorts[port] = struct{}{}
		actualRegionPorts[region] = port
		routers = append(routers, router)
	}
	if skippedMissingPools > 0 {
		m.logger.Infof("android proxy skipped %d region(s) without available pools", skippedMissingPools)
	}

	m.mu.Lock()
	if cfg.AndroidProxy.RegionPorts == nil {
		cfg.AndroidProxy.RegionPorts = make(map[string]uint16, len(actualRegionPorts))
	}
	for region, port := range actualRegionPorts {
		cfg.AndroidProxy.RegionPorts[region] = port
	}
	m.androidRouters = routers
	m.mu.Unlock()
}

func nextAndroidListenPort(listen string, start uint16, allocated map[uint16]struct{}) uint16 {
	if start == 0 {
		return 0
	}
	host := strings.TrimSpace(listen)
	if host == "" {
		host = "127.0.0.1"
	}
	for offset := 0; offset < 65535; offset++ {
		portInt := int(start) + offset
		for portInt > 65535 {
			portInt -= 65535
		}
		if portInt <= 0 {
			continue
		}
		port := uint16(portInt)
		if _, ok := allocated[port]; ok {
			continue
		}
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(int(port))))
		if err != nil {
			continue
		}
		_ = ln.Close()
		return port
	}
	return 0
}

func (m *Manager) runtimeNodePorts(cfg *config.Config) []uint16 {
	ports := make([]uint16, 0)
	if cfg != nil {
		for _, node := range cfg.Nodes {
			if node.Port != 0 {
				ports = append(ports, node.Port)
			}
		}
		if cfg.MultiPort.BasePort != 0 && (cfg.Mode == "multi-port" || cfg.Mode == "hybrid") && len(cfg.Nodes) > 0 {
			reserveCount := len(cfg.Nodes) + len(androidports.RegionOrder()) + 64
			for i := 0; i < reserveCount; i++ {
				portInt := int(cfg.MultiPort.BasePort) + i
				if portInt > 0 && portInt <= 65535 {
					ports = append(ports, uint16(portInt))
				}
			}
		}
	}
	if m != nil && m.monitorMgr != nil {
		for _, snap := range m.monitorMgr.Snapshot() {
			if snap.Port != 0 {
				ports = append(ports, snap.Port)
			}
		}
	}
	return ports
}
