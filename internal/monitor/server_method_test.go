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

func TestDefaultAPIContentTypeKeepsStructuredErrorsJSON(t *testing.T) {
	handler := defaultAPIContentType(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]any{"error": "method not allowed", "code": "method_not_allowed"})
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/example", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type=%q want application/json body=%s", got, rec.Body.String())
	}
}

func TestDefaultAPIContentTypeAllowsSSEOverride(t *testing.T) {
	handler := defaultAPIContentType(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {}\n\n"))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/nodes/probe-all", nil))

	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content-type=%q want text/event-stream", got)
	}
}

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

func TestHandleAuthStatusReportsSessionStateWithout401(t *testing.T) {
	server := &Server{cfg: Config{Password: "secret"}, sessions: make(map[string]*Session), sessionTTL: time.Hour}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	rec := httptest.NewRecorder()
	server.handleAuthStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unauthenticated status=%d body=%s", rec.Code, rec.Body.String())
	}
	var anonymous struct {
		Authenticated   bool `json:"authenticated"`
		PasswordRequired bool `json:"password_required"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &anonymous); err != nil {
		t.Fatal(err)
	}
	if anonymous.Authenticated || !anonymous.PasswordRequired {
		t.Fatalf("unexpected anonymous auth status: %#v body=%s", anonymous, rec.Body.String())
	}

	session, err := server.createSession()
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: session.Token})
	rec = httptest.NewRecorder()
	server.handleAuthStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated status=%d body=%s", rec.Code, rec.Body.String())
	}
	var authed struct {
		Authenticated bool `json:"authenticated"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &authed); err != nil {
		t.Fatal(err)
	}
	if !authed.Authenticated {
		t.Fatalf("expected authenticated status, got %#v body=%s", authed, rec.Body.String())
	}
}

func TestHandleAuthStatusAllowsPasswordlessMode(t *testing.T) {
	server := &Server{cfg: Config{Password: ""}}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	rec := httptest.NewRecorder()

	server.handleAuthStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Authenticated   bool `json:"authenticated"`
		PasswordRequired bool `json:"password_required"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Authenticated || body.PasswordRequired {
		t.Fatalf("unexpected passwordless auth status: %#v body=%s", body, rec.Body.String())
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
