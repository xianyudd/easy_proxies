package reputation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Checker struct {
	providers      []Provider
	cache          *Cache
	timeout        time.Duration
	maxConcurrency int
}
type Option func(*Checker)

func WithProviders(providers ...Provider) Option { return func(c *Checker) { c.providers = providers } }
func WithCache(cache *Cache) Option              { return func(c *Checker) { c.cache = cache } }
func WithTimeout(timeout time.Duration) Option   { return func(c *Checker) { c.timeout = timeout } }
func WithMaxConcurrency(n int) Option            { return func(c *Checker) { c.maxConcurrency = n } }
func NewChecker(opts ...Option) *Checker {
	c := &Checker{timeout: 8 * time.Second, maxConcurrency: 5, cache: NewCache(6 * time.Hour)}
	for _, opt := range opts {
		opt(c)
	}
	if c.timeout <= 0 {
		c.timeout = 8 * time.Second
	}
	if c.maxConcurrency <= 0 {
		c.maxConcurrency = 5
	}
	if c.cache == nil {
		c.cache = NewCache(6 * time.Hour)
	}
	if len(c.providers) == 0 {
		client := &http.Client{Timeout: c.timeout}
		c.providers = []Provider{NewIPAPIProvider(client), NewIPWhoisProvider(client)}
	}
	return c
}
func (c *Checker) LookupIP(ctx context.Context, ip string) (*Result, error) {
	ip = strings.TrimSpace(ip)
	if net.ParseIP(ip) == nil {
		return nil, fmt.Errorf("invalid ip")
	}
	if cached, ok := c.cache.Get(ip); ok {
		return cached, nil
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var errs []string
	for _, provider := range c.providers {
		result, err := provider.Lookup(ctx, ip)
		if err == nil && result != nil {
			if result.CheckedAt.IsZero() {
				result.CheckedAt = time.Now()
			}
			if result.RiskLevel == "" {
				Score(result, "", false, result.LatencyMS)
			}
			c.cache.Set(ip, result)
			return result, nil
		}
		if err != nil {
			errs = append(errs, provider.Name()+": "+err.Error())
		}
	}
	return nil, errors.New(strings.Join(errs, "; "))
}
func (c *Checker) CacheList() []Result { return c.cache.List() }
func (c *Checker) ClearCache()         { c.cache.Clear() }
func (c *Checker) ExitIPViaProxy(ctx context.Context, proxyURL string) (string, int64, error) {
	parsed, err := url.Parse(strings.TrimSpace(proxyURL))
	if err != nil {
		return "", 0, err
	}
	client := &http.Client{Timeout: c.timeout, Transport: &http.Transport{Proxy: http.ProxyURL(parsed)}}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org?format=json", nil)
	if err != nil {
		return "", 0, err
	}
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return "", latency, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", latency, fmt.Errorf("exit ip status %d", resp.StatusCode)
	}
	var raw struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return "", latency, err
	}
	if net.ParseIP(raw.IP) == nil {
		return "", latency, fmt.Errorf("invalid exit ip")
	}
	return raw.IP, latency, nil
}

type ProxyTarget struct {
	NodeName, NodeTag, Region, Host, Mode, ProxyURL string
	Port                                            uint16
}

func (c *Checker) CheckProxies(ctx context.Context, items []ProxyTarget, expectedCountry string) []NodeResult {
	sem := make(chan struct{}, c.maxConcurrency)
	results := make([]NodeResult, len(items))
	var wg sync.WaitGroup
	for i, item := range items {
		i, item := i, item
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = NodeResult{NodeName: item.NodeName, NodeTag: item.NodeTag, Region: item.Region, Host: item.Host, Port: item.Port, Mode: item.Mode}
			ip, lat, err := c.ExitIPViaProxy(ctx, item.ProxyURL)
			if err != nil {
				results[i].Error = err.Error()
				return
			}
			res, err := c.LookupIP(ctx, ip)
			if err != nil {
				results[i].Error = err.Error()
				results[i].Result = &Result{IP: ip, RiskScore: 50, RiskLevel: RiskLevel(50), CheckedAt: time.Now(), LatencyMS: lat, Error: err.Error()}
				return
			}
			cp := *res
			if lat > cp.LatencyMS {
				cp.LatencyMS = lat
			}
			Score(&cp, expectedCountry, false, cp.LatencyMS)
			results[i].Result = &cp
		}()
	}
	wg.Wait()
	return results
}
