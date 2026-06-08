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
