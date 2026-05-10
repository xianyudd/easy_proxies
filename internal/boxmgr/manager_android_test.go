package boxmgr

import (
	"testing"

	"easy_proxies/internal/config"
	"easy_proxies/internal/geoip"
)

func TestAndroidRegionPortUsesOverride(t *testing.T) {
	t.Parallel()

	cfg := config.AndroidProxyConfig{
		BasePort: 13001,
		RegionPorts: map[string]uint16{
			geoip.RegionCH: 13019,
		},
	}

	if got := androidRegionPort(cfg, geoip.RegionCH, 8); got != 13019 {
		t.Fatalf("expected override port 13019, got %d", got)
	}
	if got := androidRegionPort(cfg, geoip.RegionAU, 9); got != 13010 {
		t.Fatalf("expected fallback port 13010, got %d", got)
	}
}
