package builder

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"easy_proxies/internal/config"

	"github.com/miekg/dns"
	"github.com/sagernet/sing/common/json/badoption"
	"golang.org/x/sync/singleflight"
)

// echCacheEntry stores a PEM-encoded ECH CONFIGS blob for one SNI.
type echCacheEntry struct {
	SNI       string    `json:"sni"`
	PEM       string    `json:"pem"`
	FetchedAt time.Time `json:"fetched_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// echConfigCache is a process-wide SNI → ECH CONFIGS PEM cache with disk persistence.
// It is the primary path for ECH: Build injects Config into OutboundECHOptions so
// sing-box never has to dial DoH through upstream_proxy at handshake time.
type echConfigCache struct {
	mu      sync.RWMutex
	entries map[string]echCacheEntry // key: lowercased SNI
	group   singleflight.Group
	path    string
}

const (
	echCacheMinTTL     = 5 * time.Minute
	echCacheMaxTTL     = 24 * time.Hour
	echCacheDefaultTTL = 1 * time.Hour
	echDNSTimeout      = 3 * time.Second
)

var (
	globalECHCache     *echConfigCache
	globalECHCacheOnce sync.Once
)

func getECHCache() *echConfigCache {
	globalECHCacheOnce.Do(func() {
		globalECHCache = &echConfigCache{
			entries: make(map[string]echCacheEntry),
			path:    resolveECHCachePath(),
		}
		if err := globalECHCache.load(); err != nil && !os.IsNotExist(err) {
			log.Printf("[ech_cache] load %s: %v", globalECHCache.path, err)
		}
	})
	return globalECHCache
}

func resolveECHCachePath() string {
	if d := strings.TrimSpace(os.Getenv("EASY_PROXIES_CACHE_DIR")); d != "" {
		return filepath.Join(d, "ech-configs.json")
	}
	// Container default mount root.
	if st, err := os.Stat("/app"); err == nil && st.IsDir() {
		return "/app/.cache/ech-configs.json"
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache", "easy_proxies", "ech-configs.json")
	}
	return filepath.Join(os.TempDir(), "easy_proxies-ech-configs.json")
}

func (c *echConfigCache) load() error {
	if c.path == "" {
		return nil
	}
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return err
	}
	var list []echCacheEntry
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range list {
		sni := strings.ToLower(strings.TrimSpace(e.SNI))
		if sni == "" || e.PEM == "" {
			continue
		}
		if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
			continue
		}
		c.entries[sni] = e
	}
	return nil
}

func (c *echConfigCache) persist() {
	if c.path == "" {
		return
	}
	c.mu.RLock()
	list := make([]echCacheEntry, 0, len(c.entries))
	now := time.Now()
	for _, e := range c.entries {
		if !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt) {
			continue
		}
		list = append(list, e)
	}
	c.mu.RUnlock()

	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, c.path)
}

// get returns a still-valid PEM for sni, or ("", false).
func (c *echConfigCache) get(sni string) (string, bool) {
	sni = strings.ToLower(strings.TrimSpace(sni))
	if sni == "" {
		return "", false
	}
	c.mu.RLock()
	e, ok := c.entries[sni]
	c.mu.RUnlock()
	if !ok || e.PEM == "" {
		return "", false
	}
	if !e.ExpiresAt.IsZero() && time.Now().After(e.ExpiresAt) {
		return "", false
	}
	return e.PEM, true
}

func (c *echConfigCache) put(sni, pemBody string, ttl time.Duration) {
	sni = strings.ToLower(strings.TrimSpace(sni))
	if sni == "" || pemBody == "" {
		return
	}
	if ttl < echCacheMinTTL {
		ttl = echCacheMinTTL
	}
	if ttl > echCacheMaxTTL {
		ttl = echCacheMaxTTL
	}
	now := time.Now()
	e := echCacheEntry{
		SNI:       sni,
		PEM:       pemBody,
		FetchedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	c.mu.Lock()
	c.entries[sni] = e
	c.mu.Unlock()
	c.persist()
}

// GetOrFetch returns PEM-encoded ECH CONFIGS for sni. On cache miss it queries
// public UDP resolvers (no upstream_proxy detour).
func (c *echConfigCache) GetOrFetch(sni string) (string, error) {
	sni = strings.ToLower(strings.TrimSpace(sni))
	if sni == "" {
		return "", fmt.Errorf("empty sni")
	}
	if pemBody, ok := c.get(sni); ok {
		return pemBody, nil
	}
	v, err, _ := c.group.Do(sni, func() (any, error) {
		if pemBody, ok := c.get(sni); ok {
			return pemBody, nil
		}
		list, ttl, err := fetchECHConfigList(sni)
		if err != nil {
			return nil, err
		}
		pemBody := string(pem.EncodeToMemory(&pem.Block{
			Type:  "ECH CONFIGS",
			Bytes: list,
		}))
		c.put(sni, pemBody, ttl)
		return pemBody, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// Prefetch warms the cache for many SNIs concurrently (bounded).
func (c *echConfigCache) Prefetch(snis []string) (ok, fail int) {
	uniq := make([]string, 0, len(snis))
	seen := make(map[string]struct{}, len(snis))
	for _, s := range snis {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		// Skip already-fresh entries.
		if _, hit := c.get(s); hit {
			ok++
			continue
		}
		uniq = append(uniq, s)
	}
	if len(uniq) == 0 {
		return ok, fail
	}

	const workers = 8
	jobs := make(chan string, len(uniq))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < workers && i < len(uniq); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sni := range jobs {
				if _, err := c.GetOrFetch(sni); err != nil {
					mu.Lock()
					fail++
					mu.Unlock()
					log.Printf("[ech_cache] prefetch %s: %v", sni, err)
					continue
				}
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	for _, s := range uniq {
		jobs <- s
	}
	close(jobs)
	wg.Wait()
	return ok, fail
}

// pemToConfigLines splits a PEM blob into the Listable form sing-box expects.
func pemToConfigLines(pemBody string) badoption.Listable[string] {
	pemBody = strings.TrimSpace(pemBody)
	if pemBody == "" {
		return nil
	}
	lines := strings.Split(pemBody, "\n")
	out := make(badoption.Listable[string], 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// fetchECHConfigList queries DNS HTTPS(type 65) for sni and returns the raw
// ECHConfigList bytes plus a suggested cache TTL.
func fetchECHConfigList(sni string) ([]byte, time.Duration, error) {
	sni = strings.TrimSpace(sni)
	if sni == "" {
		return nil, 0, fmt.Errorf("empty sni")
	}
	// Prefer public resolvers that answer type 65; fall back to system default.
	servers := []string{
		"1.1.1.1:53",
		"8.8.8.8:53",
		"1.0.0.1:53",
		"9.9.9.9:53",
	}
	// Also try whatever /etc/resolv.conf points at (WSL stub often works).
	if cfg, err := dns.ClientConfigFromFile("/etc/resolv.conf"); err == nil {
		for _, srv := range cfg.Servers {
			addr := net.JoinHostPort(srv, cfg.Port)
			// Put system resolvers first — in the container they are the only
			// path that works when public UDP is filtered.
			servers = append([]string{addr}, servers...)
		}
	}

	var lastErr error
	for _, server := range uniqueStrings(servers) {
		list, ttl, err := exchangeHTTPSForECH(sni, server)
		if err != nil {
			lastErr = err
			continue
		}
		if len(list) > 0 {
			return list, ttl, nil
		}
	}
	if lastErr != nil {
		return nil, 0, lastErr
	}
	return nil, 0, fmt.Errorf("no ECH config in HTTPS records for %s", sni)
}

func exchangeHTTPSForECH(sni, server string) ([]byte, time.Duration, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(sni), dns.TypeHTTPS)
	msg.RecursionDesired = true

	client := &dns.Client{
		Net:     "udp",
		Timeout: echDNSTimeout,
	}
	in, _, err := client.Exchange(msg, server)
	if err != nil {
		// Retry over TCP in case of truncation / UDP filter.
		client.Net = "tcp"
		in, _, err = client.Exchange(msg, server)
		if err != nil {
			return nil, 0, err
		}
	}
	if in == nil {
		return nil, 0, fmt.Errorf("empty DNS response from %s", server)
	}
	if in.Rcode != dns.RcodeSuccess {
		return nil, 0, fmt.Errorf("rcode %s from %s", dns.RcodeToString[in.Rcode], server)
	}

	var (
		echList []byte
		ttl     = echCacheDefaultTTL
	)
	for _, rr := range in.Answer {
		httpsRR, ok := rr.(*dns.HTTPS)
		if !ok {
			continue
		}
		if rr.Header().Ttl > 0 {
			ttl = time.Duration(rr.Header().Ttl) * time.Second
		}
		for _, kv := range httpsRR.Value {
			echKV, ok := kv.(*dns.SVCBECHConfig)
			if !ok || len(echKV.ECH) == 0 {
				continue
			}
			echList = append([]byte(nil), echKV.ECH...)
			break
		}
		if len(echList) > 0 {
			break
		}
	}
	if len(echList) == 0 {
		return nil, 0, fmt.Errorf("no ech param in HTTPS answer from %s", server)
	}
	return echList, ttl, nil
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// echSNIFromQuery picks the SNI used for ECH DNS (TLS ServerName).
func echSNIFromQuery(query url.Values, fallbackHost string) string {
	if sni := strings.TrimSpace(query.Get("sni")); sni != "" {
		return sni
	}
	if peer := strings.TrimSpace(query.Get("peer")); peer != "" {
		return peer
	}
	return strings.TrimSpace(fallbackHost)
}

// prefetchECHForNodes collects SNIs from ECH-enabled share links and warms the cache.
func prefetchECHForNodes(nodes []config.NodeConfig) {
	if len(nodes) == 0 {
		return
	}
	snis := make([]string, 0, 16)
	for _, n := range nodes {
		uri := strings.TrimSpace(n.URI)
		if uri == "" || !strings.Contains(strings.ToLower(uri), "ech=") {
			continue
		}
		// Fragment-safe parse: strip #name before url.Parse query.
		raw := uri
		if i := strings.Index(raw, "#"); i >= 0 {
			raw = raw[:i]
		}
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		q := u.Query()
		if !isECHEnabled(q) {
			continue
		}
		if sni := echSNIFromQuery(q, u.Hostname()); sni != "" {
			snis = append(snis, sni)
		}
	}
	if len(snis) == 0 {
		return
	}
	ok, fail := getECHCache().Prefetch(snis)
	if ok > 0 || fail > 0 {
		log.Printf("[ech_cache] prefetch done: ok=%d fail=%d unique_sni≈%d", ok, fail, len(snis))
	}
}
