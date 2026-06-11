package androidports

import (
	"easy_proxies/internal/config"
	"easy_proxies/internal/geoip"
)

const defaultAndroidBasePort uint16 = 13001

// RegionOrder returns the stable Android-compatible region order used for
// per-region Android global proxy endpoints.
func RegionOrder() []string {
	return geoip.AndroidCompatibleRegionOrder()
}

// RegionPorts resolves Android proxy ports while avoiding already assigned
// multi-port node ports. Explicit region_ports are preferred, but if an
// explicit/default port is already occupied by a node or another Android region
// the resolver advances to the next free TCP port.
func RegionPorts(cfg config.AndroidProxyConfig, nodes []config.NodeConfig) map[string]uint16 {
	return RegionPortsAvoiding(cfg, nodePortList(nodes))
}

// RegionPortsAvoiding resolves Android proxy ports while avoiding the provided
// runtime port list. Use this when ports are created by builders and therefore
// are not all present in config.Nodes.
func RegionPortsAvoiding(cfg config.AndroidProxyConfig, usedPorts []uint16) map[string]uint16 {
	order := RegionOrder()
	result := make(map[string]uint16, len(order))
	used := usedPortSet(usedPorts)
	allocated := make(map[uint16]struct{}, len(order))
	base := cfg.BasePort
	if base == 0 {
		base = defaultAndroidBasePort
	}
	for idx, region := range order {
		start := int(base) + idx
		if cfg.RegionPorts != nil {
			if explicit := cfg.RegionPorts[region]; explicit != 0 {
				start = int(explicit)
			}
		}
		port := nextAvailablePort(start, used, allocated)
		if port == 0 {
			continue
		}
		result[region] = port
		allocated[port] = struct{}{}
	}
	return result
}

// PortFor resolves a single region's Android proxy port using the same
// collision-avoidance rules as RegionPorts.
func PortFor(cfg config.AndroidProxyConfig, nodes []config.NodeConfig, region string) uint16 {
	return RegionPorts(cfg, nodes)[region]
}

// PortForAvoiding resolves a single region's Android proxy port from a runtime
// list of occupied ports.
func PortForAvoiding(cfg config.AndroidProxyConfig, usedPorts []uint16, region string) uint16 {
	return RegionPortsAvoiding(cfg, usedPorts)[region]
}

func nodePortList(nodes []config.NodeConfig) []uint16 {
	ports := make([]uint16, 0, len(nodes))
	for _, node := range nodes {
		if node.Port != 0 {
			ports = append(ports, node.Port)
		}
	}
	return ports
}

func usedPortSet(ports []uint16) map[uint16]struct{} {
	used := make(map[uint16]struct{}, len(ports))
	for _, port := range ports {
		if port != 0 {
			used[port] = struct{}{}
		}
	}
	return used
}

func nextAvailablePort(start int, used, allocated map[uint16]struct{}) uint16 {
	if start <= 0 {
		start = int(defaultAndroidBasePort)
	}
	for offset := 0; offset < 65535; offset++ {
		portInt := start + offset
		for portInt > 65535 {
			portInt -= 65535
		}
		if portInt <= 0 {
			continue
		}
		port := uint16(portInt)
		if _, ok := used[port]; ok {
			continue
		}
		if _, ok := allocated[port]; ok {
			continue
		}
		return port
	}
	return 0
}
