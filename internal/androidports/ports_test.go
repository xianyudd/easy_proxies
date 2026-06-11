package androidports

import (
	"testing"

	"easy_proxies/internal/config"
	"easy_proxies/internal/geoip"
)

func TestRegionPortsPreservesExistingDefaultsAndOverrides(t *testing.T) {
	t.Parallel()

	cfg := config.AndroidProxyConfig{
		BasePort: 13001,
		RegionPorts: map[string]uint16{
			geoip.RegionCH: 13019,
		},
	}

	ports := RegionPorts(cfg, nil)
	if got := ports[geoip.RegionCH]; got != 13019 {
		t.Fatalf("expected override port 13019, got %d", got)
	}
	if got := ports[geoip.RegionUS]; got != 13001 {
		t.Fatalf("expected us fallback port 13001, got %d", got)
	}
	if got := ports[geoip.RegionAU]; got != 13010 {
		t.Fatalf("expected au fallback port 13010, got %d", got)
	}
	if got := ports[geoip.RegionOther]; got != 13011 {
		t.Fatalf("expected existing other fallback port 13011, got %d", got)
	}
	if got := ports[geoip.RegionDE]; got != 13012 {
		t.Fatalf("expected germany fallback port 13012, got %d", got)
	}
}

func TestRegionPortsSkipsMultiPortNodeRange(t *testing.T) {
	t.Parallel()

	nodes := make([]config.NodeConfig, 0, 857)
	for port := uint16(30000); port <= 32856; port++ {
		nodes = append(nodes, config.NodeConfig{Name: "n", Port: port})
	}
	cfg := config.AndroidProxyConfig{BasePort: 30150}

	ports := RegionPorts(cfg, nodes)
	if got := ports[geoip.RegionUS]; got != 32857 {
		t.Fatalf("expected us to move after used multi-port range, got %d", got)
	}
	if got := ports[geoip.RegionJP]; got != 32858 {
		t.Fatalf("expected jp to follow us after used range, got %d", got)
	}
	for region, port := range ports {
		if port >= 30000 && port <= 32856 {
			t.Fatalf("region %s resolved to multi-port collision %d", region, port)
		}
	}
}

func TestRegionPortsSkipsConflictingExplicitOverride(t *testing.T) {
	t.Parallel()

	cfg := config.AndroidProxyConfig{
		BasePort: 13001,
		RegionPorts: map[string]uint16{
			geoip.RegionUS: 13001,
		},
	}
	nodes := []config.NodeConfig{{Name: "used", Port: 13001}}

	if got := PortFor(cfg, nodes, geoip.RegionUS); got != 13002 {
		t.Fatalf("expected explicit colliding override to move to 13002, got %d", got)
	}
}
