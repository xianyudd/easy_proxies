package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"easy_proxies/internal/cloudflarecheck"
	"easy_proxies/internal/quality"
	"easy_proxies/internal/reputation"
)

type monitorQualityTargetSource struct{ s *Server }

func newMonitorQualityTargetSource(s *Server) quality.TargetSource {
	return monitorQualityTargetSource{s: s}
}

func (m monitorQualityTargetSource) ListTargets(ctx context.Context, q quality.TargetQuery) ([]quality.Target, error) {
	if m.s == nil || m.s.mgr == nil {
		return nil, fmt.Errorf("monitor manager is not configured")
	}
	region := strings.ToLower(strings.TrimSpace(q.Region))
	if region == "" {
		region = "all"
	}
	source := strings.ToLower(strings.TrimSpace(q.Source))
	m.s.cfgMu.RLock()
	username := m.s.cfg.ProxyUsername
	password := m.s.cfg.ProxyPassword
	m.s.cfgMu.RUnlock()

	snaps := m.s.mgr.SnapshotFiltered(!q.IncludeUnavailable)
	targets := make([]quality.Target, 0, len(snaps))
	for _, snap := range snaps {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if snap.ListenAddress == "" || snap.Port == 0 {
			continue
		}
		if !q.IncludeUnavailable && (!snap.InitialCheckDone || !snap.Available || snap.Blacklisted) {
			continue
		}
		if region != "all" && !extractorSnapshotMatchesRegion(snap, region) {
			continue
		}
		snapSource := strings.ToLower(strings.TrimSpace(snap.Source))
		if snapSource == "" {
			snapSource = "unknown"
		}
		if source != "" && source != "all" && snapSource != source {
			continue
		}
		host := m.s.resolveLocalHost(snap.ListenAddress)
		auth := ""
		if username != "" || password != "" {
			auth = url.UserPassword(username, password).String() + "@"
		}
		targets = append(targets, quality.Target{
			Index:       len(targets),
			ID:          stableQualityTargetID(snap, host),
			NodeName:    snap.Name,
			NodeTag:     snap.Tag,
			Source:      snap.Source,
			ProxyURL:    fmt.Sprintf("http://%s%s:%d", auth, host, snap.Port),
			UpstreamURL: strings.TrimSpace(snap.URI),
			Protocol:    "http",
			Host:        host,
			Port:        int(snap.Port),
			Region:      extractorBestRegion(snap),
		})
	}
	if q.RetryFailed {
		targets = m.filterRetryFailedTargets(q.Kind, targets)
		for i := range targets {
			targets[i].Index = i
			targets[i].Retry = true
		}
	}
	return targets, nil
}

func (m monitorQualityTargetSource) filterRetryFailedTargets(kind quality.CheckKind, targets []quality.Target) []quality.Target {
	if m.s == nil {
		return targets
	}
	cfFailed := map[string]bool(nil)
	repFailed := map[string]bool(nil)
	if m.s.cfChecker != nil && (kind == "" || kind == quality.CheckCloudflare || kind == quality.CheckCombined) {
		cfFailed = failedCloudflareTags(m.s.cfChecker.CacheList())
	}
	if m.s.repChecker != nil && (kind == "" || kind == quality.CheckReputation || kind == quality.CheckCombined) {
		repFailed = failedReputationTags(m.s.repChecker.NodeResults())
	}
	out := make([]quality.Target, 0, len(targets))
	for _, target := range targets {
		if qualityTargetFailed(target, cfFailed) || qualityTargetFailed(target, repFailed) {
			out = append(out, target)
		}
	}
	return out
}

func qualityTargetFailed(target quality.Target, failed map[string]bool) bool {
	if len(failed) == 0 {
		return false
	}
	if target.NodeTag != "" && failed[target.NodeTag] {
		return true
	}
	if target.Host != "" && target.Port != 0 && failed[fmt.Sprintf("%s:%d", target.Host, target.Port)] {
		return true
	}
	return false
}

type monitorQualityRunner struct{ s *Server }

const quickCheckURL = "http://cp.cloudflare.com/generate_204"

func (r monitorQualityRunner) CheckQuick(ctx context.Context, target quality.Target) quality.Result {
	result := quality.Result{Kind: quality.CheckQuick, Target: target, TargetIndex: target.Index, TargetID: target.ID}
	proxyURL, err := url.Parse(strings.TrimSpace(qualityCheckProxyURL(target)))
	if err != nil || proxyURL.Host == "" {
		result.Status = "failed"
		result.Error = "invalid proxy url"
		result.Quick = map[string]any{"status": "failed", "failure_reason": "invalid_proxy_url", "error": result.Error}
		return result
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	client := &http.Client{Timeout: 2 * time.Second, Transport: transport}
	defer client.CloseIdleConnections()
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, quickCheckURL, nil)
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		result.Quick = map[string]any{"status": "failed", "failure_reason": classifyQualityError(result.Error), "error": result.Error}
		return result
	}
	start := time.Now()
	resp, err := client.Do(req)
	result.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		result.Quick = map[string]any{"status": "failed", "failure_reason": classifyQualityError(result.Error), "latency_ms": result.LatencyMS, "error": result.Error}
		return result
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		result.Status = "failed"
		result.Error = fmt.Sprintf("quick check status %d", resp.StatusCode)
		result.Quick = map[string]any{"status": "failed", "failure_reason": "invalid_response", "latency_ms": result.LatencyMS, "http_status": resp.StatusCode, "error": result.Error}
		return result
	}
	result.Status = "completed"
	result.Success = true
	result.Quick = map[string]any{"status": "ok", "latency_ms": result.LatencyMS, "http_status": resp.StatusCode}
	return result
}

func (r monitorQualityRunner) CheckCloudflare(ctx context.Context, target quality.Target) quality.Result {
	result := quality.Result{Kind: quality.CheckCloudflare, Target: target, TargetIndex: target.Index, TargetID: target.ID}
	if r.s == nil || r.s.cfChecker == nil {
		result.Error = "cloudflare checker is not configured"
		result.Status = "failed"
		return result
	}
	if target.Retry {
		r.s.cfChecker.DeleteCache(qualityCacheKey(target))
	}
	cf := r.s.cfChecker.CheckTarget(ctx, cloudflarecheck.ProxyTarget{NodeName: target.NodeName, NodeTag: target.NodeTag, Region: target.Region, Host: target.Host, Port: uint16(target.Port), ProxyURL: qualityCheckProxyURL(target)})
	result.Success = cf.Error == "" && cf.Level != "failed"
	result.Status = "completed"
	if !result.Success {
		result.Status = "failed"
		result.Error = cf.Error
		if result.Error == "" {
			result.Error = "cloudflare check failed"
		}
	}
	result.Score = cf.Score
	result.LatencyMS = cf.LatencyMS
	result.CF = map[string]any{"score": cf.Score, "level": cf.Level, "http_204_ok": cf.HTTP204OK, "trace_ok": cf.TraceOK, "cf_loc": cf.CFLoc, "exit_ip": cf.ExitIP, "error": cf.Error}
	return result
}

func classifyQualityError(err string) string {
	err = strings.ToLower(err)
	switch {
	case strings.Contains(err, "connection refused"):
		return "connect_refused"
	case strings.Contains(err, "timeout"), strings.Contains(err, "deadline exceeded"):
		return "timeout"
	case strings.Contains(err, "eof"):
		return "eof"
	case strings.Contains(err, "proxy authentication"), strings.Contains(err, "407"):
		return "proxy_auth_failed"
	case strings.Contains(err, "not an http proxy"):
		return "not_http_proxy"
	case strings.Contains(err, "no such host"):
		return "dns_error"
	default:
		return "network_error"
	}
}

func qualityCacheKey(target quality.Target) string {
	if target.NodeTag != "" {
		return target.NodeTag
	}
	if target.Host != "" && target.Port != 0 {
		return fmt.Sprintf("%s:%d", target.Host, target.Port)
	}
	return target.ID
}

func (r monitorQualityRunner) CheckReputation(ctx context.Context, target quality.Target, expectedCountry string) quality.Result {
	result := quality.Result{Kind: quality.CheckReputation, Target: target, TargetIndex: target.Index, TargetID: target.ID}
	if r.s == nil || r.s.repChecker == nil {
		result.Error = "reputation checker is not configured"
		result.Status = "failed"
		return result
	}
	items := r.s.repChecker.CheckProxies(ctx, []reputation.ProxyTarget{{NodeName: target.NodeName, NodeTag: target.NodeTag, Region: target.Region, Host: target.Host, Port: uint16(target.Port), Mode: "quality-direct", ProxyURL: qualityCheckProxyURL(target)}}, expectedCountry)
	if len(items) == 0 {
		result.Error = "empty reputation result"
		result.Status = "failed"
		return result
	}
	item := items[0]
	result.Success = item.Error == "" && item.Result != nil && item.Result.Error == ""
	result.Status = "completed"
	if !result.Success {
		result.Status = "failed"
		result.Error = item.Error
		if result.Error == "" && item.Result != nil {
			result.Error = item.Result.Error
		}
	}
	if item.Result != nil {
		result.Score = item.Result.RiskScore
		result.LatencyMS = item.Result.LatencyMS
		result.Reputation = map[string]any{"ip": item.Result.IP, "risk_score": item.Result.RiskScore, "risk_level": item.Result.RiskLevel, "country_code": item.Result.CountryCode, "error": item.Result.Error}
	}
	return result
}

func qualityCheckProxyURL(target quality.Target) string {
	upstream := strings.TrimSpace(target.UpstreamURL)
	if isHTTPTransportProxyURL(upstream) {
		return upstream
	}
	return target.ProxyURL
}

func isHTTPTransportProxyURL(raw string) bool {
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5", "socks5h":
		return parsed.Host != ""
	default:
		return false
	}
}

func stableQualityTargetID(snap Snapshot, host string) string {
	raw := strings.Join([]string{snap.Source, snap.URI, host, fmt.Sprint(snap.Port), snap.Region}, "|")
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}
