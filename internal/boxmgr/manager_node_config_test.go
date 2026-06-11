package boxmgr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easy_proxies/internal/config"
	"easy_proxies/internal/monitor"
)

func TestCreateNodeRejectsInvalidURI(t *testing.T) {
	cfg := &config.Config{Mode: "multi-port", MultiPort: config.MultiPortConfig{BasePort: 18080, Username: "u", Password: "p"}}
	cfg.SetFilePath(t.TempDir() + "/config.yaml")
	mgr := New(cfg, monitor.Config{})

	_, err := mgr.CreateNode(context.Background(), config.NodeConfig{Name: "bad", URI: "not-a-uri"})
	if !errors.Is(err, monitor.ErrInvalidNode) {
		t.Fatalf("CreateNode error = %v, want ErrInvalidNode", err)
	}
}

func TestUpdateNodeRejectsInvalidURI(t *testing.T) {
	cfg := &config.Config{
		Mode:      "multi-port",
		MultiPort: config.MultiPortConfig{BasePort: 18080, Username: "u", Password: "p"},
		Nodes:     []config.NodeConfig{{Name: "old", URI: "http://127.0.0.1:18080", Port: 18080, Source: config.NodeSourceInline}},
	}
	cfg.SetFilePath(t.TempDir() + "/config.yaml")
	mgr := New(cfg, monitor.Config{})

	_, err := mgr.UpdateNode(context.Background(), "old", config.NodeConfig{Name: "old", URI: "http://"})
	if !errors.Is(err, monitor.ErrInvalidNode) {
		t.Fatalf("UpdateNode error = %v, want ErrInvalidNode", err)
	}
	if got := cfg.Nodes[0].URI; !strings.EqualFold(got, "http://127.0.0.1:18080") {
		t.Fatalf("node URI changed after rejected update: %q", got)
	}
}

func TestUpdateNodeRemovesManualRegionOverrideForOldURI(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("nodes: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Mode:      "multi-port",
		MultiPort: config.MultiPortConfig{BasePort: 18080, Username: "u", Password: "p"},
		Nodes:     []config.NodeConfig{{Name: "old", URI: "http://127.0.0.1:18080#old", Port: 18080, Source: config.NodeSourceInline}},
	}
	cfg.SetFilePath(configPath)
	cfg.SetRegionOverride("http://127.0.0.1:18080#old", "jp")
	mgr := New(cfg, monitor.Config{})

	_, err := mgr.UpdateNode(context.Background(), "old", config.NodeConfig{Name: "old", URI: "http://127.0.0.1:18081#new"})
	if err != nil {
		t.Fatalf("UpdateNode error = %v", err)
	}
	if _, ok := cfg.RegionOverrideForURI("http://127.0.0.1:18080#old"); ok {
		t.Fatal("manual region override for old URI should be removed")
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.RegionOverrideForURI("http://127.0.0.1:18080#old"); ok {
		t.Fatal("manual region override for old URI should be removed from disk")
	}
}

func TestDeleteNodeRemovesManualRegionOverride(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("nodes: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Mode:      "multi-port",
		MultiPort: config.MultiPortConfig{BasePort: 18080, Username: "u", Password: "p"},
		Nodes:     []config.NodeConfig{{Name: "old", URI: "http://127.0.0.1:18080#old", Port: 18080, Source: config.NodeSourceInline}},
	}
	cfg.SetFilePath(configPath)
	cfg.SetRegionOverride("http://127.0.0.1:18080#old", "jp")
	mgr := New(cfg, monitor.Config{})

	if err := mgr.DeleteNode(context.Background(), "old"); err != nil {
		t.Fatalf("DeleteNode error = %v", err)
	}
	if _, ok := cfg.RegionOverrideForURI("http://127.0.0.1:18080#old"); ok {
		t.Fatal("manual region override for deleted node should be removed")
	}
	if len(cfg.ManualRegionOverrides) != 0 {
		t.Fatalf("manual region overrides should be empty after deleting only override, got %#v", cfg.ManualRegionOverrides)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(data)), "http://127.0.0.1:18080#old") {
		t.Fatal("manual region override for deleted node should be removed from disk")
	}
}

func TestTriggerReloadWithNilConfigReturnsError(t *testing.T) {
	mgr := New(nil, monitor.Config{})

	err := mgr.TriggerReload(context.Background())

	if err == nil || !strings.Contains(err.Error(), "config is not initialized") {
		t.Fatalf("TriggerReload error = %v, want config unavailable", err)
	}
}

func TestPortConflictRetriesBeforeReassign(t *testing.T) {
	for retry := 0; retry < transientPortConflictRetries; retry++ {
		if !shouldRetryTransientPortConflict(retry) {
			t.Fatalf("retry %d should wait before permanent reassignment", retry)
		}
	}
	if shouldRetryTransientPortConflict(transientPortConflictRetries) {
		t.Fatalf("retry %d should allow reassignment", transientPortConflictRetries)
	}
}

func TestConfigHasSourceDetectsRuntimeFreeProxyNodes(t *testing.T) {
	cfg := &config.Config{Nodes: []config.NodeConfig{
		{Name: "sub", URI: "http://127.0.0.1:18080", Source: config.NodeSourceSubscription},
	}}
	if configHasSource(cfg, config.NodeSourceFreeProxy) {
		t.Fatal("subscription-only config should not report free_proxy source")
	}
	cfg.Nodes = append(cfg.Nodes, config.NodeConfig{Name: "free", URI: "http://127.0.0.1:18081", Source: config.NodeSourceFreeProxy})
	if !configHasSource(cfg, config.NodeSourceFreeProxy) {
		t.Fatal("config with free_proxy node should report free_proxy source")
	}
}

func TestConfigNodeTagsReturnsRuntimeNodeNames(t *testing.T) {
	cfg := &config.Config{Nodes: []config.NodeConfig{
		{Name: " Sub A ", URI: "http://127.0.0.1:18080", Source: config.NodeSourceSubscription},
		{Name: "", URI: "http://127.0.0.1:18081", Source: config.NodeSourceFreeProxy},
		{Name: "Sub A", URI: "http://127.0.0.1:18082", Source: config.NodeSourceFreeProxy},
		{Name: "free-a", URI: "http://127.0.0.1:18083", Source: config.NodeSourceFreeProxy},
	}}

	got := configNodeTags(cfg)

	if len(got) != 4 || got[0] != "sub-a" || got[1] != "node-2" || got[2] != "sub-a-2" || got[3] != "free-a" {
		t.Fatalf("configNodeTags()=%#v, want sanitized unique runtime tags", got)
	}
}
