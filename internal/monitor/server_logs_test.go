package monitor

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleLogsRejectsNonGetMethods(t *testing.T) {
	server := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/logs", nil)
	rec := httptest.NewRecorder()

	server.handleLogs(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
