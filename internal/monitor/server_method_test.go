package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestHandleAuthReturnsStructuredErrorCodes(t *testing.T) {
	server := &Server{cfg: Config{Password: "secret"}}
	cases := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{name: "bad json", body: `{`, status: http.StatusBadRequest, code: "invalid_request"},
		{name: "wrong password", body: `{"password":"wrong"}`, status: http.StatusUnauthorized, code: "invalid_password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/auth", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			server.handleAuth(rec, req)

			assertAuthErrorCode(t, rec, tc.status, tc.code)
		})
	}
}

func TestWithAuthReturnsStructuredUnauthorizedError(t *testing.T) {
	server := &Server{cfg: Config{Password: "secret"}}
	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	rec := httptest.NewRecorder()

	server.withAuth(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called without a valid session")
	})(rec, req)

	assertAuthErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
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

func assertAuthErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status=%d, want %d body=%s", rec.Code, status, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error == "" || body.Code != code {
		t.Fatalf("unexpected body: %#v raw=%s", body, rec.Body.String())
	}
}
