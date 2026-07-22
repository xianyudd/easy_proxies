package boxmgr

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"easy_proxies/internal/config"
	"easy_proxies/internal/monitor"
)

func TestApplyUpstreamProxyDoesNotPersistOverride(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	primary := "socks5://127.0.0.1:17890"
	fallback := "socks5://192.168.8.6:7890"
	initial := []byte("upstream_proxy: " + primary + "\n" +
		"upstream_proxy_fallback: " + fallback + "\n" +
		"mode: multi-port\n" +
		"multi_port:\n" +
		"  address: 127.0.0.1\n" +
		"  base_port: 18080\n" +
		"  username: u\n" +
		"  password: p\n" +
		"nodes:\n" +
		"  - name: n1\n" +
		"    uri: http://127.0.0.1:1\n")
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	var lastBuiltUpstream string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := New(cfg, monitor.Config{})
	mgr.boxFactory = func(_ context.Context, c *config.Config) (boxInstance, error) {
		if c == nil {
			return nil, errConfigUnavailable
		}
		lastBuiltUpstream = c.UpstreamProxy
		if c.Mode != "multi-port" {
			return nil, errConfigUnavailable
		}
		return fakeBox{}, nil
	}
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Close()

	if lastBuiltUpstream != primary {
		t.Fatalf("initial box upstream = %q, want %q", lastBuiltUpstream, primary)
	}
	if got := mgr.PreferredUpstreamProxy(); got != primary {
		t.Fatalf("preferred = %q, want %q", got, primary)
	}

	// Failover to fallback: box should use fallback; authoritative cfg stays primary.
	if err := mgr.ApplyUpstreamProxy(ctx, fallback); err != nil {
		t.Fatalf("ApplyUpstreamProxy failover: %v", err)
	}
	if lastBuiltUpstream != fallback {
		t.Fatalf("failover box upstream = %q, want %q", lastBuiltUpstream, fallback)
	}
	if got := mgr.EffectiveUpstreamProxy(); got != fallback {
		t.Fatalf("effective = %q, want %q", got, fallback)
	}
	if got := mgr.PreferredUpstreamProxy(); got != primary {
		t.Fatalf("preferred after failover = %q, want %q", got, primary)
	}
	if got := cfg.UpstreamProxy; got != primary {
		t.Fatalf("cfg.UpstreamProxy after failover = %q, want primary %q", got, primary)
	}

	// SaveSettings must keep primary on disk.
	if err := cfg.SaveSettings(); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.UpstreamProxy != primary {
		t.Fatalf("disk upstream after Save during failover = %q, want %q", reloaded.UpstreamProxy, primary)
	}

	// Recover to primary.
	if err := mgr.ApplyUpstreamProxy(ctx, primary); err != nil {
		t.Fatalf("ApplyUpstreamProxy recover: %v", err)
	}
	if lastBuiltUpstream != primary {
		t.Fatalf("recover box upstream = %q, want %q", lastBuiltUpstream, primary)
	}
	if got := mgr.EffectiveUpstreamProxy(); got != primary {
		t.Fatalf("effective after recover = %q, want %q", got, primary)
	}
	if got := cfg.UpstreamProxy; got != primary {
		t.Fatalf("cfg after recover = %q, want %q", got, primary)
	}
}

func TestApplyUpstreamForBoxBuildKeepsOverrideAcrossReload(t *testing.T) {
	primary := "socks5://127.0.0.1:17890"
	fallback := "socks5://192.168.8.6:7890"
	cfg := &config.Config{
		Mode:          "multi-port",
		UpstreamProxy: primary,
		MultiPort:     config.MultiPortConfig{Address: "127.0.0.1", BasePort: 18180, Username: "u", Password: "p"},
		Nodes:         []config.NodeConfig{{Name: "n1", URI: "http://127.0.0.1:1", Port: 18180, Source: config.NodeSourceInline}},
	}
	cfg.SetFilePath(t.TempDir() + "/config.yaml")

	var lastBuilt string
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := New(cfg, monitor.Config{})
	mgr.boxFactory = func(_ context.Context, c *config.Config) (boxInstance, error) {
		lastBuilt = c.UpstreamProxy
		return fakeBox{}, nil
	}
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Close()

	if err := mgr.ApplyUpstreamProxy(ctx, fallback); err != nil {
		t.Fatalf("failover: %v", err)
	}
	if lastBuilt != fallback {
		t.Fatalf("built=%q", lastBuilt)
	}

	// TriggerReload while failed over should rebuild with override still active.
	if err := mgr.TriggerReload(ctx); err != nil {
		t.Fatalf("TriggerReload: %v", err)
	}
	if lastBuilt != fallback {
		t.Fatalf("reload-while-failed-over built=%q, want %q", lastBuilt, fallback)
	}
	if got := cfg.UpstreamProxy; got != primary {
		t.Fatalf("cfg after reload = %q, want primary", got)
	}
}
