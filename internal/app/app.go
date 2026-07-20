package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"easy_proxies/internal/boxmgr"
	"easy_proxies/internal/cloudflarecheck"
	"easy_proxies/internal/config"
	"easy_proxies/internal/freepromote"
	"easy_proxies/internal/geoip"
	"easy_proxies/internal/monitor"
	"easy_proxies/internal/subscription"
)

// Run builds the runtime components from config and blocks until shutdown.
func Run(ctx context.Context, cfg *config.Config) error {
	// Build monitor config
	proxyUsername := cfg.Listener.Username
	proxyPassword := cfg.Listener.Password
	if cfg.Mode == "multi-port" || cfg.Mode == "hybrid" {
		proxyUsername = cfg.MultiPort.Username
		proxyPassword = cfg.MultiPort.Password
	}

	monitorCfg := monitor.Config{
		Enabled:       cfg.ManagementEnabled(),
		Listen:        cfg.Management.Listen,
		ProbeTarget:   cfg.Management.ProbeTarget,
		Password:      cfg.Management.Password,
		ProxyUsername: proxyUsername,
		ProxyPassword: proxyPassword,
		ExternalIP:    cfg.ExternalIP,
		APIKeys:       append([]config.APIKeyConfig(nil), cfg.Management.APIKeys...),
		CORSOrigins:   append([]string(nil), cfg.Management.CORSOrigins...),
		Governance:    monitor.DefaultGovernance(),
	}

	// Create and start BoxManager
	boxMgr := boxmgr.New(cfg, monitorCfg)
	if err := boxMgr.Start(ctx); err != nil {
		return fmt.Errorf("start box manager: %w", err)
	}
	defer boxMgr.Close()

	// Wire up config to monitor server for settings API
	if server := boxMgr.MonitorServer(); server != nil {
		server.SetConfig(cfg)
	}

	// Always create SubscriptionManager so WebUI can hot-reload subscription config
	subMgr := subscription.New(cfg, boxMgr)
	defer subMgr.Stop()

	// Start refresh loop only if subscriptions are already configured
	if cfg.SubscriptionRefresh.Enabled && len(cfg.Subscriptions) > 0 {
		subMgr.Start()
	}

	// Wire up subscription manager to monitor server for API endpoints
	if server := boxMgr.MonitorServer(); server != nil {
		server.SetSubscriptionRefresher(subMgr)
	}

	startFreeProxyCacheRefresh(ctx, cfg, boxMgr)
	startFreeProxyPromote(ctx, cfg, boxMgr)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-ctx.Done():
		fmt.Println("Context cancelled, initiating graceful shutdown...")
	case sig := <-sigCh:
		fmt.Printf("Received %s, initiating graceful shutdown...\n", sig)
	}

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Graceful shutdown sequence
	fmt.Println("Stopping subscription manager...")
	if subMgr != nil {
		subMgr.Stop()
	}

	fmt.Println("Stopping box manager...")
	if err := boxMgr.Close(); err != nil {
		fmt.Printf("Error closing box manager: %v\n", err)
	}

	// Wait for connections to drain
	fmt.Println("Waiting for connections to drain...")
	select {
	case <-time.After(2 * time.Second):
		fmt.Println("Graceful shutdown completed")
	case <-shutdownCtx.Done():
		fmt.Println("Shutdown timeout exceeded, forcing exit")
	}

	return nil
}

func startFreeProxyCacheRefresh(ctx context.Context, cfg *config.Config, boxMgr *boxmgr.Manager) {
	if cfg == nil || boxMgr == nil {
		return
	}
	cache := cfg.FreeProxyCache.Normalized(cfg.FilePath(), len(cfg.FreeProxySources) > 0)

	// If the cache filter is enabled, the startup path skipped live probing to
	// start sing-box immediately. Trigger a filtered reload in the background so
	// unresponsive cache nodes are culled without delaying startup.
	filter := cfg.FreeProxyFilter.Normalized()
	if cache.EnabledValue() && cache.AutoReloadValue() && filter.Enabled && len(cfg.FreeProxySources) > 0 {
		go func() {
			// Wait for the box manager to finish starting before triggering the
			// filtered reload; the manager may not be ready immediately.
			time.Sleep(3 * time.Second)
			fmt.Println("Starting background free proxy cache filter reload...")
			reloadedCfg, err := config.Load(cfg.FilePath())
			if err != nil {
				fmt.Fprintf(os.Stderr, "free proxy cache filter reload: load config failed: %v\n", err)
				return
			}
			if err := boxMgr.ReloadWithPortMap(reloadedCfg, cfg.BuildPortMap()); err != nil {
				fmt.Fprintf(os.Stderr, "free proxy cache filter reload failed: %v\n", err)
			}
		}()
	}

	if !cache.EnabledValue() || !cache.RefreshOnStartValue() || len(cfg.FreeProxySources) == 0 {
		return
	}
	go func() {
		fmt.Println("Starting background free proxy cache refresh...")
		count, err := cfg.RefreshFreeProxyCache(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "background free proxy cache refresh failed: %v\n", err)
			return
		}
		if count == 0 {
			return
		}
		if cache.AutoReloadValue() {
			reloadedCfg, err := config.Load(cfg.FilePath())
			if err != nil {
				fmt.Fprintf(os.Stderr, "free proxy cache reload config failed: %v\n", err)
				return
			}
			if err := boxMgr.ReloadWithPortMap(reloadedCfg, cfg.BuildPortMap()); err != nil {
				fmt.Fprintf(os.Stderr, "free proxy cache auto-reload failed: %v\n", err)
			}
		}
	}()
}

func startFreeProxyPromote(ctx context.Context, cfg *config.Config, boxMgr *boxmgr.Manager) {
	if cfg == nil || boxMgr == nil {
		return
	}
	snaps := freepromoteSnapshotSource{mgr: boxMgr}
	// Always wire CF checker: used for exit-region fill; demote gate still honors require_cloudflare.
	quality := freepromote.QualityChecker(freepromote.CloudflareChecker{Checker: cloudflarecheck.NewChecker(
		cloudflarecheck.WithTimeout(cfg.QualityCheck.Normalized().CloudflareTimeout),
		cloudflarecheck.WithMaxConcurrency(1),
	)})
	if db := cfg.GeoIP.DatabasePath; db != "" {
		freepromote.SetExitIPRegionLookup(func(ip string) geoip.RegionInfo {
			lkp, err := geoip.OpenExisting(db)
			if err != nil {
				return geoip.RegionInfo{}
			}
			defer lkp.Close()
			return lkp.LookupIP(ip)
		})
	}
	svc := freepromote.NewService(
		func() config.FreeProxyPromoteConfig {
			return boxMgr.FreeProxyPromoteSettings()
		},
		boxMgr,
		snaps,
		quality,
		func() freepromote.ListenAuth {
			host, user, pass := boxMgr.MultiPortListenAuth()
			return freepromote.ListenAuth{Host: host, Username: user, Password: pass}
		},
		nil,
	)
	svc.Start(ctx)
}

type freepromoteSnapshotSource struct {
	mgr *boxmgr.Manager
}

func (s freepromoteSnapshotSource) ListSnapshots() []freepromote.Snapshot {
	if s.mgr == nil || s.mgr.MonitorManager() == nil {
		return nil
	}
	raw := s.mgr.MonitorManager().Snapshot()
	out := make([]freepromote.Snapshot, 0, len(raw))
	for _, snap := range raw {
		out = append(out, freepromote.Snapshot{
			Name:             snap.Name,
			URI:              snap.URI,
			Source:           snap.Source,
			Port:             snap.Port,
			Available:        snap.Available,
			InitialCheckDone: snap.InitialCheckDone,
			Blacklisted:      snap.Blacklisted,
			SuccessCount:     snap.SuccessCount,
			FailureCount:     snap.FailureCount,
			LastLatencyMs:    snap.LastLatencyMs,
			LastSuccess:      snap.LastSuccess,
			LastFailure:      snap.LastFailure,
			LastError:        snap.LastError,
			Region:           snap.Region,
			Country:          snap.Country,
		})
	}
	return out
}
