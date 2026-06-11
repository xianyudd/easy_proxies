package builder

import (
	"testing"

	"easy_proxies/internal/config"
	"easy_proxies/internal/geoip"
	poolout "easy_proxies/internal/outbound/pool"
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
		{name: "germany chinese keyword", text: "德国DE-HY2", want: geoip.RegionDE},
		{name: "uk token", text: "premium UK London", want: geoip.RegionGB},
		{name: "canada keyword", text: "加拿大-优化", want: geoip.RegionCA},
		{name: "france token", text: "法国FR-A", want: geoip.RegionFR},
		{name: "vietnam token", text: "越南VN-A", want: geoip.RegionVN},
		{name: "ukraine token", text: "乌克兰UA-A", want: geoip.RegionUA},
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

func TestClassifyRegionFromTextDoesNotInferCountriesFromURITokens(t *testing.T) {
	t.Parallel()

	cases := []string{
		"套餐到期：2026-06-18\nss://YWVzLTEyOC1nY206@127.0.0.1:6666",
		"套餐到期：长期有效\nvmess://id@planb.mojcn.com:16617?path=%2F&type=ws",
	}
	for _, text := range cases {
		text := text
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			if got := classifyRegionFromText(text); got != "" {
				t.Fatalf("expected no region, got %q", got)
			}
		})
	}
}

func TestClassifyRegionFromTextKeepsURIFragmentRemark(t *testing.T) {
	t.Parallel()

	text := "vless://id@unamecf.example:443?encryption=none&host=ujp1.example&type=ws#%F0%9F%87%AF%F0%9F%87%B5%E6%97%A5%E6%9C%AC%E4%B8%9C%E4%BA%AC01"
	if got := classifyRegionFromText(text); got != geoip.RegionJP {
		t.Fatalf("expected URI fragment remark to classify as %q, got %q", geoip.RegionJP, got)
	}
}

func TestResolveRegionForNodePrefersStrongNameSignalOverGeoIP(t *testing.T) {
	t.Parallel()

	region, country := resolveRegionForNode(
		geoip.RegionInfo{Code: geoip.RegionUS, Country: "United States", ISOCode: "US"},
		"🇯🇵日本东京01-0.1倍 | 电信联通推荐\n01-0-1",
		"",
		false,
	)
	if region != geoip.RegionJP || country != "日本 (name matched)" {
		t.Fatalf("expected strong name signal to win over anycast GeoIP, got region=%q country=%q", region, country)
	}
}

func TestResolveRegionForNodePrefersURIFragmentRemarkOverGeoIP(t *testing.T) {
	t.Parallel()

	region, country := resolveRegionForNode(
		geoip.RegionInfo{Code: geoip.RegionUS, Country: "United States", ISOCode: "US"},
		"01-0-1\nvless://id@unamecf.example:443?encryption=none&host=ujp1.example&type=ws#%F0%9F%87%AF%F0%9F%87%B5%E6%97%A5%E6%9C%AC%E4%B8%9C%E4%BA%AC01",
		"",
		false,
	)
	if region != geoip.RegionJP || country != "日本 (name matched)" {
		t.Fatalf("expected URI fragment remark to win over GeoIP, got region=%q country=%q", region, country)
	}
}

func TestResolveRegionForNodeManualOverrideWinsOverNameAndGeoIP(t *testing.T) {
	t.Parallel()

	region, country := resolveRegionForNode(
		geoip.RegionInfo{Code: geoip.RegionUS, Country: "United States", ISOCode: "US"},
		"🇯🇵日本东京01-0.1倍",
		geoip.RegionGB,
		true,
	)
	if region != geoip.RegionGB || country != "英国 (manual)" {
		t.Fatalf("expected manual override to win, got region=%q country=%q", region, country)
	}
}

func TestBuildManualRegionOverrideWinsOverNameFallback(t *testing.T) {
	t.Parallel()

	uri := "http://127.0.0.1:18080#Tokyo"
	cfg := &config.Config{
		Mode:     "pool",
		Listener: config.ListenerConfig{Address: "127.0.0.1", Port: 12080},
		Pool:     config.PoolConfig{Mode: "sequential"},
		Nodes:    []config.NodeConfig{{Name: "日本 Tokyo", URI: uri}},
	}
	cfg.SetRegionOverride(uri, geoip.RegionUS)

	opts, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var poolMeta poolout.MemberMeta
	found := false
	for _, outbound := range opts.Outbounds {
		if outbound.Tag != poolout.Tag {
			continue
		}
		poolOpts, ok := outbound.Options.(*poolout.Options)
		if !ok {
			t.Fatalf("pool options type = %T", outbound.Options)
		}
		if len(poolOpts.Members) != 1 {
			t.Fatalf("members=%#v, want one", poolOpts.Members)
		}
		poolMeta = poolOpts.Metadata[poolOpts.Members[0]]
		found = true
		break
	}
	if !found {
		t.Fatal("proxy-pool outbound not found")
	}
	if poolMeta.Region != geoip.RegionUS || poolMeta.Country != "美国 (manual)" {
		t.Fatalf("manual override did not win: %#v", poolMeta)
	}
}

func TestBuildAndroidProxyCreatesRegionPoolsWithoutGeoIPRouter(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Mode: "pool",
		Listener: config.ListenerConfig{
			Address: "127.0.0.1",
			Port:    12080,
		},
		Pool: config.PoolConfig{Mode: "sequential"},
		AndroidProxy: config.AndroidProxyConfig{
			Enabled:  true,
			Listen:   "127.0.0.1",
			BasePort: 13001,
		},
		GeoIP: config.GeoIPConfig{Enabled: false},
		Nodes: []config.NodeConfig{
			{Name: "美国 US relay", URI: "http://127.0.0.1:18080"},
			{Name: "法国 FR relay", URI: "http://127.0.0.1:18081"},
		},
	}

	opts, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, outbound := range opts.Outbounds {
		if outbound.Type != poolout.Type {
			continue
		}
		if outbound.Tag == "pool-us" || outbound.Tag == "pool-fr" {
			poolOpts, ok := outbound.Options.(*poolout.Options)
			if !ok {
				t.Fatalf("pool options type = %T", outbound.Options)
			}
			if len(poolOpts.Members) != 1 {
				t.Fatalf("%s members=%#v, want one", outbound.Tag, poolOpts.Members)
			}
			found[outbound.Tag] = true
		}
	}
	if !found["pool-us"] || !found["pool-fr"] {
		t.Fatalf("android region pools not built: %#v", found)
	}
}

func TestBuildFreeProxyUsesOptionalExistingGeoIPOnly(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Mode: "multi-port",
		MultiPort: config.MultiPortConfig{
			Address:  "127.0.0.1",
			BasePort: 39000,
			Username: "u",
			Password: "p",
		},
		GeoIP: config.GeoIPConfig{
			Enabled:      false,
			DatabasePath: t.TempDir() + "/missing.mmdb",
		},
		Nodes: []config.NodeConfig{{
			Name:   "free-ip",
			URI:    "http://43.161.239.147:8888",
			Port:   39000,
			Source: config.NodeSourceFreeProxy,
		}},
	}

	opts, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build should not fail or download when optional GeoIP DB is missing: %v", err)
	}
	if len(opts.Outbounds) == 0 {
		t.Fatal("Build returned no outbounds")
	}
}
