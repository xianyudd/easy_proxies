package boxmgr

import (
	"context"
	"fmt"
	"testing"
	"time"

	"easy_proxies/internal/config"
	"easy_proxies/internal/monitor"
)

func TestReloadFailureRollbackPreservesMonitorNodes(t *testing.T) {
	oldCfg := &config.Config{
		Mode: "multi-port",
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			BasePort: 18080,
			Username: "u",
			Password: "p",
		},
		Nodes: []config.NodeConfig{{
			Name:   "old-node",
			URI:    "http://127.0.0.1:1",
			Port:   18080,
			Source: config.NodeSourceInline,
		}},
	}
	oldCfg.SetFilePath(t.TempDir() + "/config.yaml")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := New(oldCfg, monitor.Config{})
	mgr.boxFactory = fakeRegisteringBoxFactory(t, mgr)
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start old config: %v", err)
	}
	defer mgr.Close()

	waitForMonitorTags(t, mgr, []string{"old-node"})

	badCfg := &config.Config{
		Mode: "not-a-real-mode",
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			BasePort: 18081,
			Username: "u",
			Password: "p",
		},
		Nodes: []config.NodeConfig{{
			Name:   "new-node",
			URI:    "http://127.0.0.1:2",
			Port:   18081,
			Source: config.NodeSourceInline,
		}},
	}

	if err := mgr.Reload(badCfg); err == nil {
		t.Fatal("Reload with invalid mode succeeded, want error")
	}

	waitForMonitorTags(t, mgr, []string{"old-node"})
	if tags := monitorTags(mgr); contains(tags, "new-node") {
		t.Fatalf("failed reload should not leave new-node registered, got tags=%v", tags)
	}
}

func TestReloadKeepsOldMonitorNodesUntilNewBoxIsReady(t *testing.T) {
	oldCfg := &config.Config{
		Mode: "multi-port",
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			BasePort: 18080,
			Username: "u",
			Password: "p",
		},
		Nodes: []config.NodeConfig{{
			Name:   "old-node",
			URI:    "http://127.0.0.1:1",
			Port:   18080,
			Source: config.NodeSourceInline,
		}},
	}
	oldCfg.SetFilePath(t.TempDir() + "/config.yaml")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := New(oldCfg, monitor.Config{})
	sawOldDuringNewBox := false
	mgr.boxFactory = func(_ context.Context, cfg *config.Config) (boxInstance, error) {
		if cfg == nil {
			return nil, fmt.Errorf("config is nil")
		}
		if cfg.Mode != "multi-port" {
			return nil, fmt.Errorf("unsupported mode %s", cfg.Mode)
		}
		if len(cfg.Nodes) > 0 && cfg.Nodes[0].Name == "new-node" && contains(monitorTags(mgr), "old-node") {
			sawOldDuringNewBox = true
		}
		mm := mgr.MonitorManager()
		if mm == nil {
			return nil, fmt.Errorf("monitor manager not initialized")
		}
		for _, tag := range configNodeTags(cfg) {
			mm.Register(monitor.NodeInfo{Tag: tag, Name: tag, Source: string(config.NodeSourceInline)})
		}
		return fakeBox{}, nil
	}
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start old config: %v", err)
	}
	defer mgr.Close()
	waitForMonitorTags(t, mgr, []string{"old-node"})

	newCfg := &config.Config{
		Mode: "multi-port",
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			BasePort: 18081,
			Username: "u",
			Password: "p",
		},
		Nodes: []config.NodeConfig{{
			Name:   "new-node",
			URI:    "http://127.0.0.1:2",
			Port:   18081,
			Source: config.NodeSourceInline,
		}},
	}

	if err := mgr.Reload(newCfg); err != nil {
		t.Fatalf("Reload new config: %v", err)
	}
	if !sawOldDuringNewBox {
		t.Fatal("old monitor node should remain visible until the new box is ready")
	}
	waitForMonitorTags(t, mgr, []string{"new-node"})
	if tags := monitorTags(mgr); contains(tags, "old-node") {
		t.Fatalf("successful reload should prune old-node, got tags=%v", tags)
	}
}

func fakeRegisteringBoxFactory(t *testing.T, mgr *Manager) func(context.Context, *config.Config) (boxInstance, error) {
	t.Helper()
	return func(_ context.Context, cfg *config.Config) (boxInstance, error) {
		if cfg == nil {
			return nil, fmt.Errorf("config is nil")
		}
		if cfg.Mode != "multi-port" {
			return nil, fmt.Errorf("unsupported mode %s", cfg.Mode)
		}
		mm := mgr.MonitorManager()
		if mm == nil {
			return nil, fmt.Errorf("monitor manager not initialized")
		}
		for _, tag := range configNodeTags(cfg) {
			mm.Register(monitor.NodeInfo{Tag: tag, Name: tag, Source: string(config.NodeSourceInline)})
		}
		return fakeBox{}, nil
	}
}

type fakeBox struct{}

func (fakeBox) Start() error { return nil }
func (fakeBox) Close() error { return nil }

func waitForMonitorTags(t *testing.T, mgr *Manager, want []string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		tags := monitorTags(mgr)
		ok := true
		for _, tag := range want {
			if !contains(tags, tag) {
				ok = false
				break
			}
		}
		if ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("monitor tags did not contain %v, got %v", want, monitorTags(mgr))
}

func monitorTags(mgr *Manager) []string {
	mm := mgr.MonitorManager()
	if mm == nil {
		return nil
	}
	snapshots := mm.Snapshot()
	tags := make([]string, 0, len(snapshots))
	for _, snap := range snapshots {
		tags = append(tags, snap.Tag)
	}
	return tags
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
