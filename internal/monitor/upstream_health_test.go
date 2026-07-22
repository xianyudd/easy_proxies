package monitor

import (
	"sync"
	"testing"

	"easy_proxies/internal/config"
)

func TestParseUpstreamHostPort(t *testing.T) {
	h, p, err := parseUpstreamHostPort("socks5://127.0.0.1:17890")
	if err != nil || h != "127.0.0.1" || p != "17890" {
		t.Fatalf("got %s %s %v", h, p, err)
	}
	h, p, err = parseUpstreamHostPort("http://10.0.0.1")
	if err != nil || h != "10.0.0.1" || p != "80" {
		t.Fatalf("default port got %s %s %v", h, p, err)
	}
}

func TestApplyUpstreamOverrideDoesNotMutateCfgSrc(t *testing.T) {
	primary := "socks5://127.0.0.1:17890"
	fallback := "socks5://192.168.8.6:7890"
	cfg := &config.Config{UpstreamProxy: primary}
	var applied []string
	fake := &fakeNodeManager{
		onApplyUpstream: func(u string) {
			applied = append(applied, u)
		},
	}
	s := &Server{cfgSrc: cfg, nodeMgr: fake, cfgMu: sync.RWMutex{}}

	if err := s.applyUpstreamOverride(fallback, true, "test failover"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if cfg.UpstreamProxy != primary {
		t.Fatalf("cfgSrc.UpstreamProxy mutated to %q, want primary %q", cfg.UpstreamProxy, primary)
	}
	if len(applied) != 1 || applied[0] != fallback {
		t.Fatalf("applied = %#v, want [%q]", applied, fallback)
	}
	if !s.upstreamHealth.usingFallback {
		t.Fatal("expected usingFallback=true")
	}
}
