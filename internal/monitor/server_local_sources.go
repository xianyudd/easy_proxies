package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"easy_proxies/internal/config"
)

type localSourceManager interface {
	LocalSources(context.Context) (config.LocalSourcesState, error)
	SaveLocalSource(context.Context, string, int, string, bool, string) (config.LocalSourcesState, error)
	DeleteLocalSource(context.Context, string, int) (config.LocalSourcesState, error)
	ApplyLocalSources(context.Context, int) error
}
type localSourcePreviewer interface {
	PreviewLocalSource(context.Context, string, *int, string) (config.LocalSourcePreview, error)
}

type localSourceRequest struct {
	SourceID         string  `json:"source_id"`
	ExpectedRevision *int    `json:"expected_revision"`
	Name             string  `json:"name"`
	Enabled          bool    `json:"enabled"`
	Content          *string `json:"content"`
}

func (r localSourceRequest) contentText() string {
	if r.Content == nil {
		return ""
	}
	return *r.Content
}

func (s *Server) registerLocalSources(mux *http.ServeMux) {
	protection := http.NewCrossOriginProtection()
	for path, handler := range map[string]http.HandlerFunc{
		"/api/local-sources":  s.handleLocalSources,
		"/api/local-sources/": s.handleLocalSources,
	} {
		mux.Handle(path, protection.Handler(s.withRole("admin", handler)))
	}
}
func decodeLocalSourceRequest(w http.ResponseWriter, r *http.Request) (localSourceRequest, bool) {
	var req localSourceRequest
	// JSON escaping may expand a 1MiB text snapshot; raw content has its own cap.
	r.Body = http.MaxBytesReader(w, r.Body, config.MaxLocalSourceBodyBytes*6+4096)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(&req)
	if err == nil {
		var extra any
		if dec.Decode(&extra) != io.EOF {
			err = errors.New("trailing JSON")
		}
	}
	if len(req.contentText()) > config.MaxLocalSourceBodyBytes {
		http.Error(w, "文本超过 1 MiB", 413)
		return req, false
	}
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			http.Error(w, "请求过大", 413)
		} else {
			http.Error(w, "请求格式不正确", 400)
		}
		return req, false
	}
	return req, true
}
func localSourceError(w http.ResponseWriter, err error) {
	var parse *config.LocalSourceParseError
	if errors.As(err, &parse) {
		w.WriteHeader(400)
		writeJSON(w, map[string]any{"error": parse.Error(), "line": parse.Line, "reason": parse.Reason})
		return
	}
	status := 400
	message := "本地源操作失败，请检查输入或服务端存储"
	switch {
	case errors.Is(err, config.ErrLocalSourceConflict):
		status = 409
		message = "版本已变化，请重新读取后再保存"
	case errors.Is(err, config.ErrLocalSourceBusy):
		status = 409
		message = "正在应用配置，请稍后重试"
	case errors.Is(err, config.ErrLocalSourceNotFound):
		status = 404
		message = "本地源不存在"
	}
	w.WriteHeader(status)
	writeJSON(w, map[string]any{"error": message})
}
func localSourcesSummary(state config.LocalSourcesState) map[string]any {
	rows := make([]map[string]any, 0, len(state.Sources))
	for _, source := range state.Sources {
		rows = append(rows, map[string]any{"id": source.ID, "name": source.Name, "enabled": source.Enabled, "member_count": len(source.Nodes)})
	}
	return map[string]any{"revision": state.Revision, "active_revision": state.ActiveRevision, "pending": state.Pending, "applying": state.Applying, "sources": rows}
}
func (s *Server) handleLocalSources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	suffix := strings.TrimPrefix(r.URL.Path, "/api/local-sources")
	suffix = strings.TrimPrefix(suffix, "/")
	if suffix == "preview" {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		req, ok := decodeLocalSourceRequest(w, r)
		if !ok {
			return
		}
		if previewer, ok := s.nodeMgr.(localSourcePreviewer); ok {
			result, err := previewer.PreviewLocalSource(r.Context(), req.SourceID, req.ExpectedRevision, req.contentText())
			if err != nil {
				localSourceError(w, err)
				return
			}
			writeJSON(w, result)
			return
		}
		// A server without source management can still offer a context-free format
		// check. Never invent owner counts for an unsupported contextual request.
		if s.nodeMgr != nil || req.SourceID != "" || req.ExpectedRevision != nil {
			http.Error(w, "本地源预览上下文不可用", 503)
			return
		}
		result, err := config.ParseLocalSourceNodes(req.contentText())
		if err != nil {
			localSourceError(w, err)
			return
		}
		protocols := map[string]int{}
		for _, n := range result.Nodes {
			scheme, _, _ := strings.Cut(n.URI, "://")
			protocols[scheme]++
		}
		writeJSON(w, config.LocalSourcePreview{MemberCount: len(result.Nodes), Duplicates: result.Duplicates, Protocols: protocols})
		return
	}
	mgr, ok := s.nodeMgr.(localSourceManager)
	if !ok {
		http.Error(w, "本地源管理未启用", 503)
		return
	}
	if suffix == "apply" {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		req, ok := decodeLocalSourceRequest(w, r)
		if !ok {
			return
		}
		if req.ExpectedRevision == nil {
			http.Error(w, "缺少版本号", 400)
			return
		}
		state, err := mgr.LocalSources(r.Context())
		if err != nil {
			localSourceError(w, err)
			return
		}
		if !state.Pending || state.Revision != *req.ExpectedRevision {
			localSourceError(w, config.ErrLocalSourceConflict)
			return
		}
		status, err := s.startLocalSourceApply(mgr, *req.ExpectedRevision)
		if err != nil {
			localSourceError(w, err)
			return
		}
		w.WriteHeader(202)
		writeJSON(w, map[string]any{"reload_status": status})
		return
	}
	if r.Method == http.MethodGet {
		state, err := mgr.LocalSources(r.Context())
		if err != nil {
			localSourceError(w, err)
			return
		}
		status := s.currentReloadStatus()
		state.Applying = status.State == "running" && status.LocalRevision > 0
		if suffix == "" {
			writeJSON(w, localSourcesSummary(state))
			return
		}
		for _, source := range state.Sources {
			if source.ID == suffix {
				writeJSON(w, map[string]any{"revision": state.Revision, "source": source, "member_health": localSourceMemberHealth(source, state.Pending, s.mgr.Snapshot())})
				return
			}
		}
		localSourceError(w, config.ErrLocalSourceNotFound)
		return
	}
	if (suffix == "" && r.Method != http.MethodPost) || (suffix != "" && r.Method != http.MethodPut && r.Method != http.MethodDelete) {
		w.WriteHeader(405)
		return
	}
	req, ok := decodeLocalSourceRequest(w, r)
	if !ok {
		return
	}
	if req.ExpectedRevision == nil {
		http.Error(w, "缺少版本号", 400)
		return
	}
	// A queued application already owns its captured revision; reject edits even
	// during the short interval before the worker acquires the manager gate.
	status := s.currentReloadStatus()
	if status.State == "running" && status.LocalRevision > 0 {
		localSourceError(w, config.ErrLocalSourceBusy)
		return
	}
	var state config.LocalSourcesState
	var err error
	if r.Method == http.MethodDelete {
		state, err = mgr.DeleteLocalSource(r.Context(), suffix, *req.ExpectedRevision)
	} else {
		if req.Content != nil && strings.TrimSpace(*req.Content) == "" {
			http.Error(w, "替换内容不能为空", 400)
			return
		}
		state, err = mgr.SaveLocalSource(r.Context(), suffix, *req.ExpectedRevision, req.Name, req.Enabled, req.contentText())
	}
	if err != nil {
		localSourceError(w, err)
		return
	}
	writeJSON(w, localSourcesSummary(state))
}

type localMemberHealth struct {
	Status string `json:"status"`
	Tag    string `json:"tag,omitempty"`
}

// Match the exact connection text used by composition (ignore display fragment,
// preserve credential case). Do not return probe errors or other owners' URIs.
func localSourceMemberHealth(source config.LocalSource, pending bool, snapshots []Snapshot) []localMemberHealth {
	key := func(uri string) string { return strings.SplitN(strings.TrimSpace(uri), "#", 2)[0] }
	runtime := map[string]Snapshot{}
	for _, snap := range snapshots {
		runtime[key(snap.URI)] = snap
	}
	rows := make([]localMemberHealth, len(source.Nodes))
	for i, n := range source.Nodes {
		status := "not_running"
		if pending {
			status = "unapplied"
		}
		rows[i].Status = status
		if snap, ok := runtime[key(n.URI)]; ok {
			status = "untested"
			if snap.Blacklisted || (snap.InitialCheckDone && !snap.Available) {
				status = "unavailable"
			} else if snap.InitialCheckDone && snap.Available {
				status = "available"
			}
			rows[i] = localMemberHealth{Status: status, Tag: snap.Tag}
		}
	}
	return rows
}

func (s *Server) startLocalSourceApply(mgr localSourceManager, revision int) (reloadStatus, error) {
	s.reloadMu.Lock()
	if s.reloadState == "running" {
		s.reloadMu.Unlock()
		return reloadStatus{}, config.ErrLocalSourceBusy
	}
	now := time.Now()
	s.reloadState = "running"
	s.reloadPending = false
	s.reloadPendingBy = ""
	s.reloadStatus = reloadStatus{State: "running", StartedAt: now, RequestedBy: "local_sources", LocalRevision: revision}
	status := s.reloadStatus
	s.reloadMu.Unlock()
	go func() {
		err := mgr.ApplyLocalSources(context.Background(), revision)
		s.reloadMu.Lock()
		s.reloadStatus.FinishedAt = time.Now()
		s.reloadStatus.DurationMS = time.Since(now).Milliseconds()
		s.reloadState = "succeeded"
		s.reloadStatus.Error = ""
		if err != nil {
			s.reloadState = "failed"
			s.reloadStatus.Error = err.Error()
		}
		s.reloadStatus.State = s.reloadState
		pending := s.reloadPending
		s.reloadPending = false
		s.reloadPendingBy = ""
		s.reloadMu.Unlock()
		if pending && err == nil {
			_, _, _ = s.startAsyncReload("queued_after_local_sources")
		}
	}()
	return status, nil
}
