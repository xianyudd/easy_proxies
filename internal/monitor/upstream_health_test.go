package monitor

import "testing"

func TestParseUpstreamHostPort(t *testing.T) {
	h, p, err := parseUpstreamHostPort("socks5://127.0.0.1:17890")
	if err != nil || h != "127.0.0.1" || p != "17890" {
		t.Fatalf("got %s %s %v", h, p, err)
	}
	h, p, err = parseUpstreamHostPort("http://10.0.0.1")
	if err != nil || h != "10.0.0.1" || p != "80" {
		t.Fatalf("default port got %s %s %v", h, p, err)
	}
}
