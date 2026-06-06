# Proxy Tiering Multi-Agent Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a rules-based proxy tiering system that classifies proxies into Reject, Rescue, HTTP-only, Simple Web, Recommended, Strict, and AI candidate pools, then exposes these tiers through backend jobs, scripts, and WebUI without overloading the host during 6000+ node scans.

**Architecture:** Add a focused tiering layer on top of the existing `internal/quality` background job pipeline. Keep probing/checking logic in runners, compute deterministic tier decisions in a pure rules package, persist tier fields in quality results, and render/filter tiers in the quality UI. Multi-agent work is split by disjoint ownership: rules engine, probe/capability collection, API/store integration, frontend, documentation/verification.

**Tech Stack:** Go, existing `internal/quality` job service, existing Cloudflare/reputation checkers, bounded worker queues, Python classification scripts for offline analysis, React + Ant Design WebUI.

---

## Required Design Reference

Before implementation, read:

- `docs/proxy-tiering-rules.md`
- `internal/quality/types.go`
- `internal/quality/worker.go`
- `internal/monitor/quality_adapter.go`
- `scripts/classify_free_proxies.py`
- `docs/quality-background-jobs.md`

Safety constraint: do not run broad scans while implementing. Unit tests and small fixtures only. Any later live scan must use bounded workers; curl subprocess concurrency must stay <=80.

---

## Multi-Agent Execution Model

### Agent A — Rules Engine Owner

**Ownership:**

- Create: `internal/quality/tiering.go`
- Create: `internal/quality/tiering_test.go`
- Modify only if needed: `internal/quality/types.go`

**Output:** pure tier computation with no network calls.

**Tasks:**

- [ ] Define `Tier`, `Capability`, `TierDecision`, and reason codes.
- [ ] Implement `ClassifyResult(result Result) TierDecision`.
- [ ] Implement score helper using `quick`, `cf`, `reputation`, latency, capabilities, exit duplication hints.
- [ ] Unit-test hard gates:
  - quick failed => T0 reject;
  - quick only => T1 rescue;
  - HTTP methods without HTTPS => T2 HTTP-only;
  - HTTPS + exit IP + acceptable latency => T3;
  - CF good + low risk => T4;
  - CF excellent + low risk + strict/AI matrix => T5.

### Agent B — Capability Probe Owner

**Ownership:**

- Modify: `scripts/classify_free_proxies.py`
- Create: `docs/proxy-probe-matrix.md`

**Output:** offline capability labels compatible with `docs/proxy-tiering-rules.md`.

**Tasks:**

- [ ] Keep subprocess concurrency bounded by a CLI `--concurrency` default 40 and hard max 80.
- [ ] Ensure the script does not create all proxy/probe tasks upfront; use bounded queue/worker pattern.
- [ ] Emit normalized capabilities: `http_basic`, `http_methods`, `https_basic`, `https_cf`, `general_web`, `ai_web`, `strict_web`.
- [ ] Emit per-proxy JSONL fields usable by Go/import tools later.
- [ ] Add documentation explaining which probes map to which capability.

### Agent C — Backend API/Store Integration Owner

**Ownership:**

- Modify: `internal/quality/types.go`
- Modify: `internal/quality/store.go`
- Modify: `internal/quality/worker.go`
- Modify tests: `internal/quality/store_test.go`, `internal/quality/service_test.go`

**Output:** quality job results carry tier data and summaries.

**Tasks:**

- [ ] Add result fields: `tier`, `tier_score`, `capabilities`, `tier_reasons`, `pool`.
- [ ] Run tier classification after pipeline result is assembled.
- [ ] Extend `JobSummary` with tier counts and pool counts.
- [ ] Preserve pagination stability.
- [ ] Unit-test summaries and paged result JSON shape.

### Agent D — Monitor/API Adapter Owner

**Ownership:**

- Modify: `internal/monitor/server_quality.go`
- Modify: `internal/monitor/server_quality_test.go`
- Modify: `internal/monitor/quality_adapter.go` only if needed

**Output:** API can filter/list by tier without breaking existing job endpoints.

**Tasks:**

- [ ] Add optional query filters: `tier`, `pool`, `capability` to results endpoint.
- [ ] Keep defaults backward compatible.
- [ ] Add handler tests for filtering and invalid filters.
- [ ] Ensure legacy Cloudflare/reputation endpoints remain unchanged.

### Agent E — Frontend Owner

**Ownership:**

- Modify: `web/src/types/qualityJob.ts`
- Modify: `web/src/api/qualityJobs.ts`
- Modify: `web/src/pages/QualityPage.tsx`

**Output:** WebUI displays tier, pool, capabilities, and filters.

**Tasks:**

- [ ] Add tier badges: T0/T1/T2/T3/T4/T5.
- [ ] Add filters for tier and pool.
- [ ] Show reason codes in expandable row or tooltip.
- [ ] Keep server-side pagination; do not fetch all results.
- [ ] Run `npm run typecheck` and `npm run build`.

### Agent F — Reviewer/Verification Owner

**Ownership:** read-only review, no direct edits unless explicitly assigned after review.

**Output:** risk report and verification checklist.

**Tasks:**

- [ ] Verify no unbounded goroutine/task/subprocess fanout.
- [ ] Verify 6000+ path uses background jobs and paginated API.
- [ ] Verify grading rules match `docs/proxy-tiering-rules.md`.
- [ ] Verify tests cover hard gates and summaries.
- [ ] Verify no main service restart is required for offline scripts.

---

## Sequential Integration Order

### Phase 1 — Rule Contract

- [ ] Agent A implements pure tiering rules and tests.
- [ ] Main coordinator reviews `internal/quality/tiering.go` for deterministic behavior.
- [ ] Run: `GOCACHE=/tmp/go-build-cache go test ./internal/quality -count=1`.

### Phase 2 — Result Data Contract

- [ ] Agent C integrates tier fields into pipeline result creation and store summaries.
- [ ] Run: `GOCACHE=/tmp/go-build-cache go test ./internal/quality -count=1`.

### Phase 3 — API Filtering

- [ ] Agent D adds result filters.
- [ ] Run: `GOCACHE=/tmp/go-build-cache go test ./internal/monitor -run Quality -count=1`.

### Phase 4 — Offline Classification Tooling

- [ ] Agent B updates the Python classifier safely.
- [ ] Run only small fixture tests or `--limit 5 --concurrency 4`; do not run large live scan.

### Phase 5 — WebUI

- [ ] Agent E updates UI.
- [ ] Run from `web/`: `npm run typecheck`.
- [ ] Run from `web/`: `npm run build`.

### Phase 6 — Full Verification

- [ ] Run: `GOCACHE=/tmp/go-build-cache go test ./... -count=1`.
- [ ] Run: `cd web && npm run typecheck && npm run build`.
- [ ] Ask before starting any live 6000+ scan.
- [ ] If live validation is approved, use isolated service and import only T4/T5 candidates first.

---

## Acceptance Criteria

The feature is complete only when:

1. Every quality pipeline result has deterministic `tier`, `tier_score`, `pool`, `capabilities`, and `tier_reasons`.
2. Job summaries include tier distribution.
3. Results API supports server-side filtering by tier/pool/capability.
4. WebUI can filter and explain proxy tiers without loading all rows.
5. Offline classifier emits compatible capability labels and remains bounded.
6. Tests prove hard gate behavior and pagination stability.
7. No scan implementation creates unbounded tasks, goroutines, or subprocesses.
8. Documentation explains when to import T2/T3/T4/T5 pools and when to reject/rescue.

---

## Initial Pool Policy for Current Data

Using current scan evidence:

```text
111,319 raw candidates
29,831 quick_ok
205 exit_ip_ok
34 normal_low_risk
78 mobile_low_risk
93 risk_proxy_or_hosting
```

Initial import recommendation:

```text
recommended_pool = normal_low_risk + mobile_low_risk = 112 proxies
risk_proxy_or_hosting = backup only, not default recommended
quick_ok without exit_ip = rescue only, not imported
```

This policy avoids freezing the service and avoids filling the UI with weak proxies that only pass socket-level checks.
