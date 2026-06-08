package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleAuthRejectsNonPostMethodsWithoutPassword(t *testing.T) {
	server := &Server{cfg: Config{Password: ""}}
	req := httptest.NewRequest(http.MethodGet, "/api/auth", nil)
	rec := httptest.NewRecorder()

	server.handleAuth(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleTrafficRejectsNonGetMethods(t *testing.T) {
	server := &Server{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/traffic", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	server.handleTraffic(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
