package monitor

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"easy_proxies/internal/config"
)

func readFirstSSEData(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/traffic", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("traffic request failed: %v", err)
	}
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		_ = resp.Body.Close()
		t.Fatalf("content type = %q", resp.Header.Get("Content-Type"))
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	_ = resp.Body.Close()
	cancel()
	srv.CloseClientConnections()
	if err != nil {
		t.Fatalf("read first SSE line: %v", err)
	}
	return line
}

func TestHandleTrafficUnavailableFlushesInitialSSE(t *testing.T) {
	server := &Server{cfgSrc: &config.Config{Management: config.ManagementConfig{ClashAPIListen: "127.0.0.1:1"}}}
	line := readFirstSSEData(t, server.handleTraffic)
	if !strings.Contains(line, `"status":"unavailable"`) {
		t.Fatalf("unexpected first SSE line: %q", line)
	}
}

func TestTrafficAPIURLUsesConfiguredClashListen(t *testing.T) {
	server := &Server{cfgSrc: &config.Config{Management: config.ManagementConfig{ClashAPIListen: "127.0.0.1:19094"}}}
	if got := server.trafficAPIURL(); got != "http://127.0.0.1:19094/traffic" {
		t.Fatalf("traffic api url = %q", got)
	}

	server = &Server{cfgSrc: &config.Config{Management: config.ManagementConfig{ClashAPIListen: "http://127.0.0.1:19094"}}}
	if got := server.trafficAPIURL(); got != "http://127.0.0.1:19094/traffic" {
		t.Fatalf("traffic api url with scheme = %q", got)
	}
}

func TestHandleTrafficForwardsConfiguredClashStream(t *testing.T) {
	clash := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/traffic" {
			t.Fatalf("unexpected clash path: %s", r.URL.Path)
		}
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `{"up":123,"down":456}`)
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer clash.Close()

	listen := strings.TrimPrefix(clash.URL, "http://")
	server := &Server{cfgSrc: &config.Config{Management: config.ManagementConfig{ClashAPIListen: listen}}}
	line := readFirstSSEData(t, server.handleTraffic)
	if !strings.Contains(line, `"up":123`) {
		t.Fatalf("unexpected first SSE line: %q", line)
	}
}
