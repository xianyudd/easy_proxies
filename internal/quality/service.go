package quality

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var ErrActiveJob = errors.New("quality job already running")

// TargetSource lists runtime targets for a quality job.
type TargetSource interface {
	ListTargets(ctx context.Context, q TargetQuery) ([]Target, error)
}

// CloudflareRunner runs one Cloudflare compatibility check.
type CloudflareRunner interface {
	CheckCloudflare(ctx context.Context, target Target) Result
}

// ReputationRunner runs one reputation check.
type ReputationRunner interface {
	CheckReputation(ctx context.Context, target Target, expectedCountry string) Result
}

// QuickRunner runs a cheap reachability check before expensive quality checks.
type QuickRunner interface {
	CheckQuick(ctx context.Context, target Target) Result
}

// ServiceOptions configures a quality Service.
type ServiceOptions struct {
	Store            *Store
	TargetSource     TargetSource
	QuickRunner      QuickRunner
	CloudflareRunner CloudflareRunner
	ReputationRunner ReputationRunner
	MaxWorkers       int
	MaxActiveJobs    int
}

type activeJob struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Service owns quality background job lifecycle and execution.
type Service struct {
	store            *Store
	targetSource     TargetSource
	quickRunner      QuickRunner
	cloudflareRunner CloudflareRunner
	reputationRunner ReputationRunner
	maxWorkers       int
	maxActiveJobs    int

	rootCtx    context.Context
	rootCancel context.CancelFunc

	mu     sync.Mutex
	active map[string]*activeJob
	slots  int
}

// NewService creates a quality job service.
func NewService(opts ServiceOptions) *Service {
	store := opts.Store
	if store == nil {
		store = NewStore()
	}
	maxWorkers := opts.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = 20
	}
	maxActive := opts.MaxActiveJobs
	if maxActive <= 0 {
		maxActive = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		store:            store,
		targetSource:     opts.TargetSource,
		quickRunner:      opts.QuickRunner,
		cloudflareRunner: opts.CloudflareRunner,
		reputationRunner: opts.ReputationRunner,
		maxWorkers:       maxWorkers,
		maxActiveJobs:    maxActive,
		rootCtx:          ctx,
		rootCancel:       cancel,
		active:           make(map[string]*activeJob),
	}
}

// CreateJob creates and starts a cancellable background quality job.
func (s *Service) CreateJob(ctx context.Context, req JobRequest) (JobSnapshot, error) {
	if s == nil {
		return JobSnapshot{}, errors.New("nil quality service")
	}
	if req.Kind == "" {
		req.Kind = CheckCombined
	}
	if !validKind(req.Kind) {
		return JobSnapshot{}, fmt.Errorf("invalid quality job kind %q", req.Kind)
	}
	req.Query = targetQueryFromRequest(req)

	if err := s.reserveSlot(req.Replace); err != nil {
		return JobSnapshot{}, err
	}

	targets := append([]Target(nil), req.Targets...)
	if len(targets) == 0 {
		if s.targetSource == nil {
			s.releaseReservedSlot()
			return JobSnapshot{}, errors.New("quality target source is not configured")
		}
		listed, err := s.targetSource.ListTargets(ctx, req.Query)
		if err != nil {
			s.releaseReservedSlot()
			return JobSnapshot{}, err
		}
		targets = listed
	}
	if req.Count > 0 && len(targets) > req.Count {
		targets = targets[:req.Count]
	}
	targets = normalizeTargets(targets)
	req.Targets = targets

	snapshot, err := s.store.CreateJob(req)
	if err != nil {
		s.releaseReservedSlot()
		return JobSnapshot{}, err
	}

	jobCtx, cancel := context.WithCancel(s.rootCtx)
	done := make(chan struct{})
	s.mu.Lock()
	s.active[snapshot.ID] = &activeJob{cancel: cancel, done: done}
	s.mu.Unlock()

	go s.runJob(jobCtx, snapshot.ID, req, targets, done)
	return snapshot, nil
}

// GetJob returns a job snapshot.
func (s *Service) GetJob(id string) (JobSnapshot, bool) {
	if s == nil || s.store == nil {
		return JobSnapshot{}, false
	}
	return s.store.GetJob(id)
}

// ListResults returns paginated job results.
func (s *Service) ListResults(id string, q ResultQuery) PagedResults {
	if s == nil || s.store == nil {
		return PagedResults{}
	}
	return s.store.ListResults(id, q)
}

// CancelJob cancels a running or queued job. It is idempotent.
func (s *Service) CancelJob(id string) error {
	if s == nil {
		return errors.New("nil quality service")
	}
	s.mu.Lock()
	active := s.active[id]
	s.mu.Unlock()
	if active != nil {
		active.cancel()
	}
	if err := s.store.CancelJob(id, "cancelled"); err != nil && !errors.Is(err, ErrJobTerminal) {
		return err
	}
	return nil
}

// Shutdown cancels active jobs and waits for them or ctx timeout.
func (s *Service) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.rootCancel()
	s.mu.Lock()
	active := make([]*activeJob, 0, len(s.active))
	ids := make([]string, 0, len(s.active))
	for id, job := range s.active {
		ids = append(ids, id)
		active = append(active, job)
		job.cancel()
	}
	s.mu.Unlock()
	for _, id := range ids {
		_ = s.store.CancelJob(id, "shutdown")
	}
	for _, job := range active {
		select {
		case <-job.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *Service) runJob(ctx context.Context, id string, req JobRequest, targets []Target, done chan struct{}) {
	defer close(done)
	defer func() {
		s.mu.Lock()
		delete(s.active, id)
		if s.slots > 0 {
			s.slots--
		}
		s.mu.Unlock()
	}()

	if err := s.store.StartJob(id); err != nil {
		return
	}
	if err := runTargets(ctx, workerConfig{workers: s.maxWorkers, kind: req.Kind, expectedCountry: expectedCountryFromRegion(req.Region), quick: s.quickRunner, cf: s.cloudflareRunner, rep: s.reputationRunner}, targets, func(result Result) bool {
		result = applyTierDecision(result)
		if err := s.store.AddResult(id, result); err != nil {
			return false
		}
		return true
	}, func(completed, failed int) bool {
		return s.store.UpdateProgress(id, completed, failed, "") == nil
	}); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			_ = s.store.CancelJob(id, "cancelled")
			return
		}
		_ = s.store.FailJob(id, err.Error())
		return
	}
	_ = s.store.CompleteJob(id, "completed")
}

func applyTierDecision(result Result) Result {
	decision := ClassifyResult(result)
	result.Tier = string(decision.Tier)
	result.TierScore = decision.Score
	result.Pool = decision.Pool
	result.Capabilities = append([]string(nil), decision.Capabilities...)
	result.TierReasons = append([]string(nil), decision.Reasons...)
	if result.Recommend == false && (decision.Tier == TierRecommended || decision.Tier == TierPremium) {
		result.Recommend = true
	}
	return result
}

func validKind(kind CheckKind) bool {
	switch kind {
	case CheckQuick, CheckCloudflare, CheckReputation, CheckCombined, CheckPipeline:
		return true
	default:
		return false
	}
}

func normalizeTargets(targets []Target) []Target {
	out := make([]Target, len(targets))
	copy(out, targets)
	for i := range out {
		out[i].Index = i
		if out[i].ID == "" {
			out[i].ID = fmt.Sprintf("target-%d", i)
		}
	}
	return out
}

func (s *Service) reserveSlot(replace bool) error {
	for {
		s.mu.Lock()
		if s.slots < s.maxActiveJobs {
			s.slots++
			s.mu.Unlock()
			return nil
		}
		if !replace || len(s.active) == 0 {
			s.mu.Unlock()
			return ErrActiveJob
		}
		active := make([]*activeJob, 0, len(s.active))
		ids := make([]string, 0, len(s.active))
		for id, job := range s.active {
			ids = append(ids, id)
			active = append(active, job)
		}
		s.mu.Unlock()

		for i, job := range active {
			job.cancel()
			_ = s.store.CancelJob(ids[i], "replaced by new job")
		}
		for _, job := range active {
			<-job.done
		}
	}
}

func (s *Service) releaseReservedSlot() {
	s.mu.Lock()
	if s.slots > 0 {
		s.slots--
	}
	s.mu.Unlock()
}

func targetQueryFromRequest(req JobRequest) TargetQuery {
	q := req.Query
	if q.Kind == "" {
		q.Kind = req.Kind
	}
	if q.Region == "" {
		q.Region = req.Region
	}
	if req.Mode != "" {
		q.Mode = req.Mode
	}
	if req.IncludeUnavailable {
		q.IncludeUnavailable = true
	}
	if req.RetryFailed {
		q.RetryFailed = true
	}
	if req.ForceRefresh {
		q.ForceRefresh = true
	}
	return q
}
