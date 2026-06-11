package geoip

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureDatabaseDownloadTimeoutIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte("too late"))
	}))
	defer server.Close()

	oldURL := geoipDownloadURL
	oldTimeout := geoipDownloadTimeout
	geoipDownloadURL = server.URL
	geoipDownloadTimeout = 50 * time.Millisecond
	defer func() {
		geoipDownloadURL = oldURL
		geoipDownloadTimeout = oldTimeout
	}()

	dbPath := filepath.Join(t.TempDir(), "GeoLite2-Country.mmdb")
	started := time.Now()
	err := EnsureDatabase(dbPath)
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("EnsureDatabase succeeded against a slow server, want timeout error")
	}
	if !strings.Contains(err.Error(), "download failed") {
		t.Fatalf("EnsureDatabase error=%v, want download failure", err)
	}
	if elapsed > 250*time.Millisecond {
		t.Fatalf("EnsureDatabase took %s, want bounded by test timeout", elapsed)
	}
	if _, statErr := os.Stat(dbPath); !os.IsNotExist(statErr) {
		t.Fatalf("timed out download should not leave final db file, stat err=%v", statErr)
	}
}

func TestEnsureDatabaseSkipsDownloadForExistingNonEmptyFile(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	oldURL := geoipDownloadURL
	oldTimeout := geoipDownloadTimeout
	geoipDownloadURL = server.URL
	geoipDownloadTimeout = 50 * time.Millisecond
	defer func() {
		geoipDownloadURL = oldURL
		geoipDownloadTimeout = oldTimeout
	}()

	dbPath := filepath.Join(t.TempDir(), "GeoLite2-Country.mmdb")
	if err := os.WriteFile(dbPath, []byte("cached"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDatabase(dbPath); err != nil {
		t.Fatalf("EnsureDatabase existing file error=%v", err)
	}
	if called {
		t.Fatal("EnsureDatabase should not download when a non-empty database file already exists")
	}
}
