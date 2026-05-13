package reputation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func newHTTPClient(client *http.Client, timeout time.Duration) *http.Client {
	if client != nil {
		return client
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

func getJSON(ctx context.Context, client *http.Client, endpoint string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "easy-proxies-reputation/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	return dec.Decode(dst)
}

func joinURL(base, elem string) string {
	base = strings.TrimRight(base, "/")
	return base + "/" + strings.TrimLeft(elem, "/")
}

// LookupOutboundIP returns the apparent egress IP. If proxyURL is non-nil, the
// request is made through that proxy. Secrets embedded in proxyURL are never logged.
func LookupOutboundIP(ctx context.Context, endpoint string, proxyURL *url.URL, timeout time.Duration) (string, error) {
	if endpoint == "" {
		endpoint = "https://api.ipify.org?format=json"
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{Timeout: timeout, Transport: transport}
	return lookupOutboundIPWithClient(ctx, client, endpoint)
}

// LookupOutboundIPViaProxy is a convenience wrapper for a raw proxy URL string.
// The proxy URL may include credentials; this function never logs or returns it.
func LookupOutboundIPViaProxy(ctx context.Context, endpoint, proxyRawURL string, timeout time.Duration) (string, error) {
	var proxyURL *url.URL
	if proxyRawURL != "" {
		parsed, err := url.Parse(proxyRawURL)
		if err != nil {
			return "", errors.New("invalid proxy URL")
		}
		proxyURL = parsed
	}
	return LookupOutboundIP(ctx, endpoint, proxyURL, timeout)
}

func lookupOutboundIPWithClient(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "easy-proxies-reputation/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}
	var payload struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.IP != "" {
		return payload.IP, nil
	}
	ip := strings.TrimSpace(string(body))
	if parsed := net.ParseIP(ip); parsed == nil {
		return "", errors.New("empty or invalid outbound IP response")
	}
	return ip, nil
}
