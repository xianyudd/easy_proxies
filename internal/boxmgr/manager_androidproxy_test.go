package boxmgr

import (
	"net"
	"testing"

	"easy_proxies/internal/androidports"
	"easy_proxies/internal/config"
	"easy_proxies/internal/geoip"
)

func TestAndroidRegionPortsUseOverride(t *testing.T) {
	t.Parallel()

	cfg := config.AndroidProxyConfig{
		BasePort: 13001,
		RegionPorts: map[string]uint16{
			geoip.RegionCH: 13019,
		},
	}

	ports := androidports.RegionPorts(cfg, nil)
	if got := ports[geoip.RegionCH]; got != 13019 {
		t.Fatalf("expected override port 13019, got %d", got)
	}
	if got := ports[geoip.RegionAU]; got != 13010 {
		t.Fatalf("expected fallback port 13010, got %d", got)
	}
	if got := ports[geoip.RegionOther]; got != 13011 {
		t.Fatalf("expected existing other fallback port 13011, got %d", got)
	}
	if got := ports[geoip.RegionDE]; got != 13012 {
		t.Fatalf("expected germany fallback port 13012, got %d", got)
	}
}

func TestNextAndroidListenPortSkipsBoundAndAllocatedPorts(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	start := uint16(ln.Addr().(*net.TCPAddr).Port)
	if start > 65000 {
		t.Skipf("ephemeral port too close to uint16 limit: %d", start)
	}
	allocated := map[uint16]struct{}{
		start + 1: {},
	}

	got := nextAndroidListenPort("127.0.0.1", start, allocated)
	if got != start+2 {
		t.Fatalf("expected next free port %d, got %d", start+2, got)
	}
}
