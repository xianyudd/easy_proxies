package geoip

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/geoip2-golang"
)

// Region codes
const (
	RegionJP    = "jp"
	RegionKR    = "kr"
	RegionUS    = "us"
	RegionHK    = "hk"
	RegionTW    = "tw"
	RegionSG    = "sg"
	RegionIN    = "in"
	RegionAE    = "ae"
	RegionCH    = "ch"
	RegionAU    = "au"
	RegionDE    = "de"
	RegionGB    = "gb"
	RegionCA    = "ca"
	RegionFR    = "fr"
	RegionVN    = "vn"
	RegionRU    = "ru"
	RegionUA    = "ua"
	RegionTR    = "tr"
	RegionNG    = "ng"
	RegionOther = "other"
)

// Default GeoIP database download URL
const (
	DefaultGeoIPURL = "https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-Country.mmdb"
)

var (
	geoipDownloadURL     = DefaultGeoIPURL
	geoipDownloadTimeout = 6 * time.Second
)

// RegionInfo contains region details
type RegionInfo struct {
	Code    string // "jp", "kr", "us", "hk", "tw", "other"
	Country string // Full country name
	ISOCode string // ISO country code
}

// Lookup provides GeoIP lookup functionality
type Lookup struct {
	db             *geoip2.Reader
	mu             sync.RWMutex
	path           string
	updateInterval time.Duration
	stopChan       chan struct{}
	updateOnce     sync.Once
	dnsCache       map[string]RegionInfo
	cacheMu        sync.RWMutex
}

// EnsureDatabase checks if the GeoIP database exists, and downloads it if not
func EnsureDatabase(dbPath string) error {
	if dbPath == "" {
		return nil
	}

	// Check if file already exists and is valid
	info, err := os.Stat(dbPath)
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("geoip database path is not a file: %s", dbPath)
		}
		if info.Size() > 0 {
			return nil // File exists and has content
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat geoip database: %w", err)
	}

	log.Printf("📥 GeoIP database not found at %s, downloading...", dbPath)

	// Create parent directory if needed
	dir := filepath.Dir(dbPath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}

	// Download with bounded timeout. GeoIP is optional for runtime startup:
	// if the database cannot be fetched quickly, callers should fail fast and
	// continue with region routing disabled instead of blocking reload/startup.
	client := &http.Client{Timeout: geoipDownloadTimeout}
	req, err := http.NewRequest(http.MethodGet, geoipDownloadURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: unexpected status %s", resp.Status)
	}

	// Download to temporary file
	tempFile, err := os.CreateTemp(dir, ".geoip-*.mmdb")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if tempFile != nil {
			tempFile.Close()
		}
		if cleanup {
			os.Remove(tempPath)
		}
	}()

	// Copy with progress tracking
	progress := &progressWriter{total: resp.ContentLength}
	reader := io.TeeReader(resp.Body, progress)
	written, err := io.Copy(tempFile, reader)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Verify download completeness
	if resp.ContentLength > 0 && written < resp.ContentLength {
		return fmt.Errorf("incomplete download (%d/%d bytes)", written, resp.ContentLength)
	}

	// Sync and close temp file
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	tempFile = nil

	// Validate MMDB format
	if err := validateMMDB(tempPath); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, dbPath); err != nil {
		return fmt.Errorf("rename failed: %w", err)
	}
	cleanup = false

	log.Printf("✅ GeoIP database downloaded successfully to %s", dbPath)
	return nil
}

// progressWriter tracks download progress
type progressWriter struct {
	total       int64
	downloaded  int64
	lastPercent int64
	lastLog     time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n := len(b)
	p.downloaded += int64(n)

	now := time.Now()
	if p.total > 0 {
		percent := p.downloaded * 100 / p.total
		if percent >= 100 || percent >= p.lastPercent+10 || now.Sub(p.lastLog) >= 3*time.Second {
			log.Printf("   Progress: %d%% (%d/%d bytes)", percent, p.downloaded, p.total)
			p.lastPercent = percent
			p.lastLog = now
		}
	} else if now.Sub(p.lastLog) >= 3*time.Second {
		log.Printf("   Downloaded: %d bytes", p.downloaded)
		p.lastLog = now
	}

	return n, nil
}

// validateMMDB performs basic validation of MMDB file format
func validateMMDB(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() < 1024 {
		return fmt.Errorf("file too small (%d bytes)", info.Size())
	}

	// Check for MaxMind metadata in the last 8KB
	const tailSize int64 = 8192
	readSize := tailSize
	if info.Size() < readSize {
		readSize = info.Size()
	}
	if _, err := file.Seek(-readSize, io.SeekEnd); err != nil {
		return err
	}
	buf := make([]byte, readSize)
	if _, err := io.ReadFull(file, buf); err != nil && err != io.ErrUnexpectedEOF {
		return err
	}
	if !bytes.Contains(buf, []byte("MaxMind.com")) {
		return fmt.Errorf("missing MaxMind metadata")
	}

	return nil
}

// New creates a new GeoIP lookup instance
func New(dbPath string) (*Lookup, error) {
	return NewWithAutoUpdate(dbPath, 0)
}

// OpenExisting opens an existing GeoIP database without trying to download it.
// Use this for optional classification paths where startup/reload must not be
// blocked by a network fetch.
func OpenExisting(dbPath string) (*Lookup, error) {
	if strings.TrimSpace(dbPath) == "" {
		return &Lookup{}, nil
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("geoip database path is not a file: %s", dbPath)
	}
	if info.Size() <= 0 {
		return nil, fmt.Errorf("geoip database is empty: %s", dbPath)
	}
	db, err := geoip2.Open(dbPath)
	if err != nil {
		return nil, err
	}
	return &Lookup{
		db:       db,
		path:     dbPath,
		stopChan: make(chan struct{}),
		dnsCache: make(map[string]RegionInfo),
	}, nil
}

// NewWithAutoUpdate creates a new GeoIP lookup instance with auto-update support
func NewWithAutoUpdate(dbPath string, updateInterval time.Duration) (*Lookup, error) {
	if dbPath == "" {
		return &Lookup{}, nil
	}

	// Ensure database exists (download if needed)
	if err := EnsureDatabase(dbPath); err != nil {
		return nil, fmt.Errorf("ensure database: %w", err)
	}

	db, err := geoip2.Open(dbPath)
	if err != nil {
		return nil, err
	}

	lookup := &Lookup{
		db:             db,
		path:           dbPath,
		updateInterval: updateInterval,
		stopChan:       make(chan struct{}),
		dnsCache:       make(map[string]RegionInfo),
	}

	// Start auto-update goroutine if interval is set
	if updateInterval > 0 {
		go lookup.autoUpdateLoop()
		log.Printf("🔄 GeoIP auto-update enabled (interval: %v)", updateInterval)
	}

	return lookup, nil
}

// autoUpdateLoop periodically updates the GeoIP database
func (l *Lookup) autoUpdateLoop() {
	ticker := time.NewTicker(l.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := l.Update(); err != nil {
				log.Printf("⚠️  GeoIP auto-update failed: %v", err)
			}
		case <-l.stopChan:
			return
		}
	}
}

// Update downloads and reloads the GeoIP database
func (l *Lookup) Update() error {
	log.Printf("🔄 Updating GeoIP database...")

	// Download to temporary file
	tempPath := l.path + ".update"
	if err := downloadDatabase(tempPath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tempPath) // Clean up temp file

	// Validate the downloaded database
	if err := validateMMDB(tempPath); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Open new database
	newDB, err := geoip2.Open(tempPath)
	if err != nil {
		return fmt.Errorf("open new database: %w", err)
	}

	// Hot-swap the database
	l.mu.Lock()
	oldDB := l.db
	l.db = newDB
	l.mu.Unlock()

	// Close old database
	if oldDB != nil {
		oldDB.Close()
	}

	// Replace the old file with new one
	if err := os.Rename(tempPath, l.path); err != nil {
		log.Printf("⚠️  Failed to replace database file: %v (using in-memory version)", err)
	}

	log.Printf("✅ GeoIP database updated successfully")
	return nil
}

// downloadDatabase downloads the GeoIP database to the specified path
func downloadDatabase(dbPath string) error {
	// Create parent directory if needed
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}

	// Download with timeout
	client := &http.Client{Timeout: geoipDownloadTimeout}
	req, err := http.NewRequest(http.MethodGet, geoipDownloadURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}

	// Create temp file
	tempFile, err := os.CreateTemp(dir, ".geoip-download-*.mmdb")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if tempFile != nil {
			tempFile.Close()
		}
		if cleanup {
			os.Remove(tempPath)
		}
	}()

	// Copy with progress tracking
	progress := &progressWriter{total: resp.ContentLength}
	reader := io.TeeReader(resp.Body, progress)
	written, err := io.Copy(tempFile, reader)
	if err != nil {
		return err
	}

	// Verify download completeness
	if resp.ContentLength > 0 && written < resp.ContentLength {
		return fmt.Errorf("incomplete download (%d/%d bytes)", written, resp.ContentLength)
	}

	// Sync and close
	if err := tempFile.Sync(); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	tempFile = nil

	// Rename to target path
	if err := os.Rename(tempPath, dbPath); err != nil {
		return err
	}
	cleanup = false

	return nil
}

// Close closes the GeoIP database and stops auto-update
func (l *Lookup) Close() error {
	// Stop auto-update goroutine
	l.updateOnce.Do(func() {
		if l.stopChan != nil {
			close(l.stopChan)
		}
	})

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.db != nil {
		return l.db.Close()
	}
	return nil
}

// IsEnabled returns true if GeoIP lookup is available
func (l *Lookup) IsEnabled() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.db != nil
}

// LookupIP returns region info for an IP address
func (l *Lookup) LookupIP(ipStr string) RegionInfo {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.db == nil {
		return RegionInfo{Code: RegionOther, Country: "Unknown", ISOCode: ""}
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return RegionInfo{Code: RegionOther, Country: "Unknown", ISOCode: ""}
	}

	record, err := l.db.Country(ip)
	if err != nil {
		return RegionInfo{Code: RegionOther, Country: "Unknown", ISOCode: ""}
	}

	isoCode := countryISOCode(record.Country.IsoCode, record.RegisteredCountry.IsoCode, record.RepresentedCountry.IsoCode)
	country := record.Country.Names["en"]
	if country == "" {
		country = record.RegisteredCountry.Names["en"]
	}
	if country == "" {
		country = record.RepresentedCountry.Names["en"]
	}
	if country == "" {
		country = isoCode
	}

	return RegionInfo{
		Code:    isoCodeToRegion(isoCode),
		Country: country,
		ISOCode: isoCode,
	}
}

// LookupURI extracts server from URI and returns region info
func (l *Lookup) LookupURI(uri string) RegionInfo {
	host := extractHostFromURI(uri)
	if host == "" {
		return RegionInfo{Code: RegionOther, Country: "Unknown", ISOCode: ""}
	}

	// Check DNS cache first
	l.cacheMu.RLock()
	if cached, ok := l.dnsCache[host]; ok {
		l.cacheMu.RUnlock()
		return cached
	}
	l.cacheMu.RUnlock()

	// Resolve hostname to IP if needed
	ip := net.ParseIP(host)
	if ip == nil {
		// It's a hostname, try to resolve with timeout
		resolver := &net.Resolver{}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		ips, err := resolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 {
			result := RegionInfo{Code: RegionOther, Country: "Unknown", ISOCode: ""}
			// Cache failed lookups too to avoid repeated timeouts
			l.cacheMu.Lock()
			l.dnsCache[host] = result
			l.cacheMu.Unlock()
			return result
		}
		host = ips[0].IP.String()
	}

	result := l.LookupIP(host)

	// Cache the result
	l.cacheMu.Lock()
	l.dnsCache[extractHostFromURI(uri)] = result
	l.cacheMu.Unlock()

	return result
}

// extractHostFromURI extracts the host/IP from various proxy URI formats
func extractHostFromURI(uri string) string {
	lowerURI := strings.ToLower(uri)

	// VMess: typically base64-encoded JSON — must be handled specially
	if strings.HasPrefix(lowerURI, "vmess://") {
		return extractVMessHost(uri)
	}

	// Shadowsocks: ss://base64(method:password)@host:port#name
	if strings.HasPrefix(lowerURI, "ss://") {
		return extractSSHost(uri)
	}

	// SSR: base64 encoded
	if strings.HasPrefix(lowerURI, "ssr://") {
		return extractSSRHost(uri)
	}

	// All other standard URL-parseable schemes
	standardSchemes := []string{
		"vless://", "trojan://",
		"hysteria://", "hysteria2://", "hy2://",
		"anytls://", "tuic://",
		"socks5://", "socks://",
		"http://", "https://",
	}
	for _, scheme := range standardSchemes {
		if strings.HasPrefix(lowerURI, scheme) {
			parsed, err := url.Parse(uri)
			if err != nil {
				return ""
			}
			return parsed.Hostname()
		}
	}

	// Fallback: try url.Parse for any unknown scheme
	parsed, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// extractVMessHost extracts the server address from a vmess:// URI.
// VMess URIs come in two formats:
//  1. Base64 JSON: vmess://base64({"add":"1.2.3.4", ...})
//  2. URL format:  vmess://uuid@host:port?...
func extractVMessHost(uri string) string {
	// Strip scheme (case-insensitive)
	idx := strings.Index(uri, "://")
	if idx < 0 {
		return ""
	}
	encoded := uri[idx+3:]
	if encoded == "" {
		return ""
	}

	// Strip fragment (#name) if present — it breaks base64 decode
	if hashIdx := strings.Index(encoded, "#"); hashIdx >= 0 {
		encoded = encoded[:hashIdx]
	}

	// Try base64 JSON format first (most common)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(encoded)
	}
	if err == nil && len(decoded) > 0 {
		var obj struct {
			Add string `json:"add"`
		}
		if json.Unmarshal(decoded, &obj) == nil && obj.Add != "" {
			return obj.Add
		}
	}

	// Fallback: try URL format vmess://uuid@host:port?...
	parsed, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func extractSSHost(uri string) string {
	// Remove ss:// prefix
	content := strings.TrimPrefix(uri, "ss://")

	// Remove fragment (#name)
	if idx := strings.Index(content, "#"); idx != -1 {
		content = content[:idx]
	}

	// Check if it's the new format: base64@host:port
	if atIdx := strings.LastIndex(content, "@"); atIdx != -1 {
		hostPort := content[atIdx+1:]
		if colonIdx := strings.LastIndex(hostPort, ":"); colonIdx != -1 {
			return hostPort[:colonIdx]
		}
		return hostPort
	}

	// Old format: entire content is base64
	return ""
}

func extractSSRHost(uri string) string {
	// SSR is complex, skip for now - will be marked as "other"
	return ""
}

func countryISOCode(country, registeredCountry, representedCountry string) string {
	for _, code := range []string{country, registeredCountry, representedCountry} {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code != "" {
			return code
		}
	}
	return ""
}

// isoCodeToRegion maps ISO alpha-2 country codes to region codes.
func isoCodeToRegion(isoCode string) string {
	code := strings.ToLower(strings.TrimSpace(isoCode))
	if IsRegionCode(code) {
		return code
	}
	return RegionOther
}

// IsRegionCode reports whether code is a supported ISO alpha-2 region code.
func IsRegionCode(code string) bool {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == RegionOther || code == "" {
		return false
	}
	_, ok := iso3166CountryByCode[code]
	return ok
}

// CountryByRegion returns ISO country metadata for a region code.
func CountryByRegion(code string) (CountryInfo, bool) {
	item, ok := iso3166CountryByCode[strings.ToLower(strings.TrimSpace(code))]
	return item, ok
}

// RegionAliases returns display names and aliases useful for loose text matching.
func RegionAliases(code string) []string {
	item, ok := CountryByRegion(code)
	if !ok {
		return nil
	}
	aliases := []string{item.NameZH, item.NameEN}
	aliases = append(aliases, item.Aliases...)
	return aliases
}

// AllRegions returns all supported region codes plus other.
func AllRegions() []string {
	regions := make([]string, 0, len(iso3166Countries)+1)
	for _, item := range iso3166Countries {
		regions = append(regions, item.Code)
	}
	regions = append(regions, RegionOther)
	return regions
}

// AndroidCompatibleRegionOrder preserves the historical unauthenticated Android
// port order, then appends newly supported ISO regions.
func AndroidCompatibleRegionOrder() []string {
	ordered := []string{RegionUS, RegionJP, RegionHK, RegionSG, RegionTW, RegionKR, RegionIN, RegionAE, RegionCH, RegionAU, RegionOther, RegionDE, RegionGB, RegionCA}
	seen := make(map[string]bool, len(ordered))
	for _, region := range ordered {
		seen[region] = true
	}
	for _, region := range AllRegions() {
		if !seen[region] {
			ordered = append(ordered, region)
		}
	}
	return ordered
}

// RegionName returns the default Chinese display name for a region code.
func RegionName(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == RegionOther {
		return "其他"
	}
	if item, ok := iso3166CountryByCode[code]; ok {
		return item.NameZH
	}
	return "未知"
}

// RegionEnglishName returns the English ISO display name for a region code.
func RegionEnglishName(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == RegionOther {
		return "Other"
	}
	if item, ok := iso3166CountryByCode[code]; ok {
		return item.NameEN
	}
	return "Unknown"
}

// RegionEmoji returns the flag emoji for a region code
func RegionEmoji(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == RegionOther {
		return "🌍"
	}
	if item, ok := iso3166CountryByCode[code]; ok {
		return item.Emoji
	}
	return "❓"
}
