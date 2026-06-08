package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleLogsRejectsNonGetMethods(t *testing.T) {
	server := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/logs", nil)
	rec := httptest.NewRecorder()

	server.handleLogs(rec, req)

	assertLogsErrorCode(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
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
