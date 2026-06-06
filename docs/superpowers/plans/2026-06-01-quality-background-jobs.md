# Quality Background Jobs Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `#quality` usable for 6000+ nodes by replacing long synchronous full scans with cancellable background quality jobs, progress polling, bounded worker pools, and paginated results while keeping existing small synchronous APIs compatible.

**Architecture:** Add `internal/quality` as the orchestration boundary for jobs, cancellation, progress, bounded execution, and result storage. Keep `internal/cloudflarecheck` and `internal/reputation` as protocol/checker packages, and keep `internal/monitor` as HTTP/auth/runtime adapter only. First implementation is in-memory with deterministic paging and cancellation; durable storage can be added after behavior is stable.

**Tech Stack:** Go HTTP handlers, existing monitor manager snapshots, existing Cloudflare/reputation checkers, React + TanStack Query + Ant Design table/progress UI.

---

## Design Decisions from Multi-Agent Review

- Use a new `internal/quality` package; do not continue growing `internal/monitor/server.go`.
- Keep old `GET /api/cloudflare/check` and `GET /api/reputation/check` synchronous behavior for small scans.
- Add background mode through `POST /api/quality/jobs` and optionally `background=true` on legacy endpoints.
- Bounded worker queues are required; do not launch one goroutine per node for 6000+ checks.
- `monitor.Server` owns route wiring and adapts manager snapshots into `quality.TargetSource`; `internal/quality` must not import `internal/monitor`.
- First version stores recent jobs and results in memory with TTL/max-job limits. Persistent JSON/SQLite is a later phase.
- Result identity should prefer stable proxy identity/hash, not only `NodeTag`.
- Fix free-source ingestion cap usage while touching scale code: call `LoadLimited(remainingCapacity + dedupeBudget)` instead of loading full source then truncating, so existing inline/subscription duplicates do not prevent filling the cap.


---

## API and Data Contract (Reviewer-Required)

### Request/response defaults

- Default `page`: `1`. Values `<1` normalize to `1`.
- Default `page_size`: `100`. Maximum `page_size`: `500`. Values above max clamp to `500`.
- Default `count`: `500` for background jobs if omitted. Maximum background `count`: `10000` in first version.
- Maximum active jobs: `1` running job by default. If another job is running, `POST /api/quality/jobs` returns `409` unless `replace=true` is provided.
- Stored completed jobs: last `20` jobs or `24h`, whichever prunes first. Running jobs must never be pruned.
- Error shape for new APIs:

```json
{ "error": "message", "code": "invalid_request" }
```

### `POST /api/quality/jobs`

Request:

```json
{
  "kind": "combined",
  "region": "all",
  "mode": "multi-port",
  "count": 6000,
  "include_unavailable": true,
  "retry_failed": false,
  "force_refresh": false,
  "replace": false
}
```

Allowed `kind`: `cloudflare`, `reputation`, `combined`.

Response `202`:

```json
{
  "job_id": "quality_20260601_000001",
  "status": "queued",
  "kind": "combined",
  "progress_url": "/api/quality/jobs/quality_20260601_000001",
  "results_url": "/api/quality/jobs/quality_20260601_000001/results"
}
```

### `GET /api/quality/jobs/{id}`

Response:

```json
{
  "id": "quality_20260601_000001",
  "status": "running",
  "kind": "combined",
  "region": "all",
  "total": 6000,
  "queued": 3100,
  "running": 80,
  "completed": 2820,
  "cached": 400,
  "failed": 900,
  "cancelled": 0,
  "percent": 47.0,
  "summary": {
    "cloudflare": {"excellent": 88, "good": 170, "fair": 0, "poor": 0, "failed": 242},
    "reputation": {"low": 135, "medium": 41, "high": 0, "failed": 324}
  },
  "started_at": "2026-06-01T04:01:19Z",
  "finished_at": null,
  "error": ""
}
```

Terminal statuses: `completed`, `failed`, `cancelled`. Cancel is idempotent. A cancelled job must never transition to `completed` later; late worker results may be stored only if job status is still `running`.

### `GET /api/quality/jobs/{id}/results`

Result rows are sorted by stable `target_index` ascending by default. `checked_at` is never the primary sort for live job pages because it causes pagination drift.

Response:

```json
{
  "data": [
    {
      "job_id": "quality_20260601_000001",
      "target_index": 0,
      "target_id": "sha256:...",
      "node_name": "node-1",
      "node_tag": "node-1",
      "region": "sg",
      "source": "free_proxy",
      "host": "127.0.0.1",
      "port": 13001,
      "proxy_url": "http://127.0.0.1:13001",
      "cf": null,
      "reputation": null,
      "status": "pending",
      "error": "",
      "checked_at": null
    }
  ],
  "count": 6000,
  "page": 1,
  "page_size": 100,
  "total_pages": 60,
  "has_next": true
}
```

`combined` rows contain both `cf` and `reputation` sub-objects when available. Frontend tables read this combined row directly; they must not re-merge two unbounded arrays. Charts use `JobSnapshot.summary` for full-job totals, not only the current page.

### Legacy endpoint compatibility

- Without `background=true`/`async=true`, existing sync endpoints keep current behavior, caps, validation, and response fields.
- With `background=true`/`async=true`, legacy endpoints return the same `202` job creation response as `POST /api/quality/jobs`.
- Tests must cover old cap behavior: normal sync max `50`, `include_unavailable`/`scope=all` sync max `500`; background path permits up to `10000`.

### Reputation runner contract

The quality service uses this adapter shape, not a vague single-target reputation call:

```go
type ReputationRunner interface {
    CheckReputation(ctx context.Context, target Target, expectedCountry string) Result
}
```

The monitor adapter may implement this by wrapping existing `reputation.Checker.CheckProxies(ctx, []reputation.ProxyTarget{...}, expectedCountry)` for one target in the first version. This preserves existing `nodeResults` cache semantics while keeping the quality worker bounded at the job layer.

### Cancellation and shutdown contract

- `Service` owns a root context and all job cancel funcs.
- `Service.Shutdown(ctx)` cancels active jobs and waits up to the supplied context deadline.
- `CancelJob(id)` is idempotent and returns the latest snapshot.
- Starting a job stores its cancel func before worker goroutines launch.
- On cancellation, queue producers stop, workers stop consuming new targets, and final status remains `cancelled`.
- `max_active_jobs=1` in first version to protect the service from multiple simultaneous 6000-node scans.

---

## File Structure

### New files

- `internal/quality/types.go`
  - Defines `CheckKind`, `JobStatus`, `Target`, `TargetQuery`, `JobRequest`, `JobSnapshot`, `Result`, `ResultQuery`, `PagedResults`.
- `internal/quality/store.go`
  - In-memory job/result store, TTL/max job pruning, pagination, stable sorting.
- `internal/quality/service.go`
  - Job lifecycle: create, start, cancel, shutdown, get, list results; stores cancel funcs and enforces max active jobs.
- `internal/quality/worker.go`
  - Bounded worker execution and progress updates. Uses interfaces for CF/reputation checks so tests can use fakes. Workers must preserve `target_index` and must not transition cancelled jobs to completed.
- `internal/quality/service_test.go`
  - TDD coverage for job lifecycle, cancellation, pagination, bounded workers, cache/skip semantics.
- `internal/monitor/server_quality.go`
  - HTTP handlers for `/api/quality/jobs`, `/api/quality/jobs/{id}`, `/api/quality/jobs/{id}/results`, cancel.
- `internal/monitor/quality_adapter.go`
  - Adapter from `monitor.Manager` snapshots/config auth to `quality.TargetSource`.
- `internal/monitor/server_quality_test.go`
  - Handler contract tests for non-blocking job creation, progress, results, cancellation.
- `web/src/api/qualityJobs.ts`
  - Client API for background jobs.
- `web/src/types/qualityJob.ts`
  - TypeScript types for job request/progress/results.
- `docs/quality-background-jobs.md`
  - User/operator documentation after implementation.

### Modified files

- `internal/monitor/server.go`
  - Wire `quality.Service` into `Server`, register quality routes, keep legacy handlers.
- `internal/monitor/server_reputation_test.go`
  - Add compatibility tests for old sync reputation endpoint and optional background mode.
- `internal/config/config.go`
  - Use `LoadLimited(remainingCapacity)` for free proxy source loading.
- `internal/config/free_proxy_sources_test.go`
  - Add ingestion cap test proving sources are not fully parsed past global cap.
- `internal/cloudflarecheck/checker.go`
  - Optional: add small single-target interface helpers only if needed; avoid broad rewrite.
- `internal/reputation/checker.go`
  - Optional: add single proxy check interface helpers only if needed; avoid broad rewrite.
- `web/src/pages/QualityPage.tsx`
  - Replace full synchronous scan with create job + progress polling + paged results.
- `web/src/api/cloudflare.ts`, `web/src/api/reputation.ts`
  - Preserve legacy small sync calls; optionally support `background=true` response union.
- `README.md`, `README_ZH.md`, `CF_SCORE.md`
  - Document scalable quality checks and current limitations.

---

## Chunk 1: Backend Foundation and Contracts

### Task 1: Fix free proxy ingestion cap before more scale work

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/free_proxy_sources_test.go`

- [ ] **Step 1: Write failing test for `LoadLimited(remainingCapacity)` behavior**

Use `httptest.Server` sources. The first source returns more valid proxy rows than `free_proxy_max_nodes`; the second source increments a request counter. Assert the second source is never requested after the first source fills the global cap. Also assert output node count/order. If this does not fail before code change, do not weaken the assertion; document that current break behavior already covers cross-source skipping and keep this task focused on passing `remaining` into `LoadLimited`.

Run:
```bash
go test ./internal/config -run 'TestFreeProxy.*Max|TestLoad.*Limited' -count=1
```
Expected: FAIL before implementation if the test specifically expects early limit behavior.

- [ ] **Step 2: Implement remaining-capacity loading**

In `appendFreeProxyNodes`, replace:
```go
sourceNodes, err := provider.Load()
```
with remaining cap calculation:
```go
remaining := 0
if c.FreeProxyMaxNodes > 0 {
    remaining = c.FreeProxyMaxNodes - totalAdded
    if remaining <= 0 {
        break
    }
}
sourceNodes, err := provider.LoadLimited(remaining + len(seen))
```

- [ ] **Step 3: Verify config tests pass**

Run:
```bash
go test ./internal/config -count=1
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go internal/config/free_proxy_sources_test.go
git commit -m "fix: limit free proxy source ingestion"
```

### Task 2: Add `internal/quality` types and in-memory store

**Files:**
- Create: `internal/quality/types.go`
- Create: `internal/quality/store.go`
- Create: `internal/quality/store_test.go`

- [ ] **Step 1: Write failing store tests**

Cover:
- create job snapshot has `queued` status and timestamps.
- progress updates are concurrency safe.
- result pagination returns stable order, `count`, `page`, `page_size`, `total_pages`, `has_next`.
- cancel changes status to `cancelled` and records finish time.

Run:
```bash
go test ./internal/quality -run 'TestStore|TestJob' -count=1
```
Expected: FAIL because package does not exist.

- [ ] **Step 2: Implement minimal types**

Define:
```go
type CheckKind string
const (
    CheckCloudflare CheckKind = "cloudflare"
    CheckReputation CheckKind = "reputation"
    CheckCombined CheckKind = "combined"
)

type JobStatus string
const (
    JobQueued JobStatus = "queued"
    JobRunning JobStatus = "running"
    JobCompleted JobStatus = "completed"
    JobFailed JobStatus = "failed"
    JobCancelled JobStatus = "cancelled"
)
```

Define target/job/result structs with JSON tags suitable for API responses.

- [ ] **Step 3: Implement in-memory store with mutex**

Use `sync.RWMutex`, maps keyed by job ID, and result slices keyed by job ID. Include deterministic sorting by stable `TargetIndex` first, then `TargetID`, then `NodeTag`. `CheckedAt` is only display metadata and must not be the default live-pagination sort key.

- [ ] **Step 4: Verify package tests pass**

Run:
```bash
go test ./internal/quality -count=1
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/quality
git commit -m "feat: add quality job store"
```

### Task 3: Add quality service with cancellable background jobs and bounded workers

**Files:**
- Create: `internal/quality/service.go`
- Create: `internal/quality/worker.go`
- Create: `internal/quality/service_test.go`

- [ ] **Step 1: Write failing service tests with fake target source/checkers**

Cover:
- `CreateJob` returns quickly for 6000 fake targets.
- worker concurrency never exceeds configured limit.
- only one active job is allowed by default; second job returns conflict unless replace is enabled.
- cancellation is idempotent, stops queued work, marks job cancelled, and late worker writes cannot mark it completed.
- shutdown cancels active jobs and waits for workers.
- completed job has summary counts.
- combined job can run CF and reputation checks without blocking caller.
- result pages remain stable while results finish out of order because ordering uses `target_index`.

Run:
```bash
go test ./internal/quality -run 'TestService' -count=1
```
Expected: FAIL.

- [ ] **Step 2: Define interfaces**

```go
type TargetSource interface {
    ListTargets(ctx context.Context, q TargetQuery) ([]Target, error)
}

type CloudflareRunner interface {
    CheckCloudflare(ctx context.Context, target Target) Result
}

type ReputationRunner interface {
    CheckReputation(ctx context.Context, target Target, expectedCountry string) Result
}
```

- [ ] **Step 3: Implement service lifecycle**

`CreateJob(ctx, JobRequest)` should:
- validate request.
- create a job in store.
- create child context with cancel.
- start one goroutine per job.
- return snapshot immediately.

- [ ] **Step 4: Implement bounded worker queue**

Use `jobs := make(chan Target)` and fixed `N` workers. Workers must select on context cancellation before and during queue receive. Do not spawn one goroutine per target.

- [ ] **Step 5: Verify package tests pass, including race targeted test**

Run:
```bash
go test ./internal/quality -count=1
go test -race ./internal/quality -count=1
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/quality
git commit -m "feat: run quality checks as background jobs"
```

---

## Chunk 2: Monitor Integration and Legacy Compatibility

### Task 4: Add monitor target adapter

**Files:**
- Create: `internal/monitor/quality_adapter.go`
- Create/Modify: `internal/monitor/server_quality_test.go`

- [ ] **Step 1: Write failing adapter tests**

Cover:
- `include_unavailable=false` uses healthy-only snapshots.
- `include_unavailable=true` includes all eligible snapshots.
- region filter matches existing extractor region behavior.
- auth proxy URL is built correctly.
- limit 6000 is honored without 500 cap in background path.

Run:
```bash
go test ./internal/monitor -run 'TestQualityTarget' -count=1
```
Expected: FAIL.

- [ ] **Step 2: Implement adapter**

Move shared logic from `buildCloudflareTargets`/`buildReputationTargets` into adapter helpers where practical. Keep old functions as compatibility wrappers if needed.

- [ ] **Step 3: Verify monitor target tests pass**

Run:
```bash
go test ./internal/monitor -run 'TestQualityTarget' -count=1
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/monitor/quality_adapter.go internal/monitor/server_quality_test.go
git commit -m "refactor: add quality target adapter"
```

### Task 5: Add quality job HTTP API

**Files:**
- Create: `internal/monitor/server_quality.go`
- Modify: `internal/monitor/server.go`
- Modify: `internal/monitor/server_quality_test.go`

- [ ] **Step 1: Write failing handler contract tests**

Cover:
- `POST /api/quality/jobs` returns `202` with `job_id`, `progress_url`, `results_url`.
- `GET /api/quality/jobs/{id}` returns status/progress.
- `GET /api/quality/jobs/{id}/results?page=1&page_size=50` returns pagination metadata and rows ordered by `target_index`.
- `POST /api/quality/jobs/{id}/cancel` cancels job.
- unknown job returns `404`.
- invalid kind/region/count returns `400`.

Run:
```bash
go test ./internal/monitor -run 'TestQualityJobAPI' -count=1
```
Expected: FAIL.

- [ ] **Step 2: Wire service into Server**

Add a `qualitySvc *quality.Service` field to `Server`. Initialize it in `NewServer` with monitor adapter and checker runners.

- [ ] **Step 3: Register routes**

Add:
```go
mux.HandleFunc("/api/quality/jobs", s.withAuth(s.handleQualityJobs))
mux.HandleFunc("/api/quality/jobs/", s.withAuth(s.handleQualityJobItem))
```

- [ ] **Step 4: Implement handlers in `server_quality.go`**

Keep HTTP parsing/JSON only here. Delegate all lifecycle to `quality.Service`.

- [ ] **Step 5: Verify monitor API tests pass**

Run:
```bash
go test ./internal/monitor -run 'TestQualityJobAPI' -count=1
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/monitor internal/quality
git commit -m "feat: expose quality background job api"
```

### Task 6: Add background compatibility to legacy quality endpoints

**Files:**
- Modify: `internal/monitor/server.go` or create `internal/monitor/server_quality_compat.go`
- Modify: `internal/monitor/server_reputation_test.go`
- Modify: `internal/monitor/server_quality_test.go`

- [ ] **Step 1: Write failing compatibility tests**

Cover:
- existing sync `/api/cloudflare/check` still returns `200` and old fields.
- existing sync `/api/reputation/check` still returns `200` and old fields.
- sync max caps remain: normal max 50; `include_unavailable=true` or `scope=all` max 500.
- `retry_failed`, `scope=all`, invalid region, invalid mode, and invalid count keep existing status codes and response shapes.
- `/api/cloudflare/check?background=true&count=6000&include_unavailable=true` returns `202` job response.
- `/api/reputation/check?background=true&count=6000&include_unavailable=true` returns `202` job response.

Run:
```bash
go test ./internal/monitor -run 'Test.*Background|Test.*Check' -count=1
```
Expected: FAIL for background cases, old tests should continue passing.

- [ ] **Step 2: Implement background branch before sync max-count truncation**

If `background=true` or `async=true`, create a quality job with relevant check kind and return `202`. Keep sync `maxCount=500` unchanged.

- [ ] **Step 3: Verify compatibility tests pass**

Run:
```bash
go test ./internal/monitor -count=1
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/monitor
git commit -m "feat: support background quality checks"
```

---

## Chunk 3: Frontend and Documentation

### Task 7: Add frontend job API and types

**Files:**
- Create: `web/src/api/qualityJobs.ts`
- Create: `web/src/types/qualityJob.ts`
- Modify: `web/src/api/cloudflare.ts`
- Modify: `web/src/api/reputation.ts`

- [ ] **Step 1: Add types for job request/progress/results**

Define `QualityJobRequest`, `QualityJobSnapshot`, `QualityJobResult`, `PagedQualityResults`.

- [ ] **Step 2: Add API functions**

Functions:
```ts
createQualityJob(req)
getQualityJob(id)
getQualityJobResults(id, params)
cancelQualityJob(id)
```

- [ ] **Step 3: Verify TypeScript**

Run:
```bash
cd web && npm run typecheck
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src/api web/src/types
git commit -m "feat: add quality job client api"
```

### Task 8: Convert QualityPage full scan to background job

**Files:**
- Modify: `web/src/pages/QualityPage.tsx`

- [ ] **Step 1: Replace full scan mutation**

“One-click scan all” should call `createQualityJob({kind:'combined', include_unavailable:true, count: allCount})` and store `jobId`.

- [ ] **Step 2: Add progress polling**

Poll `getQualityJob(jobId)` every 1s while queued/running, stop on completed/failed/cancelled.

- [ ] **Step 3: Add progress UI**

Show status, total, completed, failed, cached, percent, elapsed time, cancel button.

- [ ] **Step 4: Load paginated combined results**

After job starts, load `getQualityJobResults(jobId, {page, page_size})`. Keep table pagination server-driven for job results. The table consumes combined result rows directly (`row.cf`, `row.reputation`) and must not merge two full CF/reputation arrays. Sorting/filtering applies to server result parameters or current page only; full-job metric cards and charts use `job.summary`, not current page counts.

- [ ] **Step 5: Preserve small sample sync CF detection**

Do not remove `抽样检测 CF`; keep it using legacy sync endpoint.

- [ ] **Step 6: Verify frontend build**

Run:
```bash
cd web && npm run typecheck
cd web && npm run build
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/pages/QualityPage.tsx
git commit -m "feat: run quality page scans in background"
```

### Task 9: Documentation and operational notes

**Files:**
- Create: `docs/quality-background-jobs.md`
- Modify: `README.md`
- Modify: `README_ZH.md`
- Modify: `CF_SCORE.md`

- [ ] **Step 1: Document API examples**

Include curl examples for create/progress/results/cancel.

- [ ] **Step 2: Document 6000+ behavior**

Explain background jobs, worker limits, current in-memory retention, and old sync endpoint cap.

- [ ] **Step 3: Commit**

```bash
git add docs/quality-background-jobs.md README.md README_ZH.md CF_SCORE.md
git commit -m "docs: document scalable quality checks"
```

---

## Chunk 4: Verification, Review, and Handoff

### Task 10: Full verification

**Files:** no code changes expected.

- [ ] **Step 1: Run backend targeted tests**

```bash
go test ./internal/quality ./internal/monitor ./internal/config ./internal/cloudflarecheck ./internal/reputation -count=1
```
Expected: PASS.

- [ ] **Step 2: Run full Go tests**

```bash
go test ./... -count=1
```
Expected: PASS.

- [ ] **Step 3: Run race tests for concurrency-sensitive packages**

```bash
go test -race ./internal/quality ./internal/monitor -count=1
```
Expected: PASS.

- [ ] **Step 4: Run frontend checks**

```bash
cd web && npm run typecheck
cd web && npm run build
```
Expected: PASS.

### Task 11: Multi-agent review

- [ ] **Step 1: Dispatch reviewer agent for architecture**

Review scope:
- `internal/quality` package boundaries.
- no `internal/quality -> internal/monitor` import.
- cancellation and goroutine lifecycle.
- bounded workers.
- legacy endpoint compatibility.

- [ ] **Step 2: Dispatch reviewer agent for frontend/API contract**

Review scope:
- frontend polling stops correctly.
- cancel behavior.
- paginated result handling.
- old cache/sample behavior still usable.

- [ ] **Step 3: Fix critical/important findings**

Run relevant tests after each fix.

### Task 12: Runtime scale smoke test

**Files:** optional scripts under `/tmp` only.

- [ ] **Step 1: Build binary**

```bash
go build -o /tmp/easy-proxies-quality-test/easy_proxies_quality ./cmd/easy-proxies
```
Expected: exit 0.

- [ ] **Step 2: Start isolated service**

Use ports distinct from main service:
```text
WebUI: 127.0.0.1:19091
Pool: 127.0.0.1:22323
Clash API: 127.0.0.1:29092
```

- [ ] **Step 3: Create background quality job for 6000+ nodes**

```bash
curl --noproxy '*' -fsS -X POST http://127.0.0.1:19091/api/quality/jobs \
  -H 'Content-Type: application/json' \
  -d '{"kind":"combined","region":"all","count":6000,"include_unavailable":true}'
```
Expected: returns `202`/job JSON quickly, without waiting minutes.

- [ ] **Step 4: Poll progress and fetch first results page**

Expected:
- progress endpoint responds while job runs.
- results endpoint returns paginated rows.
- service remains responsive.

---

## Review Notes Already Incorporated

- Explorer confirmed current bottlenecks: sync request, 500 cap, checker concurrency 5, unpaginated cache, frontend loads/sorts all rows.
- Planner recommended staged job API, compatibility mode, frontend polling, and small commits.
- Architect required `internal/quality`, bounded workers, cancelable scheduler/job contexts, no monitor import cycle, and no NodeTag-only cache identity.



## Multi-Agent Ownership Rules

- Only one worker may edit `internal/monitor/server.go` at a time.
- `internal/quality/*` is owned by the quality backend worker until Task 3 completes.
- `internal/monitor/server_quality.go` and `internal/monitor/quality_adapter.go` are owned by the monitor integration worker after `internal/quality` APIs stabilize.
- `web/src/api/qualityJobs.ts` and `web/src/types/qualityJob.ts` must be completed before any worker edits `QualityPage.tsx`.
- Commits are made by the coordinator after verifying each worker diff unless a worker has exclusive ownership and no parallel edits are active.
- Reviewers are read-only and must not edit files.
