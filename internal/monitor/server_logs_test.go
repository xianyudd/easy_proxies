package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleLogsRejectsNonGetMethods(t *testing.T) {
	server := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/logs", nil)
	rec := httptest.NewRecorder()

	server.handleLogs(rec, req)

	assertLogsErrorCode(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
}

func TestHandleLogsRedactsSensitiveContent(t *testing.T) {
	old := SharedLogBuffer
	SharedLogBuffer = NewLogBuffer(4096)
	t.Cleanup(func() { SharedLogBuffer = old })
	_, _ = SharedLogBuffer.Write([]byte(`proxy=http://user:pass@127.0.0.1:30116
curl -x user:pass@127.0.0.1:30116
{"password":"secret","token":"abc"}
password: plain-secret
safe=http://127.0.0.1:30116
`))

	server := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := httptest.NewRecorder()

	server.handleLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Logs string `json:"logs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"user:pass@", `"password":"secret"`, `"token":"abc"`, "password: plain-secret"} {
		if strings.Contains(body.Logs, leaked) {
			t.Fatalf("sensitive log content leaked %q in %s", leaked, body.Logs)
		}
	}
	for _, want := range []string{"http://***:***@127.0.0.1:30116", "***:***@127.0.0.1:30116", `"password":"***"`, `"token":"***"`, "password: ***", "safe=http://127.0.0.1:30116"} {
		if !strings.Contains(body.Logs, want) {
			t.Fatalf("redacted logs missing %q: %s", want, body.Logs)
		}
	}
}

func TestHandleLogsHonorsLinesLimit(t *testing.T) {
	old := SharedLogBuffer
	SharedLogBuffer = NewLogBuffer(4096)
	t.Cleanup(func() { SharedLogBuffer = old })
	_, _ = SharedLogBuffer.Write([]byte("line-1\nline-2\nline-3\nline-4\n"))

	server := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/logs?lines=2", nil)
	rec := httptest.NewRecorder()

	server.handleLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Logs string `json:"logs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Logs != "line-3\nline-4\n" {
		t.Fatalf("logs=%q, want last 2 lines", body.Logs)
	}
}

func TestHandleLogsRejectsInvalidLines(t *testing.T) {
	for _, path := range []string{"/api/logs?lines=bad", "/api/logs?lines=-1", "/api/logs?lines=0"} {
		t.Run(path, func(t *testing.T) {
			server := &Server{}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			server.handleLogs(rec, req)

			assertLogsErrorCode(t, rec, http.StatusBadRequest, "invalid_lines")
		})
	}
}

func assertLogsErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
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
