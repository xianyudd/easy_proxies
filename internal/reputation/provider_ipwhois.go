package reputation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type IPWhoisProvider struct{ Client *http.Client }

func NewIPWhoisProvider(client *http.Client) *IPWhoisProvider {
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	return &IPWhoisProvider{Client: client}
}
func (p *IPWhoisProvider) Name() string { return "ipwho.is" }
func (p *IPWhoisProvider) Lookup(ctx context.Context, ip string) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://ipwho.is/%s?security=1", ip), nil)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ipwho.is status %d", resp.StatusCode)
	}
	var raw struct {
		Success              bool `json:"success"`
		Message, IP, Country string
		CountryCode          string `json:"country_code"`
		Region, City         string
		Connection           struct {
			ASN      int
			ISP, Org string
		}
		Security struct{ Anonymous, Proxy, VPN, Tor, Hosting bool }
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if !raw.Success {
		if raw.Message == "" {
			raw.Message = "lookup failed"
		}
		return nil, fmt.Errorf("ipwho.is: %s", raw.Message)
	}
	result := &Result{IP: ip, Country: raw.Country, CountryCode: raw.CountryCode, Region: raw.Region, City: raw.City, ASN: fmt.Sprintf("AS%d", raw.Connection.ASN), ISP: raw.Connection.ISP, Org: raw.Connection.Org, Hosting: raw.Security.Hosting, Proxy: raw.Security.Proxy || raw.Security.Anonymous, VPN: raw.Security.VPN, Tor: raw.Security.Tor, Provider: p.Name(), CheckedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	Score(result, "", false, result.LatencyMS)
	return result, nil
}
