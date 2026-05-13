package reputation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type IPAPIProvider struct{ Client *http.Client }

func NewIPAPIProvider(client *http.Client) *IPAPIProvider {
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	return &IPAPIProvider{Client: client}
}
func (p *IPAPIProvider) Name() string { return "ip-api" }
func (p *IPAPIProvider) Lookup(ctx context.Context, ip string) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://ip-api.com/json/%s?fields=status,message,country,countryCode,regionName,city,as,isp,org,proxy,hosting", ip), nil)
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
		return nil, fmt.Errorf("ip-api status %d", resp.StatusCode)
	}
	var raw struct {
		Status, Message, Country, CountryCode, RegionName, City, AS, ISP, Org string
		Proxy, Hosting                                                        bool
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if raw.Status != "success" {
		if raw.Message == "" {
			raw.Message = "lookup failed"
		}
		return nil, fmt.Errorf("ip-api: %s", raw.Message)
	}
	result := &Result{IP: ip, Country: raw.Country, CountryCode: raw.CountryCode, Region: raw.RegionName, City: raw.City, ASN: raw.AS, ISP: raw.ISP, Org: raw.Org, Proxy: raw.Proxy, Hosting: raw.Hosting, Provider: p.Name(), CheckedAt: time.Now(), LatencyMS: time.Since(start).Milliseconds()}
	Score(result, "", false, result.LatencyMS)
	return result, nil
}
