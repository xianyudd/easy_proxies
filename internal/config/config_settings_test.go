package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
