package monitor

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"easy_proxies/internal/quality"
)

func (s *Server) handleQualityJobs(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/quality/jobs" {
		s.handleQualityJobItem(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]any{"error": "method not allowed", "code": "method_not_allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": "invalid request body", "code": "invalid_request"})
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err == nil {
		if rawCount, ok := raw["count"]; ok {
			var count int
			if err := json.Unmarshal(rawCount, &count); err != nil || count <= 0 {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]any{"error": "invalid count", "code": "invalid_request"})
				return
			}
		}
	}
	var req quality.JobRequest
	if err := decodeSingleJSONBytes(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": "invalid request body", "code": "invalid_request"})
		return
	}
	if code, ok := normalizeQualityJobRequest(&req); !ok {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": strings.ReplaceAll(code, "_", " "), "code": code})
		return
	}
	if req.Count <= 0 {
		req.Count = 500
	}
	if req.Count > 10000 {
		req.Count = 10000
	}
	snap, err := s.qualitySvc.CreateJob(r.Context(), req)
	if err != nil {
		if errors.Is(err, quality.ErrActiveJob) {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{"error": err.Error(), "code": "active_job"})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": err.Error(), "code": "invalid_request"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, qualityJobCreatedResponse(snap))
}

func (s *Server) handleQualityJobItem(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/quality/jobs/")
	path = strings.Trim(path, "/")
	if path == "" {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]any{"error": "missing job id", "code": "not_found"})
		return
	}
	parts := strings.Split(path, "/")
	id := parts[0]
	if len(parts) == 1 && r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]any{"error": "method not allowed", "code": "method_not_allowed"})
		return
	}
	if len(parts) == 1 {
		snap, ok := s.qualitySvc.GetJob(id)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]any{"error": "job not found", "code": "not_found"})
			return
		}
		writeJSON(w, snap)
		return
	}
	if len(parts) == 2 && parts[1] == "results" && r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]any{"error": "method not allowed", "code": "method_not_allowed"})
		return
	}
	if len(parts) == 2 && parts[1] == "results" {
		if _, ok := s.qualitySvc.GetJob(id); !ok {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]any{"error": "job not found", "code": "not_found"})
			return
		}
		page, ok := parsePositiveQueryIntStrict(w, r, "page", 1)
		if !ok {
			return
		}
		pageSize, ok := parsePositiveQueryIntStrict(w, r, "page_size", 100)
		if !ok {
			return
		}
		writeJSON(w, s.qualitySvc.ListResults(id, quality.ResultQuery{Page: page, PageSize: pageSize}))
		return
	}
	if len(parts) == 2 && parts[1] == "cancel" && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]any{"error": "method not allowed", "code": "method_not_allowed"})
		return
	}
	if len(parts) == 2 && parts[1] == "cancel" {
		if err := s.qualitySvc.CancelJob(id); err != nil && !errors.Is(err, quality.ErrJobTerminal) {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]any{"error": err.Error(), "code": "not_found"})
			return
		}
		snap, _ := s.qualitySvc.GetJob(id)
		writeJSON(w, snap)
		return
	}
	w.WriteHeader(http.StatusNotFound)
	writeJSON(w, map[string]any{"error": "not found", "code": "not_found"})
}

func normalizeQualityJobRequest(req *quality.JobRequest) (string, bool) {
	if req == nil {
		return "invalid_request", false
	}
	if code, ok := normalizeQualityJobRegion(&req.Region); !ok {
		return code, false
	}
	if code, ok := normalizeQualityJobRegion(&req.Query.Region); !ok {
		return code, false
	}
	if req.Region == "" && req.Query.Region != "" {
		req.Region = req.Query.Region
	}
	if req.Region == "" {
		req.Region = "all"
	}
	if code, ok := normalizeQualityJobMode(&req.Mode); !ok {
		return code, false
	}
	if code, ok := normalizeQualityJobMode(&req.Query.Mode); !ok {
		return code, false
	}
	if req.Mode == "" {
		req.Mode = "multi-port"
	}
	if code, ok := normalizeQualityJobSource(&req.Source); !ok {
		return code, false
	}
	if code, ok := normalizeQualityJobSource(&req.Query.Source); !ok {
		return code, false
	}
	return "", true
}

func normalizeQualityJobRegion(region *string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(*region))
	if value == "" {
		*region = ""
		return "", true
	}
	if !isAllowedMonitorRegion(value) {
		return "invalid_region", false
	}
	*region = value
	return "", true
}

func normalizeQualityJobMode(mode *string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(*mode))
	if value == "" {
		*mode = ""
		return "", true
	}
	switch value {
	case "multi", "multi_port":
		*mode = "multi-port"
	case "multi-port":
		*mode = value
	default:
		return "invalid_mode", false
	}
	return "", true
}

func normalizeQualityJobSource(source *string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(*source))
	if value == "" {
		*source = ""
		return "", true
	}
	if !isAllowedQueryValue(value, "all", "subscription", "free_proxy", "inline", "nodes_file", "unknown") {
		return "invalid_source", false
	}
	*source = value
	return "", true
}

func isBackgroundQualityRequest(r *http.Request) (bool, bool, string) {
	background, ok := parseOptionalBoolParam(r.URL.Query(), "background")
	if !ok {
		return false, false, "background"
	}
	async, ok := parseOptionalBoolParam(r.URL.Query(), "async")
	if !ok {
		return false, false, "async"
	}
	return background || async, true, ""
}

func (s *Server) startBackgroundQualityCheck(w http.ResponseWriter, r *http.Request, kind quality.CheckKind, region, mode, source string, count int, includeUnavailable, retryFailed bool) bool {
	background, ok, invalidKey := isBackgroundQualityRequest(r)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": "invalid " + invalidKey, "code": "invalid_bool"})
		return true
	}
	if !background {
		return false
	}
	if count <= 0 {
		count = 500
	}
	if count > 10000 {
		count = 10000
	}
	req := quality.JobRequest{Kind: kind, Region: region, Mode: mode, Source: source, Count: count, IncludeUnavailable: includeUnavailable, RetryFailed: retryFailed}
	if code, ok := normalizeQualityJobRequest(&req); !ok {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": strings.ReplaceAll(code, "_", " "), "code": code})
		return true
	}
	snap, err := s.qualitySvc.CreateJob(r.Context(), req)
	if err != nil {
		if errors.Is(err, quality.ErrActiveJob) {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{"error": err.Error(), "code": "active_job"})
			return true
		}
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": err.Error(), "code": "invalid_request"})
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, qualityJobCreatedResponse(snap))
	return true
}

func parsePositiveQueryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func parsePositiveQueryIntStrict(w http.ResponseWriter, r *http.Request, key string, fallback int) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": "invalid " + key, "code": "invalid_pagination"})
		return 0, false
	}
	return parsed, true
}

func qualityJobCreatedResponse(snap quality.JobSnapshot) map[string]any {
	return map[string]any{
		"job_id":       snap.ID,
		"status":       snap.Status,
		"kind":         snap.Kind,
		"progress_url": "/api/quality/jobs/" + snap.ID,
		"results_url":  "/api/quality/jobs/" + snap.ID + "/results",
	}
}
