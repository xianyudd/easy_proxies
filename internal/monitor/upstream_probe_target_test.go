package monitor

import "testing"

func TestUpstreamProbeTarget(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		want       string
	}{
		{"empty falls back", "", defaultUpstreamProbeTarget},
		{"blank falls back", "   ", defaultUpstreamProbeTarget},
		{"http kept as-is", "http://cp.cloudflare.com/generate_204", "http://cp.cloudflare.com/generate_204"},
		// The probe only proves the proxy forwards; a TLS handshake would add
		// failure modes unrelated to that, so https is downgraded.
		{"https downgraded", "https://cp.cloudflare.com/generate_204", "http://cp.cloudflare.com/generate_204"},
		{"other scheme falls back", "socks5://127.0.0.1:7890", defaultUpstreamProbeTarget},
		{"schemeless falls back", "cp.cloudflare.com/generate_204", defaultUpstreamProbeTarget},
		{"garbage falls back", "://%%", defaultUpstreamProbeTarget},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := upstreamProbeTarget(tc.configured); got != tc.want {
				t.Fatalf("upstreamProbeTarget(%q) = %q, want %q", tc.configured, got, tc.want)
			}
		})
	}
}
