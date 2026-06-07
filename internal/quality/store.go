package quality

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

const defaultResultPageSize = 100
const maxResultPageSize = 500
const maxStoredTerminalJobs = 20
const terminalJobTTL = 24 * time.Hour

var ErrJobNotFound = errors.New("quality job not found")
var ErrJobTerminal = errors.New("quality job is already terminal")

type storedJob struct {
	snapshot JobSnapshot
	targets  []Target
}

// Store is a concurrency-safe in-memory job and result store.
type Store struct {
	mu      sync.RWMutex
	now     func() time.Time
	nextID  uint64
	jobs    map[string]storedJob
	results map[string][]Result
}

// NewStore creates an empty in-memory Store.
func NewStore() *Store {
	return &Store{
		now:     time.Now,
		jobs:    make(map[string]storedJob),
		results: make(map[string][]Result),
	}
}

// CreateJob creates a queued job snapshot and stores its requested targets.
func (s *Store) CreateJob(req JobRequest) (JobSnapshot, error) {
	if s == nil {
		return JobSnapshot{}, errors.New("nil quality store")
	}
	if req.Kind == "" {
		req.Kind = CheckCombined
	}
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	id := fmt.Sprintf("job-%d-%d", now.UnixNano(), s.nextID)
	snapshot := JobSnapshot{
		ID:        id,
		Kind:      req.Kind,
		Status:    JobQueued,
		Region:    req.Region,
		Query:     copyTargetQuery(req.Query),
		Total:     len(req.Targets),
		Queued:    len(req.Targets),
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.jobs[id] = storedJob{snapshot: snapshot, targets: copyTargets(req.Targets)}
	s.results[id] = nil
	s.pruneTerminalJobsLocked(now)
	return snapshot, nil
}

// GetJob returns a copy of a job snapshot.
func (s *Store) GetJob(id string) (JobSnapshot, bool) {
	if s == nil {
		return JobSnapshot{}, false
	}
	s.mu.RLock()
	job, ok := s.jobs[id]
	if !ok {
		s.mu.RUnlock()
		return JobSnapshot{}, false
	}
	snapshot := copyJobSnapshot(job.snapshot)
	s.mu.RUnlock()
	return snapshot, true
}

// StartJob moves a queued job to running and records its start time.
func (s *Store) StartJob(id string) error {
	return s.setStatus(id, JobRunning, "")
}

// CompleteJob marks a job completed and records its finish time.
func (s *Store) CompleteJob(id, message string) error {
	return s.setStatus(id, JobCompleted, message)
}

// FailJob marks a job failed and records its finish time.
func (s *Store) FailJob(id, message string) error {
	return s.setStatus(id, JobFailed, message)
}

// CancelJob marks a job cancelled and records its finish time.
func (s *Store) CancelJob(id, message string) error {
	if s == nil {
		return errors.New("nil quality store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	if isTerminalStatus(job.snapshot.Status) {
		if job.snapshot.Status == JobCancelled {
			return nil
		}
		return ErrJobTerminal
	}
	now := s.now()
	job.snapshot.Status = JobCancelled
	job.snapshot.Message = message
	job.snapshot.UpdatedAt = now
	if job.snapshot.FinishedAt.IsZero() {
		job.snapshot.FinishedAt = now
	}
	if job.snapshot.Completed > job.snapshot.Total {
		job.snapshot.Completed = job.snapshot.Total
	}
	pending := job.snapshot.Total - job.snapshot.Completed
	if pending < 0 {
		pending = 0
	}
	job.snapshot.Cancelled = pending
	job.snapshot.Queued = 0
	if job.snapshot.Total > 0 {
		job.snapshot.Percent = 100
	}
	s.jobs[id] = job
	s.pruneTerminalJobsLocked(now)
	return nil
}

// UpdateProgress updates a job's counters and optional message.
func (s *Store) UpdateProgress(id string, completed, failed int, message string) error {
	if s == nil {
		return errors.New("nil quality store")
	}
	if completed < 0 {
		completed = 0
	}
	if failed < 0 {
		failed = 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	if isTerminalStatus(job.snapshot.Status) {
		return ErrJobTerminal
	}
	job.snapshot.Completed = completed
	job.snapshot.Failed = failed
	if job.snapshot.Completed > job.snapshot.Total {
		job.snapshot.Completed = job.snapshot.Total
	}
	job.snapshot.Queued = job.snapshot.Total - job.snapshot.Completed
	if job.snapshot.Queued < 0 {
		job.snapshot.Queued = 0
	}
	if job.snapshot.Total > 0 {
		job.snapshot.Percent = float64(job.snapshot.Completed) * 100 / float64(job.snapshot.Total)
	}
	job.snapshot.Message = message
	job.snapshot.UpdatedAt = s.now()
	s.jobs[id] = job
	return nil
}

// AddResult appends one result to the job's result list.
func (s *Store) AddResult(jobID string, result Result) error {
	if s == nil {
		return errors.New("nil quality store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return ErrJobNotFound
	}
	if job.snapshot.Status != JobRunning {
		return ErrJobTerminal
	}
	if result.JobID == "" {
		result.JobID = jobID
	}
	if result.CheckedAt.IsZero() {
		result.CheckedAt = s.now()
	}
	result = normalizeResult(result)
	s.results[jobID] = append(s.results[jobID], result)
	updateSummary(&job.snapshot, result)
	job.snapshot.UpdatedAt = s.now()
	s.jobs[jobID] = job
	return nil
}

// ListResults returns a deterministic page of results for a job.
func (s *Store) ListResults(jobID string, q ResultQuery) PagedResults {
	page, pageSize := normalizePage(q.Page, q.PageSize)
	if s == nil {
		return emptyPage(page, pageSize)
	}

	s.mu.RLock()
	job, ok := s.jobs[jobID]
	if !ok {
		s.mu.RUnlock()
		return emptyPage(page, pageSize)
	}
	items := buildResultRows(job.snapshot, job.targets, s.results[jobID])
	s.mu.RUnlock()

	sort.SliceStable(items, func(i, j int) bool {
		return resultLess(items[i], items[j])
	})

	count := len(items)
	totalPages := 0
	if count > 0 {
		totalPages = (count + pageSize - 1) / pageSize
	}
	start := (page - 1) * pageSize
	if start >= count {
		return PagedResults{Data: []Result{}, Count: count, Page: page, PageSize: pageSize, TotalPages: totalPages, HasNext: false}
	}
	end := start + pageSize
	if end > count {
		end = count
	}
	return PagedResults{
		Data:       append([]Result(nil), items[start:end]...),
		Count:      count,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		HasNext:    page < totalPages,
	}
}

func (s *Store) setStatus(id string, status JobStatus, message string) error {
	if s == nil {
		return errors.New("nil quality store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return ErrJobNotFound
	}
	if isTerminalStatus(job.snapshot.Status) {
		if job.snapshot.Status == status {
			return nil
		}
		return ErrJobTerminal
	}
	now := s.now()
	job.snapshot.Status = status
	job.snapshot.Message = message
	job.snapshot.UpdatedAt = now
	if status == JobRunning && job.snapshot.StartedAt.IsZero() {
		job.snapshot.StartedAt = now
	}
	if isTerminalStatus(status) && job.snapshot.FinishedAt.IsZero() {
		job.snapshot.FinishedAt = now
	}
	s.jobs[id] = job
	if isTerminalStatus(status) {
		s.pruneTerminalJobsLocked(now)
	}
	return nil
}

func (s *Store) pruneTerminalJobsLocked(now time.Time) {
	type terminal struct {
		id        string
		createdAt time.Time
	}
	terminals := make([]terminal, 0)
	for id, job := range s.jobs {
		if !isTerminalStatus(job.snapshot.Status) {
			continue
		}
		if !job.snapshot.FinishedAt.IsZero() && now.Sub(job.snapshot.FinishedAt) > terminalJobTTL {
			delete(s.jobs, id)
			delete(s.results, id)
			continue
		}
		terminals = append(terminals, terminal{id: id, createdAt: job.snapshot.CreatedAt})
	}
	if len(terminals) <= maxStoredTerminalJobs {
		return
	}
	sort.Slice(terminals, func(i, j int) bool {
		return terminals[i].createdAt.Before(terminals[j].createdAt)
	})
	for _, item := range terminals[:len(terminals)-maxStoredTerminalJobs] {
		delete(s.jobs, item.id)
		delete(s.results, item.id)
	}
}

func isTerminalStatus(status JobStatus) bool {
	switch status {
	case JobCompleted, JobFailed, JobCancelled:
		return true
	default:
		return false
	}
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultResultPageSize
	}
	if pageSize > maxResultPageSize {
		pageSize = maxResultPageSize
	}
	return page, pageSize
}

func emptyPage(page, pageSize int) PagedResults {
	return PagedResults{Data: []Result{}, Page: page, PageSize: pageSize}
}

func resultLess(a, b Result) bool {
	if a.TargetIndex != b.TargetIndex {
		return a.TargetIndex < b.TargetIndex
	}
	if a.TargetID != b.TargetID {
		return a.TargetID < b.TargetID
	}
	if a.NodeTag != b.NodeTag {
		return a.NodeTag < b.NodeTag
	}
	if a.ProxyURL != b.ProxyURL {
		return a.ProxyURL < b.ProxyURL
	}
	return a.Port < b.Port
}

func normalizeResult(result Result) Result {
	if result.TargetIndex == 0 {
		result.TargetIndex = result.Target.Index
	}
	if result.TargetID == "" {
		result.TargetID = result.Target.ID
	}
	if result.NodeName == "" {
		result.NodeName = result.Target.NodeName
	}
	if result.NodeTag == "" {
		result.NodeTag = result.Target.NodeTag
	}
	if result.Source == "" {
		result.Source = result.Target.Source
	}
	if result.ProxyURL == "" {
		result.ProxyURL = result.Target.ProxyURL
	}
	if result.Protocol == "" {
		result.Protocol = result.Target.Protocol
	}
	if result.Host == "" {
		result.Host = result.Target.Host
	}
	if result.Port == 0 {
		result.Port = result.Target.Port
	}
	if result.Region == "" {
		result.Region = result.Target.Region
	}
	if result.Tier == "" {
		result.Tier = string(TierReject)
	}
	if result.Pool == "" {
		result.Pool = "reject_pool"
	}
	if result.Status == "" {
		if result.Error != "" || !result.Success {
			result.Status = "failed"
		} else {
			result.Status = "completed"
		}
	}
	return result
}

func updateSummary(snapshot *JobSnapshot, result Result) {
	if snapshot == nil {
		return
	}
	if snapshot.Summary.Cloudflare == nil {
		snapshot.Summary.Cloudflare = map[string]int{"excellent": 0, "good": 0, "fair": 0, "poor": 0, "failed": 0}
	}
	if snapshot.Summary.Reputation == nil {
		snapshot.Summary.Reputation = map[string]int{"low": 0, "medium": 0, "high": 0, "failed": 0}
	}
	switch result.Kind {
	case CheckQuick:
		ensureQuickSummary(snapshot)
		updateQuickSummary(snapshot.Summary.Quick, result)
	case CheckCloudflare:
		snapshot.Summary.Cloudflare[cfSummaryLevel(result)]++
	case CheckReputation:
		snapshot.Summary.Reputation[repSummaryLevel(result)]++
	case CheckCombined:
		snapshot.Summary.Cloudflare[cfSummaryLevelFromMap(result.CF)]++
		snapshot.Summary.Reputation[repSummaryLevelFromMap(result.Reputation)]++
	case CheckPipeline:
		ensureQuickSummary(snapshot)
		ensureFinalSummary(snapshot)
		updateQuickSummary(snapshot.Summary.Quick, Result{Success: quickMapOK(result.Quick), Status: quickStatus(result.Quick), Error: quickError(result.Quick), Quick: result.Quick})
		if result.Recommend {
			snapshot.Summary.Final["recommend"]++
		} else {
			snapshot.Summary.Final["rejected"]++
		}
		if result.CF != nil {
			snapshot.Summary.Cloudflare[cfSummaryLevelFromMap(result.CF)]++
		}
		if result.Reputation != nil {
			snapshot.Summary.Reputation[repSummaryLevelFromMap(result.Reputation)]++
		}
	}
	if snapshot.Summary.Tier == nil {
		snapshot.Summary.Tier = map[string]int{string(TierReject): 0, string(TierRescue): 0, string(TierHTTPOnly): 0, string(TierSimpleWeb): 0, string(TierRecommended): 0, string(TierPremium): 0}
	}
	if snapshot.Summary.Pool == nil {
		snapshot.Summary.Pool = map[string]int{"reject_pool": 0, "rescue_pool": 0, "http_pool": 0, "web_pool": 0, "recommended_pool": 0, "strict_pool": 0, "ai_pool": 0}
	}
	snapshot.Summary.Tier[result.Tier]++
	snapshot.Summary.Pool[result.Pool]++
}

func ensureQuickSummary(snapshot *JobSnapshot) {
	if snapshot.Summary.Quick == nil {
		snapshot.Summary.Quick = map[string]int{"ok": 0, "failed": 0}
	}
}

func ensureFinalSummary(snapshot *JobSnapshot) {
	if snapshot.Summary.Final == nil {
		snapshot.Summary.Final = map[string]int{"recommend": 0, "rejected": 0}
	}
}

func updateQuickSummary(summary map[string]int, result Result) {
	if result.Error != "" || result.Status == "failed" || !result.Success {
		summary["failed"]++
		if reason, _ := result.Quick["failure_reason"].(string); reason != "" {
			summary[reason]++
		}
		return
	}
	summary["ok"]++
}

func quickMapOK(detail map[string]any) bool {
	if detail == nil {
		return false
	}
	if ok, exists := detail["success"].(bool); exists {
		return ok
	}
	status, _ := detail["status"].(string)
	return status == "ok" || status == "completed"
}

func quickStatus(detail map[string]any) string {
	status, _ := detail["status"].(string)
	if status == "ok" {
		return "completed"
	}
	return status
}

func quickError(detail map[string]any) string {
	err, _ := detail["error"].(string)
	return err
}

func cfSummaryLevel(result Result) string {
	if result.Error != "" || result.Status == "failed" || !result.Success {
		return "failed"
	}
	if level, _ := result.CF["level"].(string); level != "" {
		switch level {
		case "excellent", "good", "fair", "poor", "failed":
			return level
		}
	}
	return "good"
}

func repSummaryLevel(result Result) string {
	if result.Error != "" || result.Status == "failed" || !result.Success {
		return "failed"
	}
	if level, _ := result.Reputation["risk_level"].(string); level != "" {
		switch level {
		case "low", "medium", "high", "failed":
			return level
		}
	}
	return "low"
}

func cfSummaryLevelFromMap(detail map[string]any) string {
	if detailFailed(detail) {
		return "failed"
	}
	if level, _ := detail["level"].(string); level != "" {
		switch level {
		case "excellent", "good", "fair", "poor", "failed":
			return level
		}
	}
	return "good"
}

func repSummaryLevelFromMap(detail map[string]any) string {
	if detailFailed(detail) {
		return "failed"
	}
	if level, _ := detail["risk_level"].(string); level != "" {
		switch level {
		case "low", "medium", "high", "failed":
			return level
		}
	}
	return "low"
}

func detailFailed(detail map[string]any) bool {
	if detail == nil {
		return true
	}
	if err, _ := detail["error"].(string); err != "" {
		return true
	}
	if status, _ := detail["status"].(string); status == "failed" {
		return true
	}
	if success, ok := detail["success"].(bool); ok && !success {
		return true
	}
	return false
}

func buildResultRows(snapshot JobSnapshot, targets []Target, results []Result) []Result {
	if len(targets) == 0 || isTerminalStatus(snapshot.Status) {
		out := append([]Result(nil), results...)
		for i := range out {
			out[i] = cloneResult(out[i])
		}
		return out
	}

	byIndex := make(map[int]Result, len(results))
	for _, result := range results {
		result = normalizeResult(result)
		byIndex[result.TargetIndex] = result
	}

	out := make([]Result, 0, len(targets))
	for idx, target := range targets {
		if target.Index == 0 && idx != 0 {
			target.Index = idx
		}
		if result, ok := byIndex[target.Index]; ok {
			result = cloneResult(result)
			result.Target = target
			result = normalizeResult(result)
			out = append(out, result)
			continue
		}
		out = append(out, normalizeResult(Result{
			JobID:       snapshot.ID,
			Kind:        snapshot.Kind,
			Target:      target,
			TargetIndex: target.Index,
			TargetID:    target.ID,
			NodeName:    target.NodeName,
			NodeTag:     target.NodeTag,
			Source:      target.Source,
			ProxyURL:    target.ProxyURL,
			Protocol:    target.Protocol,
			Host:        target.Host,
			Port:        target.Port,
			Region:      target.Region,
			Status:      "pending",
			Tier:        string(TierReject),
			Pool:        "reject_pool",
		}))
	}
	return out
}

func cloneResult(result Result) Result {
	result.Quick = cloneAnyMap(result.Quick)
	result.CF = cloneAnyMap(result.CF)
	result.Reputation = cloneAnyMap(result.Reputation)
	return result
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyJobSnapshot(snapshot JobSnapshot) JobSnapshot {
	snapshot.Query = copyTargetQuery(snapshot.Query)
	snapshot.Summary.Cloudflare = cloneIntMap(snapshot.Summary.Cloudflare)
	snapshot.Summary.Reputation = cloneIntMap(snapshot.Summary.Reputation)
	snapshot.Summary.Quick = cloneIntMap(snapshot.Summary.Quick)
	snapshot.Summary.Final = cloneIntMap(snapshot.Summary.Final)
	return snapshot
}

func copyTargetQuery(query TargetQuery) TargetQuery {
	query.IDs = append([]string(nil), query.IDs...)
	return query
}

func copyTargets(targets []Target) []Target {
	return append([]Target(nil), targets...)
}

func cloneIntMap(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
