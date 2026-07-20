package builder

import (
	"net/url"
	"strings"
	"testing"

	"easy_proxies/internal/config"

	"github.com/sagernet/sing-box/option"
)

func TestBuildNodeOutbound_Hysteria2PortRangeInRawURI(t *testing.T) {
	outbound, err := buildNodeOutbound("test-hy2", "hysteria2://secret@example.com:10000-20000?sni=hy2.example.com", false)
	if err != nil {
		t.Fatalf("build node outbound failed: %v", err)
	}

	opts, ok := outbound.Options.(*option.Hysteria2OutboundOptions)
	if !ok {
		t.Fatalf("expected *option.Hysteria2OutboundOptions, got %T", outbound.Options)
	}

	if opts.Server != "example.com" {
		t.Fatalf("expected server example.com, got %q", opts.Server)
	}
	if opts.ServerPort != 443 {
		t.Fatalf("expected default server port 443, got %d", opts.ServerPort)
	}
	if len(opts.ServerPorts) != 1 || opts.ServerPorts[0] != "10000:20000" {
		t.Fatalf("expected server ports [10000:20000], got %v", opts.ServerPorts)
	}
}

func TestBuildHysteria2Options_PortsFromQuery(t *testing.T) {
	u, err := url.Parse("hysteria2://secret@example.com:443?ports=10000-20000,30000")
	if err != nil {
		t.Fatalf("parse uri failed: %v", err)
	}

	opts, err := buildHysteria2Options(u, false)
	if err != nil {
		t.Fatalf("build hysteria2 options failed: %v", err)
	}

	if len(opts.ServerPorts) != 2 {
		t.Fatalf("expected 2 server ports, got %d (%v)", len(opts.ServerPorts), opts.ServerPorts)
	}
	if opts.ServerPorts[0] != "10000:20000" || opts.ServerPorts[1] != "30000" {
		t.Fatalf("unexpected server ports: %v", opts.ServerPorts)
	}
}

func TestBuild_UpstreamProxyBypassProtocolsSkipsHy2DetourOnly(t *testing.T) {
	opts, err := Build(&config.Config{
		Mode:                "pool",
		Listener:            config.ListenerConfig{Address: "127.0.0.1", Port: 2323},
		Pool:                config.PoolConfig{Mode: "sequential"},
		Management:          config.ManagementConfig{ClashAPIListen: "127.0.0.1:9090"},
		LogLevel:            "info",
		UpstreamProxy:       "socks5://127.0.0.1:7890",
		UpstreamProxyBypass: config.UpstreamProxyBypassConfig{Protocols: []string{"hysteria2", "hy2"}},
		Nodes: []config.NodeConfig{
			{Name: "hy2", URI: "hysteria2://secret@example.com:10000-20000?sni=hy2.example.com"},
			{Name: "vless", URI: "vless://00000000-0000-0000-0000-000000000000@example.org:443?security=tls&sni=example.org"},
		},
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	hy2Outbound := findOutbound(t, opts.Outbounds, "hy2")
	if got := outboundDetour(t, hy2Outbound); got != "" {
		t.Fatalf("hy2 detour = %q, want empty", got)
	}

	vlessOutbound := findOutbound(t, opts.Outbounds, "vless")
	if got := outboundDetour(t, vlessOutbound); got != "__upstream_proxy" {
		t.Fatalf("vless detour = %q, want __upstream_proxy", got)
	}
}

func TestBuild_UpstreamProxyAppliesToHy2WhenBypassUnset(t *testing.T) {
	opts, err := Build(&config.Config{
		Mode:          "pool",
		Listener:      config.ListenerConfig{Address: "127.0.0.1", Port: 2323},
		Pool:          config.PoolConfig{Mode: "sequential"},
		Management:    config.ManagementConfig{ClashAPIListen: "127.0.0.1:9090"},
		LogLevel:      "info",
		UpstreamProxy: "socks5://127.0.0.1:7890",
		Nodes: []config.NodeConfig{
			{Name: "hy2", URI: "hysteria2://secret@example.com:10000-20000?sni=hy2.example.com"},
		},
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	hy2Outbound := findOutbound(t, opts.Outbounds, "hy2")
	if got := outboundDetour(t, hy2Outbound); got != "__upstream_proxy" {
		t.Fatalf("hy2 detour = %q, want __upstream_proxy", got)
	}
}

func findOutbound(t *testing.T, outbounds []option.Outbound, tag string) option.Outbound {
	t.Helper()
	for _, outbound := range outbounds {
		if outbound.Tag == tag {
			return outbound
		}
	}
	t.Fatalf("outbound %q not found", tag)
	return option.Outbound{}
}

func outboundDetour(t *testing.T, outbound option.Outbound) string {
	t.Helper()
	wrapper, ok := outbound.Options.(option.DialerOptionsWrapper)
	if !ok {
		t.Fatalf("outbound %q options %T do not expose dialer options", outbound.Tag, outbound.Options)
	}
	return wrapper.TakeDialerOptions().Detour
}

func TestBuildHysteria2Options_ColonPortRangeInQuery(t *testing.T) {
	u, err := url.Parse("hysteria2://secret@example.com:443?ports=21000:21999&sni=www.bing.com&insecure=1")
	if err != nil {
		t.Fatal(err)
	}
	opts, err := buildHysteria2Options(u, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(opts.ServerPorts) == 0 {
		t.Fatalf("expected hop ports from 21000:21999, got %#v", opts.ServerPorts)
	}
	// sing-box style keeps colon ranges
	joined := strings.Join([]string(opts.ServerPorts), ",")
	if !strings.Contains(joined, "21000") || !strings.Contains(joined, "21999") {
		t.Fatalf("unexpected ports %#v", opts.ServerPorts)
	}
}
