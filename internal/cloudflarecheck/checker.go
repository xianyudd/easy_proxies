package cloudflarecheck

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	Default204URL   = "http://cp.cloudflare.com/generate_204"
	DefaultTraceURL = "https://www.cloudflare.com/cdn-cgi/trace"
)

type Checker struct {
	timeout        time.Duration
	maxConcurrency int
	cache          *Cache
	challengeURL   string
}

type Option func(*Checker)

func WithTimeout(timeout time.Duration) Option { return func(c *Checker) { c.timeout = timeout } }
func WithMaxConcurrency(n int) Option          { return func(c *Checker) { c.maxConcurrency = n } }
func WithCache(cache *Cache) Option            { return func(c *Checker) { c.cache = cache } }
func WithChallengeURL(raw string) Option {
	return func(c *Checker) { c.challengeURL = strings.TrimSpace(raw) }
}

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
	return c
}

func (c *Checker) CacheList() []Result    { return c.cache.List() }
func (c *Checker) DeleteCache(key string) { c.cache.Delete(key) }
func (c *Checker) ClearCache()            { c.cache.Clear() }

func (c *Checker) Cache() *Cache {
	if c == nil {
		return nil
	}
	return c.cache
}

func (c *Checker) Settings() (time.Duration, int) {
	if c == nil {
		return 0, 0
	}
	return c.timeout, c.maxConcurrency
}

func (c *Checker) CheckTargets(ctx context.Context, targets []ProxyTarget) []Result {
	results := make([]Result, len(targets))
	if len(targets) == 0 {
		return results
	}
	workers := c.maxConcurrency
	if workers <= 0 {
		workers = 1
	}
	if workers > len(targets) {
		workers = len(targets)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = c.CheckTarget(ctx, targets[i])
			}
		}()
	}
	for i := range targets {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return results
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()
	return results
}

func (c *Checker) CheckTarget(ctx context.Context, target ProxyTarget) Result {
	key := target.NodeTag
	if key == "" {
		key = fmt.Sprintf("%s:%d", target.Host, target.Port)
	}
	if cached, ok := c.cache.Get(key); ok {
		return cached
	}
	result := Result{NodeName: target.NodeName, NodeTag: target.NodeTag, Region: target.Region, Host: target.Host, Port: target.Port, ChallengeStatus: "not_configured", CheckedAt: time.Now()}
	proxyURL, err := url.Parse(strings.TrimSpace(target.ProxyURL))
	if err != nil {
		result.Error = "invalid proxy url"
		result.Score, result.Level = Score(ScoreInput{ExpectedRegion: target.Region, HTTP204OK: false, TraceOK: false, Error: result.Error})
		return result
	}
	client := c.httpClient(proxyURL)
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	start := time.Now()
	status, _, err := fetchText(ctx, client, Default204URL)
	lat204 := time.Since(start).Milliseconds()
	if err == nil && status == http.StatusNoContent {
		result.HTTP204OK = true
	} else if err != nil {
		result.Error = err.Error()
	}

	start = time.Now()
	status, body, traceErr := fetchText(ctx, client, DefaultTraceURL)
	latTrace := time.Since(start).Milliseconds()
	if traceErr == nil && status >= 200 && status < 300 {
		trace := ParseTrace(body)
		result.TraceOK = true
		result.ExitIP = trace.IP
		result.CFLoc = trace.LOC
		result.CFColo = trace.COLO
		result.HTTPProtocol = trace.HTTP
		result.TLSVersion = trace.TLS
		result.Warp = trace.WARP
	} else if result.Error == "" && traceErr != nil {
		result.Error = traceErr.Error()
	}

	if c.challengeURL != "" {
		result.ChallengeStatus = c.checkChallenge(ctx, client)
	}
	result.LatencyMS = lat204 + latTrace
	result = finalizeResultScore(result, target.Region)
	c.cache.Set(key, result)
	return result
}

func finalizeResultScore(result Result, expectedRegion string) Result {
	result.Score, result.Level = Score(ScoreInput{ExpectedRegion: expectedRegion, CFLoc: result.CFLoc, HTTP204OK: result.HTTP204OK, TraceOK: result.TraceOK, ChallengeStatus: result.ChallengeStatus, LatencyMS: result.LatencyMS, Error: result.Error})
	if result.Level != "failed" {
		result.Error = ""
	}
	return result
}

func (c *Checker) httpClient(proxyURL *url.URL) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{Timeout: c.timeout, Transport: transport}
}

func fetchText(ctx context.Context, client *http.Client, endpoint string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("User-Agent", "easy-proxies-cf-score/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(body), nil
}

func (c *Checker) checkChallenge(ctx context.Context, client *http.Client) string {
	status, body, err := fetchText(ctx, client, c.challengeURL)
	if err != nil {
		return "error"
	}
	if status == http.StatusForbidden {
		return "forbidden"
	}
	lower := strings.ToLower(body)
	if strings.Contains(lower, "cf-chl") || strings.Contains(lower, "managed challenge") || strings.Contains(lower, "cf_clearance") {
		return "managed_challenge"
	}
	if status >= 200 && status < 400 {
		return "passed"
	}
	return fmt.Sprintf("http_%d", status)
}
