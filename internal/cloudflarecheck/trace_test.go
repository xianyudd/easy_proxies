package cloudflarecheck

import "testing"

func TestParseTrace(t *testing.T) {
	trace := ParseTrace("ip=1.2.3.4\nloc=jp\ncolo=nrt\nhttp=http/3\ntls=TLSv1.3\nwarp=off\n")
	if trace.IP != "1.2.3.4" || trace.LOC != "JP" || trace.COLO != "NRT" || trace.HTTP != "http/3" || trace.TLS != "TLSv1.3" || trace.WARP != "off" {
		t.Fatalf("unexpected trace: %#v", trace)
	}
}

func TestParseTraceMissingFields(t *testing.T) {
	trace := ParseTrace("loc=us\ninvalid\n")
	if trace.LOC != "US" || trace.IP != "" || trace.COLO != "" {
		t.Fatalf("unexpected trace: %#v", trace)
	}
}
