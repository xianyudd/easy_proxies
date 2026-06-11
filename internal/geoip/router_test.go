package geoip

import (
	"context"
	"encoding/base64"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRouterStartReturnsBindError(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	router := NewRouter(RouterConfig{Listen: "127.0.0.1", Port: port}, log.New(io.Discard, "", 0))
	if err := router.Start(context.Background()); err == nil {
		t.Fatal("expected bind error, got nil")
	}
}

type testRouteSummaryDialer struct{}

func (testRouteSummaryDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, nil
}

func TestRouterRouteSummaryShowsOnlyRegisteredRoutes(t *testing.T) {
	t.Parallel()

	router := NewRouter(RouterConfig{Listen: "127.0.0.1", Port: 0}, log.New(io.Discard, "", 0))
	if got := router.routeSummary(); got != "none" {
		t.Fatalf("empty route summary=%q", got)
	}

	router.SetGlobalPool(testRouteSummaryDialer{})
	if got := router.routeSummary(); got != "default only" {
		t.Fatalf("global-only route summary=%q", got)
	}

	router.SetPool("jp", testRouteSummaryDialer{})
	router.SetPool("us", testRouteSummaryDialer{})
	if got := router.routeSummary(); got != "/jp, /us (default: all nodes)" {
		t.Fatalf("registered route summary=%q", got)
	}
}

func TestRouterProxyAuthCanSelectRegionByUsernameSuffix(t *testing.T) {
	t.Parallel()

	router := NewRouter(RouterConfig{Username: "user", Password: "pass"}, log.New(io.Discard, "", 0))

	cases := []struct {
		name       string
		credential string
		wantRegion string
		wantOK     bool
	}{
		{name: "global exact user", credential: "user:pass", wantOK: true},
		{name: "region suffix", credential: "user-us:pass", wantRegion: "us", wantOK: true},
		{name: "upper suffix rejected", credential: "user-US:pass", wantRegion: "us", wantOK: true},
		{name: "bad password", credential: "user-us:bad", wantOK: false},
		{name: "unknown region", credential: "user-zz:pass", wantOK: false},
		{name: "wrong user", credential: "other-us:pass", wantOK: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
			req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(tc.credential)))
			gotRegion, gotOK := router.checkProxyAuth(req)
			if gotOK != tc.wantOK || gotRegion != tc.wantRegion {
				t.Fatalf("checkProxyAuth(%q)=(%q,%v), want (%q,%v)", tc.credential, gotRegion, gotOK, tc.wantRegion, tc.wantOK)
			}
		})
	}
}

func TestRouterStandardProxyURLPathIsNotSeenByCurl(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	received := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 2048)
		n, _ := conn.Read(buf)
		received <- string(buf[:n])
		_, _ = conn.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm=\"x\"\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
	}()

	// This documents the compatibility issue that prompted username-suffix
	// routing: curl does not forward /us/ from the proxy URL to the proxy.
	proxyURL := "http://user:pass@" + ln.Addr().String() + "/us/"
	_, _ = (&http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(mustParseURL(proxyURL))}}).Get("http://example.com/a")
	got := <-received
	if strings.Contains(got, "/us/") {
		t.Fatalf("standard proxy request unexpectedly included proxy URL path: %q", got)
	}
	if !strings.Contains(got, "GET http://example.com/a HTTP/1.1") {
		t.Fatalf("unexpected proxy request: %q", got)
	}
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

func TestRouterParseRequestDoesNotTreatDestinationPathAsRegion(t *testing.T) {
	t.Parallel()

	router := NewRouter(RouterConfig{}, log.New(io.Discard, "", 0))
	req := httptest.NewRequest(http.MethodGet, "http://example.com/us/foo", nil)
	region, target := router.parseRequest(req)
	if region != "" || target != "example.com" || req.URL.Path != "/us/foo" {
		t.Fatalf("parseRequest region=%q target=%q path=%q, want global target path preserved", region, target, req.URL.Path)
	}
}

func TestRouterParseRequestKeepsLegacyOriginFormRegionPrefix(t *testing.T) {
	t.Parallel()

	router := NewRouter(RouterConfig{}, log.New(io.Discard, "", 0))
	req := httptest.NewRequest(http.MethodGet, "/us/foo", nil)
	req.Host = "example.com"
	region, target := router.parseRequest(req)
	if region != "us" || target != "example.com" || req.URL.Path != "/foo" {
		t.Fatalf("parseRequest region=%q target=%q path=%q, want legacy origin-form region rewrite", region, target, req.URL.Path)
	}
}
