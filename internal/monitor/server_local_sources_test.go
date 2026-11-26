package monitor

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"easy_proxies/internal/config"
)

func TestLocalSourcesAdminAndPreview(t *testing.T) {
	s := newAPIKeyTestServer(t, []config.APIKeyConfig{{Name: "read", Key: "epk_read_test", Role: "read"}, {Name: "admin", Key: "epk_admin_test", Role: "admin"}}, "")
	for _, tt := range []struct {
		key, body, origin string
		want              int
	}{
		{"", `{"content":"http://node.test"}`, "", 401},
		{"epk_read_test", `{"content":"http://node.test"}`, "", 403},
		{"epk_admin_test", `{"content":"http://secret@node.test"}`, "", 200},
		{"epk_admin_test", `{"content":"vmess://secret!!!"}`, "", 400},
		{"epk_admin_test", `{"content":"http://node.test"}`, "https://evil.test", 403},
		{"epk_admin_test", `{"content":"http://node.test","path":"/etc/passwd"}`, "", 400},
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/local-sources/preview", strings.NewReader(tt.body))
		r.Header.Set("X-API-Key", tt.key)
		r.Header.Set("Origin", tt.origin)
		w := httptest.NewRecorder()
		s.handler.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Fatalf("want %d got %d", tt.want, w.Code)
		}
		if strings.Contains(w.Body.String(), "secret") {
			t.Fatal("preview leaked credentials")
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/api/local-sources/preview", strings.NewReader(`{"content":"`+strings.Repeat("x", config.MaxLocalSourceBodyBytes+1)+`"}`))
	r.Header.Set("X-API-Key", "epk_admin_test")
	w := httptest.NewRecorder()
	s.handler.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatalf("body limit: %d", w.Code)
	}
}
