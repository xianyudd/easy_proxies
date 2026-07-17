package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadParsesUpstreamProxyBypassProtocols(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`upstream_proxy: socks5://127.0.0.1:7890
upstream_proxy_bypass:
  protocols:
    - hysteria2
    - hy2
nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UpstreamProxy != "socks5://127.0.0.1:7890" {
		t.Fatalf("upstream proxy = %q", cfg.UpstreamProxy)
	}
	if len(cfg.UpstreamProxyBypass.Protocols) != 2 || cfg.UpstreamProxyBypass.Protocols[0] != "hysteria2" || cfg.UpstreamProxyBypass.Protocols[1] != "hy2" {
		t.Fatalf("upstream proxy bypass protocols = %#v", cfg.UpstreamProxyBypass.Protocols)
	}
}

func TestSaveSettingsPersistsUpstreamProxyBypass(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`upstream_proxy: socks5://127.0.0.1:7890
upstream_proxy_bypass:
  protocols:
    - hysteria2
nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	cfg.UpstreamProxy = "socks5://127.0.0.1:7891"
	cfg.UpstreamProxyBypass.Protocols = []string{"hy2"}
	if err := cfg.SaveSettings(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.UpstreamProxy != "socks5://127.0.0.1:7891" {
		t.Fatalf("upstream proxy = %q", reloaded.UpstreamProxy)
	}
	if len(reloaded.UpstreamProxyBypass.Protocols) != 1 || reloaded.UpstreamProxyBypass.Protocols[0] != "hy2" {
		t.Fatalf("upstream proxy bypass protocols = %#v", reloaded.UpstreamProxyBypass.Protocols)
	}
}

func TestSaveSettingsPersistsAndroidProxy(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`android_proxy:
  enabled: true
  listen: 127.0.0.1
  base_port: 13001
  region_ports:
    US: 13010
nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	cfg.AndroidProxy.Listen = "0.0.0.0"
	cfg.AndroidProxy.BasePort = 14001
	if err := cfg.SaveSettings(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.AndroidProxy.Enabled || reloaded.AndroidProxy.Listen != "0.0.0.0" || reloaded.AndroidProxy.BasePort != 14001 || reloaded.AndroidProxy.RegionPorts["US"] != 13010 {
		t.Fatalf("android proxy settings not persisted or corrupted: %#v", reloaded.AndroidProxy)
	}
}

func TestManualRegionOverridesCanonicalizeAndPersist(t *testing.T) {
	cfg := &Config{}
	cfg.SetRegionOverride("  HTTP://1.2.3.4:8080#Node-A  ", " JP ")

	if got, ok := cfg.RegionOverrideForURI("http://1.2.3.4:8080#node-a"); !ok || got != "jp" {
		t.Fatalf("override = %q, %v; want jp,true", got, ok)
	}
	cfg.SetRegionOverride("", "us")
	cfg.SetRegionOverride("http://ignored.example:80", "")
	if len(cfg.ManualRegionOverrides) != 1 {
		t.Fatalf("empty uri/region should be ignored, got %#v", cfg.ManualRegionOverrides)
	}

	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	initial := []byte(`nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	loaded.SetRegionOverride("HTTP://9.9.9.9:80#Manual", "GB")
	if err := loaded.SaveSettings(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reloaded.RegionOverrideForURI("http://9.9.9.9:80#manual"); !ok || got != "gb" {
		t.Fatalf("persisted override = %q, %v; want gb,true", got, ok)
	}
}

func TestManualRegionOverridesCanBeRemoved(t *testing.T) {
	cfg := &Config{}
	cfg.SetRegionOverride("  HTTP://1.2.3.4:8080#Node-A  ", " JP ")

	if !cfg.RemoveRegionOverride("http://1.2.3.4:8080#node-a") {
		t.Fatal("RemoveRegionOverride should report removal")
	}
	if _, ok := cfg.RegionOverrideForURI("http://1.2.3.4:8080#node-a"); ok {
		t.Fatal("region override should be removed")
	}
	if cfg.ManualRegionOverrides != nil {
		t.Fatalf("empty override map should be reset to nil, got %#v", cfg.ManualRegionOverrides)
	}
	if cfg.RemoveRegionOverride("http://1.2.3.4:8080#node-a") {
		t.Fatal("removing a missing override should report false")
	}
}

func TestFreeProxyPromoteNormalizedDefaults(t *testing.T) {
	cfg := FreeProxyPromoteConfig{Enabled: true}.Normalized()
	if cfg.Interval != DefaultFreeProxyPromoteInterval {
		t.Fatalf("interval=%v", cfg.Interval)
	}
	if cfg.BatchSize != DefaultFreeProxyPromoteBatchSize {
		t.Fatalf("batch=%d", cfg.BatchSize)
	}
	if cfg.MaxPromoted != DefaultFreeProxyPromoteMaxPromoted {
		t.Fatalf("max=%d", cfg.MaxPromoted)
	}
	if cfg.NamePrefix != DefaultFreeProxyPromoteNamePrefix {
		t.Fatalf("prefix=%q", cfg.NamePrefix)
	}
	if cfg.MaxFailureCount != DefaultFreeProxyPromoteMaxFailureCount {
		t.Fatalf("max failure=%d", cfg.MaxFailureCount)
	}
	if cfg.RecentSuccessWithin != DefaultFreeProxyPromoteRecentSuccessWithin {
		t.Fatalf("recent success=%v", cfg.RecentSuccessWithin)
	}
	if cfg.FailedCooldown != DefaultFreeProxyPromoteFailedCooldown {
		t.Fatalf("failed cooldown=%v", cfg.FailedCooldown)
	}
	if !cfg.PostValidateValue() {
		t.Fatal("post validate default true")
	}
	if !cfg.DemoteOnFailValue() {
		t.Fatal("demote default true")
	}
}

func TestSaveSettingsPersistsFreeProxyPromote(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	// Use pool mode so Load does not probe host ports via IsPortAvailable.
	initial := []byte(`mode: pool
listener:
  port: 12323
nodes:
  - name: base
    uri: http://127.0.0.1:18080
`)
	if err := os.WriteFile(configPath, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.FreeProxyPromote.Enabled = true
	cfg.FreeProxyPromote.BatchSize = 3
	cfg.FreeProxyPromote.MaxPromoted = 7
	cfg.FreeProxyPromote.MaxFailureCount = 2
	cfg.FreeProxyPromote.RecentSuccessWithin = 15 * time.Minute
	cfg.FreeProxyPromote.FailedCooldown = 2 * time.Hour
	postValidate := false
	cfg.FreeProxyPromote.PostValidate = &postValidate
	cfg.FreeProxyPromote.RequireCloudflare = true
	if err := cfg.SaveSettings(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.FreeProxyPromote.Enabled || reloaded.FreeProxyPromote.BatchSize != 3 || reloaded.FreeProxyPromote.MaxPromoted != 7 {
		t.Fatalf("promote=%#v", reloaded.FreeProxyPromote)
	}
	if reloaded.FreeProxyPromote.MaxFailureCount != 2 || reloaded.FreeProxyPromote.RecentSuccessWithin != 15*time.Minute || reloaded.FreeProxyPromote.FailedCooldown != 2*time.Hour {
		t.Fatalf("promote stability=%#v", reloaded.FreeProxyPromote)
	}
	if reloaded.FreeProxyPromote.PostValidateValue() {
		t.Fatalf("post_validate should persist false: %#v", reloaded.FreeProxyPromote)
	}
}
