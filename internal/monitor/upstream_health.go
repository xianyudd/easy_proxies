package monitor

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// UpstreamHealth is a lightweight reachability snapshot for upstream_proxy.
type UpstreamHealth struct {
	URL              string    `json:"url,omitempty"`
	PrimaryURL       string    `json:"primary_url,omitempty"`
	FallbackURL      string    `json:"fallback_url,omitempty"`
	UsingFallback    bool      `json:"using_fallback"`
	OK               bool      `json:"ok"`
	LatencyMs        int64     `json:"latency_ms,omitempty"`
	Error            string    `json:"error,omitempty"`
	CheckedAt        time.Time `json:"checked_at,omitempty"`
	ConsecutiveFail  int       `json:"consecutive_failures"`
	ConsecutiveOK    int       `json:"consecutive_successes"`
	LastSwitch       time.Time `json:"last_switch,omitempty"`
	LastSwitchReason string    `json:"last_switch_reason,omitempty"`
}

type upstreamHealthState struct {
	mu              sync.RWMutex
	last            UpstreamHealth
	stop            chan struct{}
	once            sync.Once
	primary         string // configured preferred upstream (usually 17890)
	usingFallback   bool
	consecFail      int
	consecOK        int
	lastSwitch      time.Time
	lastSwitchReason string
	switching       bool
}

const (
	upstreamFailThreshold    = 3
	upstreamRecoverThreshold = 2
	upstreamSwitchCooldownounce   = 45 * time.Second
)

func (s *Server) upstreamHealthSnapshot() UpstreamHealth {
	if s == nil {
		return UpstreamHealth{}
	}
	s.upstreamHealth.mu.RLock()
	defer s.upstreamHealth.mu.RUnlock()
	h := s.upstreamHealth.last
	h.UsingFallback = s.upstreamHealth.usingFallback
	h.PrimaryURL = s.upstreamHealth.primary
	h.ConsecutiveFail = s.upstreamHealth.consecFail
	h.ConsecutiveOK = s.upstreamHealth.consecOK
	h.LastSwitch = s.upstreamHealth.lastSwitch
	h.LastSwitchReason = s.upstreamHealth.lastSwitchReason
	return h
}

// StartUpstreamHealthLoop probes primary upstream and optionally fails over.
func (s *Server) StartUpstreamHealthLoop(interval time.Duration) {
	if s == nil {
		return
	}
	if interval <= 0 {
		interval = 20 * time.Second
	}
	s.upstreamHealth.once.Do(func() {
		s.upstreamHealth.stop = make(chan struct{})
		// Capture primary at start (preferred relay).
		s.cfgMu.RLock()
		if s.cfgSrc != nil {
			s.upstreamHealth.primary = strings.TrimSpace(s.cfgSrc.UpstreamProxy)
		}
		s.cfgMu.RUnlock()
		go func() {
			t := time.NewTicker(interval)
			defer t.Stop()
			s.checkUpstreamOnce()
			for {
				select {
				case <-s.upstreamHealth.stop:
					return
				case <-t.C:
					s.checkUpstreamOnce()
				}
			}
		}()
	})
}

func (s *Server) StopUpstreamHealthLoop() {
	if s == nil {
		return
	}
	s.upstreamHealth.mu.Lock()
	if s.upstreamHealth.stop != nil {
		select {
		case <-s.upstreamHealth.stop:
		default:
			close(s.upstreamHealth.stop)
		}
	}
	s.upstreamHealth.mu.Unlock()
}

func (s *Server) checkUpstreamOnce() {
	s.cfgMu.RLock()
	var configured string
	var fallback string
	if s.cfgSrc != nil {
		configured = strings.TrimSpace(s.cfgSrc.UpstreamProxy)
		fallback = strings.TrimSpace(s.cfgSrc.UpstreamProxyFallback)
	}
	s.cfgMu.RUnlock()
	if fallback == "" {
		fallback = "socks5://192.168.8.6:7890"
	}

	s.upstreamHealth.mu.Lock()
	// Establish primary once from config when not failed over.
	if s.upstreamHealth.primary == "" {
		// If currently configured is fallback, still prefer non-empty previous primary only.
		if configured != "" && configured != fallback {
			s.upstreamHealth.primary = configured
		} else if configured != "" && s.upstreamHealth.primary == "" {
			// First run: configured is the preferred primary even if user pointed at 7890.
			s.upstreamHealth.primary = configured
		}
	}
	// If we are not on fallback, keep primary in sync with config (user may change settings).
	if !s.upstreamHealth.usingFallback && configured != "" && configured != fallback {
		s.upstreamHealth.primary = configured
	}
	primary := s.upstreamHealth.primary
	usingFallback := s.upstreamHealth.usingFallback
	s.upstreamHealth.mu.Unlock()

	// Health-check the preferred primary (not whatever is currently effective).
	target := primary
	if target == "" {
		target = configured
	}

	h := UpstreamHealth{
		URL:         target,
		PrimaryURL:  primary,
		FallbackURL: fallback,
		CheckedAt:   time.Now(),
	}
	if target == "" {
		h.OK = true
		h.Error = "upstream_proxy empty"
		s.storeUpstreamHealth(h, true)
		return
	}

	host, port, err := parseUpstreamHostPort(target)
	if err != nil {
		h.OK = false
		h.Error = err.Error()
		s.storeUpstreamHealth(h, false)
		s.maybeFailover(primary, fallback, "parse_error: "+err.Error())
		return
	}
	addr := net.JoinHostPort(host, port)
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	h.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		h.OK = false
		h.Error = err.Error()
		s.storeUpstreamHealth(h, false)
		if s.logger != nil {
			s.logger.Printf("[upstream_health] FAIL primary %s: %v", target, err)
		}
		s.maybeFailover(primary, fallback, err.Error())
		return
	}
	_ = conn.Close()
	h.OK = true
	s.storeUpstreamHealth(h, true)
	if usingFallback {
		s.maybeRecover(primary)
	}
}

func (s *Server) storeUpstreamHealth(h UpstreamHealth, ok bool) {
	s.upstreamHealth.mu.Lock()
	if ok {
		s.upstreamHealth.consecOK++
		s.upstreamHealth.consecFail = 0
	} else {
		s.upstreamHealth.consecFail++
		s.upstreamHealth.consecOK = 0
	}
	h.ConsecutiveFail = s.upstreamHealth.consecFail
	h.ConsecutiveOK = s.upstreamHealth.consecOK
	h.UsingFallback = s.upstreamHealth.usingFallback
	h.PrimaryURL = s.upstreamHealth.primary
	h.LastSwitch = s.upstreamHealth.lastSwitch
	h.LastSwitchReason = s.upstreamHealth.lastSwitchReason
	s.upstreamHealth.last = h
	s.upstreamHealth.mu.Unlock()
}

func (s *Server) maybeFailover(primary, fallback, reason string) {
	if s == nil || s.nodeMgr == nil {
		return
	}
	s.upstreamHealth.mu.Lock()
	if s.upstreamHealth.usingFallback || s.upstreamHealth.switching {
		s.upstreamHealth.mu.Unlock()
		return
	}
	if s.upstreamHealth.consecFail < upstreamFailThreshold {
		s.upstreamHealth.mu.Unlock()
		return
	}
	if !s.upstreamHealth.lastSwitch.IsZero() && time.Since(s.upstreamHealth.lastSwitch) < upstreamSwitchCooldownounce {
		s.upstreamHealth.mu.Unlock()
		return
	}
	if strings.TrimSpace(fallback) == "" || fallback == primary {
		s.upstreamHealth.mu.Unlock()
		return
	}
	s.upstreamHealth.switching = true
	s.upstreamHealth.mu.Unlock()

	if s.logger != nil {
		s.logger.Printf("[upstream_health] FAILOVER primary=%s -> fallback=%s reason=%s", primary, fallback, reason)
	}
	if err := s.applyUpstreamOverride(fallback, true, "failover: "+reason); err != nil {
		if s.logger != nil {
			s.logger.Printf("[upstream_health] failover reload failed: %v", err)
		}
		s.upstreamHealth.mu.Lock()
		s.upstreamHealth.switching = false
		s.upstreamHealth.mu.Unlock()
		return
	}
}

func (s *Server) maybeRecover(primary string) {
	if s == nil || s.nodeMgr == nil || strings.TrimSpace(primary) == "" {
		return
	}
	s.upstreamHealth.mu.Lock()
	if !s.upstreamHealth.usingFallback || s.upstreamHealth.switching {
		s.upstreamHealth.mu.Unlock()
		return
	}
	if s.upstreamHealth.consecOK < upstreamRecoverThreshold {
		s.upstreamHealth.mu.Unlock()
		return
	}
	if !s.upstreamHealth.lastSwitch.IsZero() && time.Since(s.upstreamHealth.lastSwitch) < upstreamSwitchCooldownounce {
		s.upstreamHealth.mu.Unlock()
		return
	}
	s.upstreamHealth.switching = true
	s.upstreamHealth.mu.Unlock()

	if s.logger != nil {
		s.logger.Printf("[upstream_health] RECOVER fallback -> primary=%s", primary)
	}
	if err := s.applyUpstreamOverride(primary, false, "recover"); err != nil {
		if s.logger != nil {
			s.logger.Printf("[upstream_health] recover reload failed: %v", err)
		}
		s.upstreamHealth.mu.Lock()
		s.upstreamHealth.switching = false
		s.upstreamHealth.mu.Unlock()
	}
}

// applyUpstreamOverride updates runtime upstream via NodeManager and records switch state.
func (s *Server) applyUpstreamOverride(upstream string, toFallback bool, reason string) error {
	if s.nodeMgr == nil {
		return fmt.Errorf("node manager not configured")
	}
	// Also mirror into cfgSrc for settings display (not persisted unless Save).
	s.cfgMu.Lock()
	if s.cfgSrc != nil {
		s.cfgSrc.UpstreamProxy = strings.TrimSpace(upstream)
	}
	s.cfgMu.Unlock()

	if err := s.nodeMgr.ApplyUpstreamProxy(context.Background(), upstream); err != nil {
		return err
	}

	s.upstreamHealth.mu.Lock()
	s.upstreamHealth.usingFallback = toFallback
	s.upstreamHealth.lastSwitch = time.Now()
	s.upstreamHealth.lastSwitchReason = reason
	s.upstreamHealth.switching = false
	s.upstreamHealth.consecFail = 0
	s.upstreamHealth.consecOK = 0
	s.upstreamHealth.mu.Unlock()
	return nil
}

func parseUpstreamHostPort(raw string) (host, port string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("empty upstream")
	}
	if !strings.Contains(raw, "://") {
		raw = "socks5://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", err
	}
	host = u.Hostname()
	port = u.Port()
	if host == "" {
		return "", "", fmt.Errorf("missing host in upstream %q", raw)
	}
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http", "https":
			port = "80"
		default:
			port = "1080"
		}
	}
	return host, port, nil
}
