package quality

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestStoreCreateJobSnapshotQueued(t *testing.T) {
	store := NewStore()

	snapshot, err := store.CreateJob(JobRequest{
		Kind: CheckCloudflare,
		Targets: []Target{
			{ID: "node-1", NodeTag: "node-1", ProxyURL: "socks://127.0.0.1:1001"},
			{ID: "node-2", NodeTag: "node-2", ProxyURL: "socks://127.0.0.1:1002"},
		},
	})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}

	if snapshot.ID == "" {
		t.Fatal("expected generated job ID")
	}
	if snapshot.Kind != CheckCloudflare {
		t.Fatalf("kind = %q, want %q", snapshot.Kind, CheckCloudflare)
	}
	if snapshot.Status != JobQueued {
		t.Fatalf("status = %q, want %q", snapshot.Status, JobQueued)
	}
	if snapshot.Total != 2 {
		t.Fatalf("total = %d, want 2", snapshot.Total)
	}
	if snapshot.CreatedAt.IsZero() || snapshot.UpdatedAt.IsZero() {
		t.Fatalf("expected created/updated timestamps: %#v", snapshot)
	}
	if !snapshot.StartedAt.IsZero() || !snapshot.FinishedAt.IsZero() {
		t.Fatalf("new queued job should not have start/finish times: %#v", snapshot)
	}

	got, ok := store.GetJob(snapshot.ID)
	if !ok {
		t.Fatal("expected stored job snapshot")
	}
	if got.ID != snapshot.ID || got.Status != JobQueued || got.Total != 2 {
		t.Fatalf("unexpected stored snapshot: %#v", got)
	}
}

func TestStoreProgressUpdatesAreConcurrencySafe(t *testing.T) {
	store := NewStore()
	snapshot, err := store.CreateJob(JobRequest{Kind: CheckCombined, Targets: make([]Target, 100)})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if err := store.StartJob(snapshot.ID); err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}

	var wg sync.WaitGroup
	for i := 1; i <= 100; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.UpdateProgress(snapshot.ID, i, i/10, fmt.Sprintf("processed %d", i)); err != nil {
				t.Errorf("UpdateProgress returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	got, ok := store.GetJob(snapshot.ID)
	if !ok {
		t.Fatal("expected job snapshot")
	}
	if got.Status != JobRunning {
		t.Fatalf("status = %q, want %q", got.Status, JobRunning)
	}
	if got.Completed < 1 || got.Completed > 100 {
		t.Fatalf("completed = %d, want within [1,100]", got.Completed)
	}
	if got.Failed < 0 || got.Failed > 10 {
		t.Fatalf("failed = %d, want within [0,10]", got.Failed)
	}
	if got.UpdatedAt.Before(got.StartedAt) {
		t.Fatalf("updated_at should not be before started_at: %#v", got)
	}
}

func TestStoreCancelJobRecordsFinishedTime(t *testing.T) {
	store := NewStore()
	snapshot, err := store.CreateJob(JobRequest{Kind: CheckReputation, Targets: []Target{{ID: "node-1"}, {ID: "node-2"}, {ID: "node-3"}}})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if err := store.StartJob(snapshot.ID); err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}
	if err := store.UpdateProgress(snapshot.ID, 1, 0, "processed one"); err != nil {
		t.Fatalf("UpdateProgress returned error: %v", err)
	}

	if err := store.CancelJob(snapshot.ID, "user requested"); err != nil {
		t.Fatalf("CancelJob returned error: %v", err)
	}

	got, ok := store.GetJob(snapshot.ID)
	if !ok {
		t.Fatal("expected job snapshot")
	}
	if got.Status != JobCancelled {
		t.Fatalf("status = %q, want %q", got.Status, JobCancelled)
	}
	if got.FinishedAt.IsZero() {
		t.Fatal("expected finished_at to be recorded")
	}
	if got.Message != "user requested" {
		t.Fatalf("message = %q, want user requested", got.Message)
	}
	if got.Completed != 1 || got.Cancelled != 2 || got.Queued != 0 {
		t.Fatalf("cancel counters = completed:%d cancelled:%d queued:%d, want 1/2/0", got.Completed, got.Cancelled, got.Queued)
	}
	if got.Percent != 100 {
		t.Fatalf("cancelled job percent = %f, want 100", got.Percent)
	}
}

func TestStoreResultPaginationMetadata(t *testing.T) {
	store := NewStore()
	snapshot, err := store.CreateJob(JobRequest{Kind: CheckCloudflare})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if err := store.StartJob(snapshot.ID); err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		if err := store.AddResult(snapshot.ID, Result{
			JobID:     snapshot.ID,
			Kind:      CheckCloudflare,
			NodeTag:   fmt.Sprintf("node-%02d", i),
			ProxyURL:  fmt.Sprintf("socks://127.0.0.1:%d", 1000+i),
			CheckedAt: base.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("AddResult returned error: %v", err)
		}
	}

	page := store.ListResults(snapshot.ID, ResultQuery{Page: 2, PageSize: 2})
	if page.Count != 5 || page.Page != 2 || page.PageSize != 2 || page.TotalPages != 3 || !page.HasNext {
		t.Fatalf("unexpected pagination metadata: %#v", page)
	}
	if len(page.Data) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(page.Data))
	}
	if page.Data[0].NodeTag != "node-03" || page.Data[1].NodeTag != "node-04" {
		t.Fatalf("unexpected page items: %#v", page.Data)
	}
}

func TestStoreListResultsHugePageDoesNotOverflow(t *testing.T) {
	store := NewStore()
	snapshot, err := store.CreateJob(JobRequest{Kind: CheckCloudflare})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if err := store.StartJob(snapshot.ID); err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}
	if err := store.AddResult(snapshot.ID, Result{
		JobID:    snapshot.ID,
		Kind:     CheckCloudflare,
		NodeTag:  "node-01",
		ProxyURL: "socks://127.0.0.1:1001",
	}); err != nil {
		t.Fatalf("AddResult returned error: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ListResults panicked for huge page: %v", r)
		}
	}()

	page := store.ListResults(snapshot.ID, ResultQuery{Page: int(^uint(0) >> 1), PageSize: 500})
	if page.Count != 1 || page.PageSize != 500 || page.TotalPages != 1 || page.HasNext || len(page.Data) != 0 {
		t.Fatalf("huge page should return a safe empty page, got %#v", page)
	}
}

func TestStoreResultStableOrder(t *testing.T) {
	store := NewStore()
	snapshot, err := store.CreateJob(JobRequest{Kind: CheckCombined})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if err := store.StartJob(snapshot.ID); err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}
	sameTime := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	inputs := []Result{
		{JobID: snapshot.ID, Kind: CheckCombined, TargetIndex: 3, NodeTag: "b", ProxyURL: "socks://host:1002", Port: 1002, CheckedAt: sameTime},
		{JobID: snapshot.ID, Kind: CheckCombined, TargetIndex: 1, NodeTag: "a", ProxyURL: "socks://host:1003", Port: 1003, CheckedAt: sameTime.Add(3 * time.Second)},
		{JobID: snapshot.ID, Kind: CheckCombined, TargetIndex: 2, NodeTag: "a", ProxyURL: "socks://host:1001", Port: 1001, CheckedAt: sameTime.Add(time.Second)},
		{JobID: snapshot.ID, Kind: CheckCombined, TargetIndex: 0, NodeTag: "late", ProxyURL: "socks://host:1000", Port: 1000, CheckedAt: sameTime.Add(10 * time.Second)},
	}
	for _, result := range inputs {
		if err := store.AddResult(snapshot.ID, result); err != nil {
			t.Fatalf("AddResult returned error: %v", err)
		}
	}

	first := store.ListResults(snapshot.ID, ResultQuery{Page: 1, PageSize: 10})
	second := store.ListResults(snapshot.ID, ResultQuery{Page: 1, PageSize: 10})
	want := []string{"late|socks://host:1000|1000", "a|socks://host:1003|1003", "a|socks://host:1001|1001", "b|socks://host:1002|1002"}
	if got := orderKeys(first.Data); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("first order = %v, want %v", got, want)
	}
	if got := orderKeys(second.Data); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("second order = %v, want %v", got, want)
	}
}

func orderKeys(results []Result) []string {
	keys := make([]string, 0, len(results))
	for _, result := range results {
		keys = append(keys, fmt.Sprintf("%s|%s|%d", result.NodeTag, result.ProxyURL, result.Port))
	}
	return keys
}

func TestStoreCancelledJobCannotBecomeCompletedOrAcceptLateResults(t *testing.T) {
	store := NewStore()
	snapshot, err := store.CreateJob(JobRequest{Kind: CheckCombined, Targets: []Target{{Index: 0, ID: "node-1"}}})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if err := store.StartJob(snapshot.ID); err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}
	if err := store.CancelJob(snapshot.ID, "user requested"); err != nil {
		t.Fatalf("CancelJob returned error: %v", err)
	}
	if err := store.CompleteJob(snapshot.ID, "late completion"); err == nil {
		t.Fatal("expected CompleteJob after cancellation to fail")
	}
	if err := store.AddResult(snapshot.ID, Result{TargetIndex: 0, TargetID: "node-1", Success: true}); err == nil {
		t.Fatal("expected AddResult after cancellation to fail")
	}
	got, ok := store.GetJob(snapshot.ID)
	if !ok {
		t.Fatal("expected job snapshot")
	}
	if got.Status != JobCancelled {
		t.Fatalf("status = %q, want %q", got.Status, JobCancelled)
	}
	if got.Cancelled != 1 || got.Queued != 0 {
		t.Fatalf("cancelled counters = cancelled:%d queued:%d, want 1/0", got.Cancelled, got.Queued)
	}
	page := store.ListResults(snapshot.ID, ResultQuery{Page: 1, PageSize: 10})
	if page.Count != 0 || len(page.Data) != 0 {
		t.Fatalf("cancelled job should not expose synthetic pending target rows, got %#v", page)
	}
}

func TestStoreListResultsIncludesPendingTargetsWithStableCount(t *testing.T) {
	store := NewStore()
	snapshot, err := store.CreateJob(JobRequest{Kind: CheckCombined, Targets: []Target{
		{Index: 0, ID: "node-0", NodeTag: "node-0"},
		{Index: 1, ID: "node-1", NodeTag: "node-1"},
		{Index: 2, ID: "node-2", NodeTag: "node-2"},
	}})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if err := store.StartJob(snapshot.ID); err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}
	if err := store.AddResult(snapshot.ID, Result{TargetIndex: 2, TargetID: "node-2", NodeTag: "node-2", Status: "completed", Success: true}); err != nil {
		t.Fatalf("AddResult returned error: %v", err)
	}

	page := store.ListResults(snapshot.ID, ResultQuery{Page: 1, PageSize: 2})
	if page.Count != 3 || page.TotalPages != 2 || !page.HasNext {
		t.Fatalf("unexpected pagination metadata: %#v", page)
	}
	if got := []string{page.Data[0].NodeTag + ":" + page.Data[0].Status, page.Data[1].NodeTag + ":" + page.Data[1].Status}; fmt.Sprint(got) != fmt.Sprint([]string{"node-0:pending", "node-1:pending"}) {
		t.Fatalf("unexpected first page rows: %v", got)
	}
	page2 := store.ListResults(snapshot.ID, ResultQuery{Page: 2, PageSize: 2})
	if len(page2.Data) != 1 || page2.Data[0].NodeTag != "node-2" || page2.Data[0].Status != "completed" {
		t.Fatalf("unexpected second page rows: %#v", page2.Data)
	}
}

func TestStoreTerminalResultsDoNotSynthesizePendingTargets(t *testing.T) {
	store := NewStore()
	snapshot, err := store.CreateJob(JobRequest{Kind: CheckCombined, Targets: []Target{
		{Index: 0, ID: "node-0", NodeTag: "node-0"},
		{Index: 1, ID: "node-1", NodeTag: "node-1"},
	}})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if err := store.StartJob(snapshot.ID); err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}
	if err := store.AddResult(snapshot.ID, Result{TargetIndex: 1, TargetID: "node-1", NodeTag: "node-1", Status: "completed", Success: true}); err != nil {
		t.Fatalf("AddResult returned error: %v", err)
	}
	if err := store.CancelJob(snapshot.ID, "cancelled"); err != nil {
		t.Fatalf("CancelJob returned error: %v", err)
	}

	page := store.ListResults(snapshot.ID, ResultQuery{Page: 1, PageSize: 10})
	if page.Count != 1 || len(page.Data) != 1 {
		t.Fatalf("terminal job should expose only actual results, got %#v", page)
	}
	if page.Data[0].NodeTag != "node-1" || page.Data[0].Status != "completed" {
		t.Fatalf("unexpected terminal result rows: %#v", page.Data)
	}
}

func TestStoreTerminalJobIgnoresLateProgressUpdates(t *testing.T) {
	store := NewStore()
	snapshot, err := store.CreateJob(JobRequest{Kind: CheckCombined, Targets: []Target{{Index: 0, ID: "node-1"}, {Index: 1, ID: "node-2"}}})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if err := store.StartJob(snapshot.ID); err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}
	if err := store.CancelJob(snapshot.ID, "user requested"); err != nil {
		t.Fatalf("CancelJob returned error: %v", err)
	}
	before, ok := store.GetJob(snapshot.ID)
	if !ok {
		t.Fatal("expected job snapshot")
	}
	if err := store.UpdateProgress(snapshot.ID, 2, 1, "late progress"); err == nil {
		t.Fatal("expected UpdateProgress after terminal status to fail")
	}
	after, ok := store.GetJob(snapshot.ID)
	if !ok {
		t.Fatal("expected job snapshot")
	}
	if after.Status != before.Status || after.Completed != before.Completed || after.Failed != before.Failed || after.Queued != before.Queued || after.Percent != before.Percent || after.Message != before.Message {
		t.Fatalf("terminal snapshot mutated by late progress: before=%#v after=%#v", before, after)
	}
}

func TestStorePrunesOldTerminalJobsButKeepsRunningJobs(t *testing.T) {
	store := NewStore()
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return base }

	running, err := store.CreateJob(JobRequest{Kind: CheckCloudflare, Targets: []Target{{ID: "running"}}})
	if err != nil {
		t.Fatalf("CreateJob running returned error: %v", err)
	}
	if err := store.StartJob(running.ID); err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}

	var terminalIDs []string
	for i := 0; i < maxStoredTerminalJobs+5; i++ {
		i := i
		store.now = func() time.Time { return base.Add(time.Duration(i+1) * time.Minute) }
		snapshot, err := store.CreateJob(JobRequest{Kind: CheckCloudflare, Targets: []Target{{ID: fmt.Sprintf("terminal-%d", i)}}})
		if err != nil {
			t.Fatalf("CreateJob terminal returned error: %v", err)
		}
		if err := store.StartJob(snapshot.ID); err != nil {
			t.Fatalf("StartJob terminal returned error: %v", err)
		}
		if err := store.CompleteJob(snapshot.ID, "done"); err != nil {
			t.Fatalf("CompleteJob returned error: %v", err)
		}
		terminalIDs = append(terminalIDs, snapshot.ID)
	}

	if _, ok := store.GetJob(running.ID); !ok {
		t.Fatal("running job should not be pruned")
	}
	if _, ok := store.GetJob(terminalIDs[0]); ok {
		t.Fatal("oldest terminal job should be pruned")
	}
	if _, ok := store.GetJob(terminalIDs[len(terminalIDs)-1]); !ok {
		t.Fatal("newest terminal job should be retained")
	}
}

func TestStoreListResultsHugePageSizeIsClampedSafely(t *testing.T) {
	store := NewStore()
	snapshot, err := store.CreateJob(JobRequest{Kind: CheckCloudflare})
	if err != nil {
		t.Fatalf("CreateJob returned error: %v", err)
	}
	if err := store.StartJob(snapshot.ID); err != nil {
		t.Fatalf("StartJob returned error: %v", err)
	}
	if err := store.AddResult(snapshot.ID, Result{JobID: snapshot.ID, Kind: CheckCloudflare, NodeTag: "node-01"}); err != nil {
		t.Fatalf("AddResult returned error: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ListResults panicked for huge page_size: %v", r)
		}
	}()

	page := store.ListResults(snapshot.ID, ResultQuery{Page: 1, PageSize: int(^uint(0) >> 1)})
	if page.Page != 1 || page.PageSize != maxResultPageSize || page.Count != 1 || page.TotalPages != 1 || page.HasNext || len(page.Data) != 1 {
		t.Fatalf("huge page_size should be clamped to a safe page, got %#v", page)
	}
}

func TestTotalPagesForCountAvoidsIntegerOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if got := totalPagesForCount(maxInt, maxResultPageSize); got <= 0 {
		t.Fatalf("totalPagesForCount(maxInt, maxPageSize) = %d, want positive safe value", got)
	}
	if got := totalPagesForCount(maxInt, maxInt); got != 1 {
		t.Fatalf("totalPagesForCount(maxInt, maxInt) = %d, want 1", got)
	}
	if got := totalPagesForCount(0, maxResultPageSize); got != 0 {
		t.Fatalf("totalPagesForCount(0, maxPageSize) = %d, want 0", got)
	}
}
