package quality

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTargetSource struct{ targets []Target }

func (f fakeTargetSource) ListTargets(ctx context.Context, q TargetQuery) ([]Target, error) {
	out := make([]Target, len(f.targets))
	copy(out, f.targets)
	return out, nil
}

type trackingRunner struct {
	delay   time.Duration
	current int32
	max     int32
	calls   int32
}

func (r *trackingRunner) CheckCloudflare(ctx context.Context, target Target) Result {
	return r.check(ctx, CheckCloudflare, target)
}

func (r *trackingRunner) CheckReputation(ctx context.Context, target Target, expectedCountry string) Result {
	return r.check(ctx, CheckReputation, target)
}

func (r *trackingRunner) check(ctx context.Context, kind CheckKind, target Target) Result {
	cur := atomic.AddInt32(&r.current, 1)
	defer atomic.AddInt32(&r.current, -1)
	atomic.AddInt32(&r.calls, 1)
	for {
		max := atomic.LoadInt32(&r.max)
		if cur <= max || atomic.CompareAndSwapInt32(&r.max, max, cur) {
			break
		}
	}
	if r.delay > 0 {
		select {
		case <-ctx.Done():
			return Result{Kind: kind, Target: target, TargetIndex: target.Index, TargetID: target.ID, Error: ctx.Err().Error()}
		case <-time.After(r.delay):
		}
	}
	return Result{Kind: kind, Target: target, TargetIndex: target.Index, TargetID: target.ID, Status: "completed", Success: true, Score: 90}
}

func makeTargets(n int) []Target {
	targets := make([]Target, n)
	for i := range targets {
		targets[i] = Target{Index: i, ID: fmt.Sprintf("target-%d", i), NodeTag: fmt.Sprintf("node-%d", i), Region: "sg"}
	}
	return targets
}

func TestServiceCreateJobReturnsQuicklyForLargeTargetSet(t *testing.T) {
	runner := &trackingRunner{delay: 20 * time.Millisecond}
	svc := NewService(ServiceOptions{TargetSource: fakeTargetSource{targets: makeTargets(6000)}, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 2})
	defer svc.Shutdown(context.Background())

	start := time.Now()
	snap, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare, Count: 6000})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("CreateJob blocked too long: %s", elapsed)
	}
	if snap.ID == "" || snap.Status != JobQueued || snap.Total != 6000 {
		t.Fatalf("unexpected snapshot: %#v", snap)
	}
	if err := svc.CancelJob(snap.ID); err != nil {
		t.Fatalf("CancelJob returned error: %v", err)
	}
}

func TestServiceWorkerConcurrencyDoesNotExceedLimit(t *testing.T) {
	runner := &trackingRunner{delay: 5 * time.Millisecond}
	svc := NewService(ServiceOptions{TargetSource: fakeTargetSource{targets: makeTargets(40)}, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 3})
	defer svc.Shutdown(context.Background())

	snap, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare, Count: 40})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	got := waitForJobStatus(t, svc, snap.ID, JobCompleted, 2*time.Second)
	if got.Completed != 40 || got.Failed != 0 {
		t.Fatalf("unexpected completed snapshot: %#v", got)
	}
	if max := atomic.LoadInt32(&runner.max); max > 3 {
		t.Fatalf("max concurrency = %d, want <= 3", max)
	}
}

func TestServiceDefaultWorkerLimitIsCapped(t *testing.T) {
	runner := &trackingRunner{delay: time.Millisecond}
	svc := NewService(ServiceOptions{TargetSource: fakeTargetSource{targets: makeTargets(120)}, CloudflareRunner: runner, ReputationRunner: runner})
	defer svc.Shutdown(context.Background())

	snap, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare, Count: 120})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	got := waitForJobStatus(t, svc, snap.ID, JobCompleted, 2*time.Second)
	if got.Completed != 120 || got.Failed != 0 {
		t.Fatalf("unexpected completed snapshot: %#v", got)
	}
	if max := atomic.LoadInt32(&runner.max); max > defaultMaxWorkers {
		t.Fatalf("default max concurrency = %d, want <= %d", max, defaultMaxWorkers)
	}
}

func TestServiceRejectsSecondActiveJobUnlessReplace(t *testing.T) {
	runner := &trackingRunner{delay: 50 * time.Millisecond}
	svc := NewService(ServiceOptions{TargetSource: fakeTargetSource{targets: makeTargets(20)}, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 1})
	defer svc.Shutdown(context.Background())

	first, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare, Count: 20})
	if err != nil {
		t.Fatalf("CreateJob first returned error: %v", err)
	}
	if _, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare, Count: 20}); !errors.Is(err, ErrActiveJob) {
		t.Fatalf("second CreateJob error = %v, want ErrActiveJob", err)
	}
	replacement, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare, Count: 20, Replace: true})
	if err != nil {
		t.Fatalf("replacement CreateJob returned error: %v", err)
	}
	waitForJobStatus(t, svc, first.ID, JobCancelled, 2*time.Second)
	if replacement.ID == first.ID {
		t.Fatal("replacement should create a new job")
	}
}

func TestServiceCancelStopsQueuedWorkAndKeepsCancelledStatus(t *testing.T) {
	runner := &trackingRunner{delay: 30 * time.Millisecond}
	svc := NewService(ServiceOptions{TargetSource: fakeTargetSource{targets: makeTargets(100)}, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 2})
	defer svc.Shutdown(context.Background())

	snap, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare, Count: 100})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if err := svc.CancelJob(snap.ID); err != nil {
		t.Fatalf("CancelJob returned error: %v", err)
	}
	got := waitForJobStatus(t, svc, snap.ID, JobCancelled, 2*time.Second)
	time.Sleep(80 * time.Millisecond)
	after, ok := svc.GetJob(snap.ID)
	if !ok {
		t.Fatal("expected job snapshot")
	}
	if after.Status != JobCancelled {
		t.Fatalf("status changed after cancellation: before=%#v after=%#v", got, after)
	}
	if after.Cancelled == 0 || after.Queued != 0 {
		t.Fatalf("cancelled job counters should describe skipped work, got %#v", after)
	}
}

func TestServiceShutdownCancelsActiveJobs(t *testing.T) {
	runner := &trackingRunner{delay: 50 * time.Millisecond}
	svc := NewService(ServiceOptions{TargetSource: fakeTargetSource{targets: makeTargets(100)}, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 2})
	snap, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare, Count: 100})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
	got, ok := svc.GetJob(snap.ID)
	if !ok {
		t.Fatal("expected job snapshot")
	}
	if got.Status != JobCancelled {
		t.Fatalf("status = %q, want cancelled", got.Status)
	}
}

func TestServiceCombinedJobWritesStablePagedResults(t *testing.T) {
	runner := &trackingRunner{}
	svc := NewService(ServiceOptions{TargetSource: fakeTargetSource{targets: makeTargets(5)}, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 2})
	defer svc.Shutdown(context.Background())

	snap, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCombined, Count: 5})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	waitForJobStatus(t, svc, snap.ID, JobCompleted, 2*time.Second)
	page := svc.ListResults(snap.ID, ResultQuery{Page: 1, PageSize: 3})
	if page.Count != 5 || page.PageSize != 3 || !page.HasNext || len(page.Data) != 3 {
		t.Fatalf("unexpected page: %#v", page)
	}
	for i, row := range page.Data {
		if row.TargetIndex != i || row.Status != "completed" || row.CF == nil || row.Reputation == nil {
			t.Fatalf("unexpected combined row %d: %#v", i, row)
		}
	}
}

func waitForJobStatus(t *testing.T, svc *Service, id string, want JobStatus, timeout time.Duration) JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last JobSnapshot
	for time.Now().Before(deadline) {
		snap, ok := svc.GetJob(id)
		if !ok {
			t.Fatalf("job %s not found", id)
		}
		last = snap
		if snap.Status == want {
			return snap
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s, last snapshot: %#v", want, last)
	return JobSnapshot{}
}

type blockingTargetSource struct {
	targets []Target
	ready   chan struct{}
	release chan struct{}
}

func (b *blockingTargetSource) ListTargets(ctx context.Context, q TargetQuery) ([]Target, error) {
	select {
	case <-b.ready:
	default:
		close(b.ready)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.release:
	}
	out := make([]Target, len(b.targets))
	copy(out, b.targets)
	return out, nil
}

func TestServiceConcurrentCreateJobReservesActiveSlotBeforeListingTargets(t *testing.T) {
	runner := &trackingRunner{delay: 10 * time.Millisecond}
	source := &blockingTargetSource{targets: makeTargets(5), ready: make(chan struct{}), release: make(chan struct{})}
	svc := NewService(ServiceOptions{TargetSource: source, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 1})
	defer svc.Shutdown(context.Background())

	firstResult := make(chan error, 1)
	go func() {
		_, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare})
		firstResult <- err
	}()
	select {
	case <-source.ready:
	case <-time.After(time.Second):
		t.Fatal("first CreateJob did not reach target listing")
	}

	if _, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare}); !errors.Is(err, ErrActiveJob) {
		t.Fatalf("concurrent CreateJob error = %v, want ErrActiveJob", err)
	}
	close(source.release)
	if err := <-firstResult; err != nil {
		t.Fatalf("first CreateJob returned error: %v", err)
	}
}

func TestServiceJobRequestFieldsAreForwardedToTargetQuery(t *testing.T) {
	source := &recordingTargetSource{targets: makeTargets(1)}
	runner := &trackingRunner{}
	svc := NewService(ServiceOptions{TargetSource: source, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 1})
	defer svc.Shutdown(context.Background())

	_, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare, Region: "sg", Mode: "multi-port", IncludeUnavailable: true, RetryFailed: true, ForceRefresh: true})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if source.query.Region != "sg" || source.query.Mode != "multi-port" || !source.query.IncludeUnavailable || !source.query.RetryFailed || !source.query.ForceRefresh {
		t.Fatalf("request fields were not forwarded to target query: %#v", source.query)
	}
}

type recordingTargetSource struct {
	targets []Target
	query   TargetQuery
}

func (r *recordingTargetSource) ListTargets(ctx context.Context, q TargetQuery) ([]Target, error) {
	r.query = q
	out := make([]Target, len(r.targets))
	copy(out, r.targets)
	return out, nil
}

func TestServiceReplaceDoesNotClearInFlightReservation(t *testing.T) {
	runner := &trackingRunner{delay: 10 * time.Millisecond}
	source := &blockingTargetSource{targets: makeTargets(5), ready: make(chan struct{}), release: make(chan struct{})}
	svc := NewService(ServiceOptions{TargetSource: source, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 1})
	defer svc.Shutdown(context.Background())

	firstResult := make(chan error, 1)
	go func() {
		_, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare})
		firstResult <- err
	}()
	select {
	case <-source.ready:
	case <-time.After(time.Second):
		t.Fatal("first CreateJob did not reserve and enter target listing")
	}

	if _, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare, Replace: true}); !errors.Is(err, ErrActiveJob) {
		t.Fatalf("replace during in-flight reservation error = %v, want ErrActiveJob", err)
	}
	close(source.release)
	if err := <-firstResult; err != nil {
		t.Fatalf("first CreateJob returned error: %v", err)
	}
	if _, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare}); !errors.Is(err, ErrActiveJob) {
		t.Fatalf("third CreateJob error = %v, want ErrActiveJob while first job active", err)
	}
}

func TestServiceTargetQueryNestedValuesAreNotClearedByZeroTopLevelFields(t *testing.T) {
	source := &recordingTargetSource{targets: makeTargets(1)}
	runner := &trackingRunner{}
	svc := NewService(ServiceOptions{TargetSource: source, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 1})
	defer svc.Shutdown(context.Background())

	_, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare, Query: TargetQuery{Region: "hk", Mode: "multi-port", IncludeUnavailable: true, RetryFailed: true, ForceRefresh: true}})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if source.query.Region != "hk" || source.query.Mode != "multi-port" || !source.query.IncludeUnavailable || !source.query.RetryFailed || !source.query.ForceRefresh {
		t.Fatalf("nested query values were cleared: %#v", source.query)
	}
}

func TestServiceCombinedJobPreservesDetailMapsAndSummary(t *testing.T) {
	runner := &detailRunner{}
	svc := NewService(ServiceOptions{TargetSource: fakeTargetSource{targets: makeTargets(2)}, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 1})
	defer svc.Shutdown(context.Background())

	snap, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCombined, Region: "all", Count: 2})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	got := waitForJobStatus(t, svc, snap.ID, JobCompleted, 2*time.Second)
	if got.Summary.Cloudflare["excellent"] != 2 || got.Summary.Reputation["low"] != 2 {
		t.Fatalf("unexpected summary: %#v", got.Summary)
	}
	page := svc.ListResults(snap.ID, ResultQuery{Page: 1, PageSize: 1})
	if len(page.Data) != 1 || page.Data[0].CF["level"] != "excellent" || page.Data[0].Reputation["risk_level"] != "low" {
		t.Fatalf("combined detail maps not preserved: %#v", page.Data)
	}
	if runner.expectedCountry != "" {
		t.Fatalf("region=all expected country = %q, want empty", runner.expectedCountry)
	}
}

func TestServiceCombinedJobSummarySeparatesPartialFailures(t *testing.T) {
	runner := &partialFailureRunner{}
	svc := NewService(ServiceOptions{TargetSource: fakeTargetSource{targets: makeTargets(1)}, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 1})
	defer svc.Shutdown(context.Background())

	snap, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCombined, Region: "all", Count: 1})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	got := waitForJobStatus(t, svc, snap.ID, JobCompleted, 2*time.Second)
	if got.Summary.Cloudflare["excellent"] != 1 || got.Summary.Cloudflare["failed"] != 0 {
		t.Fatalf("cloudflare summary should keep successful CF detail: %#v", got.Summary.Cloudflare)
	}
	if got.Summary.Reputation["failed"] != 1 || got.Summary.Reputation["low"] != 0 {
		t.Fatalf("reputation summary should count only reputation failure: %#v", got.Summary.Reputation)
	}
}

type partialFailureRunner struct{}

func (p *partialFailureRunner) CheckCloudflare(ctx context.Context, target Target) Result {
	return Result{Kind: CheckCloudflare, Target: target, TargetIndex: target.Index, TargetID: target.ID, Status: "completed", Success: true, Score: 95, CF: map[string]any{"level": "excellent"}}
}

func (p *partialFailureRunner) CheckReputation(ctx context.Context, target Target, expectedCountry string) Result {
	return Result{Kind: CheckReputation, Target: target, TargetIndex: target.Index, TargetID: target.ID, Status: "failed", Success: false, Error: "lookup failed", Reputation: map[string]any{"risk_level": "failed", "error": "lookup failed"}}
}

type detailRunner struct{ expectedCountry string }

func (d *detailRunner) CheckCloudflare(ctx context.Context, target Target) Result {
	return Result{Kind: CheckCloudflare, Target: target, TargetIndex: target.Index, TargetID: target.ID, Status: "completed", Success: true, Score: 95, CF: map[string]any{"level": "excellent", "cf_loc": "SG"}}
}

func (d *detailRunner) CheckReputation(ctx context.Context, target Target, expectedCountry string) Result {
	d.expectedCountry = expectedCountry
	return Result{Kind: CheckReputation, Target: target, TargetIndex: target.Index, TargetID: target.ID, Status: "completed", Success: true, Score: 10, Reputation: map[string]any{"risk_level": "low", "country_code": "SG"}}
}

type pipelineRunner struct {
	quickCalls int32
	cfCalls    int32
	repCalls   int32
}

func (p *pipelineRunner) CheckQuick(ctx context.Context, target Target) Result {
	atomic.AddInt32(&p.quickCalls, 1)
	if target.NodeTag == "node-1" {
		return Result{Kind: CheckQuick, Target: target, TargetIndex: target.Index, TargetID: target.ID, Status: "failed", Success: false, Error: "connect refused", Quick: map[string]any{"status": "failed", "failure_reason": "connect_refused"}}
	}
	return Result{Kind: CheckQuick, Target: target, TargetIndex: target.Index, TargetID: target.ID, Status: "completed", Success: true, LatencyMS: 50, Quick: map[string]any{"status": "ok", "latency_ms": int64(50)}}
}

func (p *pipelineRunner) CheckCloudflare(ctx context.Context, target Target) Result {
	atomic.AddInt32(&p.cfCalls, 1)
	return Result{Kind: CheckCloudflare, Target: target, TargetIndex: target.Index, TargetID: target.ID, Status: "completed", Success: true, Score: 90, LatencyMS: 80, CF: map[string]any{"level": "excellent", "score": 90}}
}

func (p *pipelineRunner) CheckReputation(ctx context.Context, target Target, expectedCountry string) Result {
	atomic.AddInt32(&p.repCalls, 1)
	return Result{Kind: CheckReputation, Target: target, TargetIndex: target.Index, TargetID: target.ID, Status: "completed", Success: true, Score: 0, LatencyMS: 40, Reputation: map[string]any{"risk_level": "low", "risk_score": 0}}
}

func TestServiceQuickJobRecordsQuickSummary(t *testing.T) {
	runner := &pipelineRunner{}
	svc := NewService(ServiceOptions{TargetSource: fakeTargetSource{targets: makeTargets(3)}, QuickRunner: runner, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 2})
	defer svc.Shutdown(context.Background())

	snap, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckQuick, Count: 3})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	got := waitForJobStatus(t, svc, snap.ID, JobCompleted, 2*time.Second)
	if got.Summary.Quick["ok"] != 2 || got.Summary.Quick["failed"] != 1 || got.Summary.Quick["connect_refused"] != 1 {
		t.Fatalf("unexpected quick summary: %#v", got.Summary.Quick)
	}
	if atomic.LoadInt32(&runner.cfCalls) != 0 || atomic.LoadInt32(&runner.repCalls) != 0 {
		t.Fatalf("quick job should not run deep checks: cf=%d rep=%d", runner.cfCalls, runner.repCalls)
	}
}

func TestServicePipelineSkipsDeepChecksForQuickFailuresAndScoresRecommendations(t *testing.T) {
	runner := &pipelineRunner{}
	svc := NewService(ServiceOptions{TargetSource: fakeTargetSource{targets: makeTargets(3)}, QuickRunner: runner, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 2})
	defer svc.Shutdown(context.Background())

	snap, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckPipeline, Count: 3})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	got := waitForJobStatus(t, svc, snap.ID, JobCompleted, 2*time.Second)
	if got.Summary.Quick["ok"] != 2 || got.Summary.Quick["failed"] != 1 {
		t.Fatalf("unexpected quick summary: %#v", got.Summary.Quick)
	}
	if got.Summary.Final["recommend"] != 2 || got.Summary.Final["rejected"] != 1 {
		t.Fatalf("unexpected final summary: %#v", got.Summary.Final)
	}
	if atomic.LoadInt32(&runner.cfCalls) != 2 || atomic.LoadInt32(&runner.repCalls) != 2 {
		t.Fatalf("deep checks should run only for quick-ok targets: cf=%d rep=%d", runner.cfCalls, runner.repCalls)
	}
	page := svc.ListResults(snap.ID, ResultQuery{Page: 1, PageSize: 3})
	if len(page.Data) != 3 {
		t.Fatalf("unexpected page: %#v", page)
	}
	if page.Data[0].FinalScore < 75 || !page.Data[0].Recommend {
		t.Fatalf("quick-ok target should be recommended: %#v", page.Data[0])
	}
	if page.Data[1].CF != nil || page.Data[1].Reputation != nil || page.Data[1].Recommend {
		t.Fatalf("quick-failed target should skip deep checks and not recommend: %#v", page.Data[1])
	}
}

func TestRunTargetsStopsWithoutDeadlockWhenEmitRejects(t *testing.T) {
	runner := &trackingRunner{}
	done := make(chan error, 1)
	go func() {
		done <- runTargets(context.Background(), workerConfig{workers: 1, kind: CheckCloudflare, cf: runner}, makeTargets(10), func(Result) bool {
			return false
		}, nil)
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runTargets error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runTargets deadlocked after emit rejected a result")
	}
}

func TestServiceCapsActiveJobs(t *testing.T) {
	runner := &trackingRunner{delay: 50 * time.Millisecond}
	svc := NewService(ServiceOptions{TargetSource: fakeTargetSource{targets: makeTargets(20)}, CloudflareRunner: runner, ReputationRunner: runner, MaxWorkers: 300, MaxActiveJobs: 10})
	defer svc.Shutdown(context.Background())

	first, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare, Count: 20})
	if err != nil {
		t.Fatalf("first CreateJob returned error: %v", err)
	}
	if _, err := svc.CreateJob(context.Background(), JobRequest{Kind: CheckCloudflare, Count: 20}); !errors.Is(err, ErrActiveJob) {
		t.Fatalf("second CreateJob error = %v, want ErrActiveJob", err)
	}
	if max := atomic.LoadInt32(&runner.max); max > defaultMaxWorkers {
		t.Fatalf("worker fanout = %d, want <= %d", max, defaultMaxWorkers)
	}
	_ = svc.CancelJob(first.ID)
}
