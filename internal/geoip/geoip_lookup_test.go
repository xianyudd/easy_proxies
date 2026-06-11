package geoip

import "testing"

func TestCountryISOCodeFallsBackToRegisteredCountryForAnycast(t *testing.T) {
	if got := countryISOCode("", "US", ""); got != "US" {
		t.Fatalf("countryISOCode() = %q, want registered country US", got)
	}
}

func TestCountryISOCodePrefersExplicitCountry(t *testing.T) {
	if got := countryISOCode("JP", "US", ""); got != "JP" {
		t.Fatalf("countryISOCode() = %q, want explicit country JP", got)
	}
}

func TestISO3166RegionTableHasConcreteUniqueCodes(t *testing.T) {
	seen := make(map[string]bool, len(iso3166Countries))
	for _, item := range iso3166Countries {
		if item.Code == "" {
			t.Fatalf("empty ISO code in %#v", item)
		}
		if item.Code == RegionOther || item.Code == "all" {
			t.Fatalf("ISO table must contain only concrete countries, got %q", item.Code)
		}
		if seen[item.Code] {
			t.Fatalf("duplicate ISO code %q", item.Code)
		}
		seen[item.Code] = true
		if item.NameZH == "" || item.NameEN == "" || item.Emoji == "" {
			t.Fatalf("ISO code %q missing display metadata: %#v", item.Code, item)
		}
		if !IsRegionCode(item.Code) {
			t.Fatalf("IsRegionCode(%q) = false, want true", item.Code)
		}
	}
	if IsRegionCode(RegionOther) || IsRegionCode("all") || IsRegionCode("") {
		t.Fatalf("non-concrete regions must not be accepted as ISO region codes")
	}
	all := AllRegions()
	if len(all) != len(iso3166Countries)+1 || all[len(all)-1] != RegionOther {
		t.Fatalf("AllRegions should expose all countries plus trailing other, got len=%d last=%q", len(all), all[len(all)-1])
	}
}
