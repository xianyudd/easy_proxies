package monitor

import (
	"net/http/httptest"
	"testing"
)

func TestServeEmbeddedFileSetsCacheHeadersForWebAssets(t *testing.T) {
	tests := []struct {
		name             string
		file             string
		wantContentType  string
		wantCacheControl string
	}{
		{
			name:             "html entry is not cached",
			file:             "index.html",
			wantContentType:  "text/html; charset=utf-8",
			wantCacheControl: "no-cache, no-store, must-revalidate",
		},
		{
			name:             "hashed javascript asset is immutable",
			file:             "assets/index-example.js",
			wantContentType:  "text/javascript; charset=utf-8",
			wantCacheControl: "public, max-age=31536000, immutable",
		},
		{
			name:             "hashed stylesheet asset is immutable",
			file:             "assets/index-example.css",
			wantContentType:  "text/css; charset=utf-8",
			wantCacheControl: "public, max-age=31536000, immutable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			serveEmbeddedFile(rec, tc.file, []byte("ok"))
			if got := rec.Header().Get("Content-Type"); got != tc.wantContentType {
				t.Fatalf("Content-Type = %q, want %q", got, tc.wantContentType)
			}
			if got := rec.Header().Get("Cache-Control"); got != tc.wantCacheControl {
				t.Fatalf("Cache-Control = %q, want %q", got, tc.wantCacheControl)
			}
		})
	}
}
