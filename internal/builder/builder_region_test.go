package builder

import (
	"testing"

	"easy_proxies/internal/geoip"
)

func TestClassifyRegionFromTextAvoidsTwoLetterFalsePositives(t *testing.T) {
	t.Parallel()

	text := "🇨🇭瑞士苏黎世 | 高速专线-hy2\nhysteria2://id@example.com:443?insecure=1&fingerprint=chrome&sni=www.apple.com#Zurich"
	got := classifyRegionFromText(text)
	if got != geoip.RegionCH {
		t.Fatalf("expected %q, got %q", geoip.RegionCH, got)
	}
}

func TestClassifyRegionFromTextMatchesTokenizedRegionCodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want string
	}{
		{name: "india token", text: "premium IN relay", want: geoip.RegionIN},
		{name: "uae token", text: "edge AE route", want: geoip.RegionAE},
		{name: "australia keyword", text: "Sydney premium", want: geoip.RegionAU},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyRegionFromText(tc.text); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
