# Free Proxy Scale + Paginated Overview Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Safely support large 6000+ free-proxy source lists by loading subscription nodes first, then a bounded/deduplicated free-proxy runtime set, and make `/api/nodes` + `#overview` usable with pagination and filters.

**Architecture:** Treat free proxies as bounded runtime inputs, not an unbounded always-active pool. Extract shared runtime-source composition so startup/reload/subscription refresh all materialize sources in one order: inline/file/subscription/cache first, then deduplicated free proxies up to per-source/global limits. Monitor metadata will carry node `source`; `/api/nodes` will keep legacy no-query behavior but add explicit paged/filter mode and metadata-only summary; WebUI overview/topbar/auth probe will avoid unbounded full-node fetches.

**Tech Stack:** Go 1.24, existing `internal/nodesource`, `internal/config`, `internal/monitor`, `internal/builder`, React 19 + TanStack Query + Ant Design.

---

## Reviewed Requirements

1. Subscription sources have priority: load subscriptions and persist/cache them before free proxies are appended.
2. Shared runtime-source composition must be used consistently by startup, manual reload, and subscription refresh; free proxies must not be appended twice or kept stale across reloads.
3. Free proxy sources can be enabled safely with limits:
   - per-source `max_nodes`
   - global `free_proxy_max_nodes`
   - per-source/file `max_bytes` with safe default to prevent unbounded reads/parses
   - URI deduplication against existing subscription/file/inline nodes and across free sources
   - disabled sources skipped
4. Runtime must not persist free-proxy nodes back to `nodes.txt` or `config.yaml` node arrays.
5. Monitor/API must expose source and support pagination/filtering:
   - `page`, `page_size`
   - `region`, `availability`, `latency`, `source`, `q`, `sort`
   - response includes `total_nodes`, `total_filtered`, `page`, `page_size`, `has_next`, `region_stats`, `region_healthy`, `source_stats`
6. `/api/nodes` compatibility:
   - plain `/api/nodes` remains exactly legacy shape and semantics (`SnapshotFiltered(true)` in `nodes`)
   - unknown query params such as `?_=` do not trigger paged mode
   - paged mode triggers only if one of `page`, `page_size`, `region`, `availability`, `latency`, `source`, `q`, `sort`, or `summary_only` is present
   - paged/filter mode starts from all `Snapshot()` nodes so `unavailable`/`blacklisted` filters work
7. Add metadata-only summary mode for always-mounted UI/auth code so the app does not fetch thousands of nodes unnecessarily.
8. WebUI overview must use server-side pagination/filtering and build filter options/summary from server metadata, not current-page rows.
9. Verification must run in an isolated worktree and use isolated config/ports/logs/Clash API/synthetic sources to avoid disrupting the current service.

## File Structure

- Modify `internal/nodesource/source.go`: add `MaxNodes`, `MaxBytes`, load options, bounded reads, per-source truncation, and parser options.
- Modify `internal/nodesource/source_test.go`: tests for max nodes, max bytes, disabled sources, limited TXT/JSON parsing.
- Modify `internal/config/config.go`: add `FreeProxyMaxNodes`, shared runtime source composition, dedupe and limits, preserve runtime-only behavior.
- Modify `internal/config/free_proxy_sources_test.go`: tests for subscription priority, global/per-source caps, dedupe, no double-append.
- Inspect/modify `internal/subscription/manager.go` and `internal/boxmgr/manager.go`: route refresh/reload through shared composition or ensure runtime sources are reloaded consistently.
- Modify `internal/outbound/pool/pool.go`: add `Source` to metadata and monitor registration.
- Modify `internal/builder/builder.go`: pass node source into pool metadata; make Clash API controller configurable for isolated verification if feasible.
- Modify `internal/monitor/manager.go`: add source to `NodeInfo` snapshots.
- Modify `internal/monitor/server.go`: add `/api/nodes` filtering/pagination/summary helpers and tests.
- Modify `web/src/types/node.ts`: add `source`, `NodesPage`, `NodeSummary` types.
- Modify `web/src/api/nodes.ts`: add `getNodesPage(params)` and `getNodesSummary()` while preserving `getNodes()`.
- Modify `web/src/App.tsx` and `web/src/components/layout/Topbar.tsx`: use summary/auth endpoint instead of full node list.
- Modify `web/src/pages/NodeOverviewPage.tsx`: server-side filters, pagination controls, source filter, total/filtered summary.
- Modify docs/config examples: document safe defaults and warning against unbounded activation.

---

## Chunk 1: Backend Load Limits and Shared Runtime Composition

### Task 1: Add free-proxy source read and node limits

**Files:**
- Modify: `internal/nodesource/source.go`
- Modify: `internal/nodesource/source_test.go`

- [ ] Add `MaxNodes int yaml/json:"max_nodes"` and `MaxBytes int64 yaml/json:"max_bytes"` to `SourceConfig`.
- [ ] Add parser option support so provider can stop after `MaxNodes` valid entries.
- [ ] Apply `MaxBytes` to remote/file reads before unmarshal; default to a safe value such as 2 MiB when unset.
- [ ] For TXT parsing, scan/parse line-by-line and stop after max valid nodes.
- [ ] For JSON parsing, reject oversized content before unmarshal and cap accepted entries.
- [ ] Keep `ParseFreeProxyContent(format, data)` backward-compatible by delegating to unlimited/legacy options.
- [ ] Add tests proving `max_nodes: 2` returns only two valid nodes from TXT and JSON.
- [ ] Add tests proving oversized source errors before unmarshal.
- [ ] Run: `GOCACHE=/tmp/easy-proxies-go-build go test ./internal/nodesource`.

### Task 2: Reorder source loading and dedupe using shared composition

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/free_proxy_sources_test.go`
- Inspect/modify: `internal/subscription/manager.go`, `internal/boxmgr/manager.go`

- [ ] Add `FreeProxyMaxNodes int yaml:"free_proxy_max_nodes"` to `Config`.
- [ ] Normalize default: if free sources exist and global max is unset, use safe default `500`; `-1` may mean unlimited only for explicit expert use.
- [ ] Extract shared function/method for runtime source composition used by startup and reload flows.
- [ ] Load order: inline -> file only when no subscriptions -> subscriptions/cache -> free proxies.
- [ ] Before appending free proxies, build a canonical `seenURI` map from all existing nodes.
- [ ] Pass remaining global capacity to each provider as an effective max.
- [ ] Append free proxies only if not seen and under global cap; set `Source=NodeSourceFreeProxy`.
- [ ] Ensure reload/subscription refresh removes stale runtime free nodes and re-appends at most once.
- [ ] Do not write free proxies into `nodes.txt`; keep existing `SaveNodes` skip.
- [ ] Add tests for priority/load order, global cap, per-source cap, dedupe against existing nodes, and no double-append on repeated normalize/reload helper calls.
- [ ] Run: `GOCACHE=/tmp/easy-proxies-go-build go test ./internal/config ./internal/nodesource`.

---

## Chunk 2: API Pagination, Summary, and Source Metadata

### Task 3: Carry node source into monitor snapshots

**Files:**
- Modify: `internal/outbound/pool/pool.go`
- Modify: `internal/builder/builder.go`
- Modify: `internal/monitor/manager.go`

- [ ] Add `Source string json:"source,omitempty"` to `pool.MemberMeta` and `monitor.NodeInfo`.
- [ ] Set metadata source from `config.NodeConfig.Source` in builder for every member.
- [ ] Ensure initial global pool, lazy pool, per-node pool, and region pool registration all include source.
- [ ] Add/adjust tests to assert source survives duplicate registration in pool/hybrid/GeoIP-related paths.
- [ ] Run targeted tests/build for packages.

### Task 4: Add server-side filtering, pagination, and summary to `/api/nodes`

**Files:**
- Modify: `internal/monitor/server.go`
- Add/modify tests in `internal/monitor`.

- [ ] Define paged mode strictly: trigger only for `page`, `page_size`, `region`, `availability`, `latency`, `source`, `q`, `sort`, or `summary_only`.
- [ ] Plain `/api/nodes` and unknown-only query params preserve legacy object shape and `SnapshotFiltered(true)` semantics.
- [ ] `summary_only=true` returns stats and `nodes: []` or omits node payload while authenticating successfully.
- [ ] Paged/filter mode starts from all `Snapshot()` nodes, applies filters, stable sort, then pagination.
- [ ] Cap `page_size` at 500; normalize invalid/negative page/page_size.
- [ ] Stats contract: `total_nodes`, `region_stats`, `region_healthy`, and `source_stats` computed over all snapshots; `total_filtered` computed after filters before pagination.
- [ ] Add tests for legacy plain response, unknown query compatibility, page metadata, invalid pagination, filters (`region`, `availability`, `latency`, `source`, `q`), stable sort, and source stats.
- [ ] Run: `GOCACHE=/tmp/easy-proxies-go-build go test ./internal/monitor`.

---

## Chunk 3: WebUI Overview Pagination and Always-Mounted Fetch Fixes

### Task 5: Add summary and paged node API client

**Files:**
- Modify: `web/src/types/node.ts`
- Modify: `web/src/api/nodes.ts`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/layout/Topbar.tsx`

- [ ] Define `NodesPage`, `NodesQuery`, `NodesSummary` types.
- [ ] Implement `getNodesPage(params)` building query string and omitting `all`/empty values.
- [ ] Implement `getNodesSummary()` using `summary_only=true` or lightweight equivalent.
- [ ] Preserve `getNodes()` for legacy callers.
- [ ] Move auth probe and topbar telemetry to `getNodesSummary()` so they do not fetch all nodes.
- [ ] Run: `cd web && npm run typecheck`.

### Task 6: Convert overview to server-side pagination/filtering

**Files:**
- Modify: `web/src/pages/NodeOverviewPage.tsx`
- Optional CSS adjustments in `web/src/styles/globals.css` if needed.

- [ ] Keep region/status/latency/sort filters but pass them to API.
- [ ] Add source filter (`全部`, `subscription`, `free_proxy`, `nodes_file`, `inline`).
- [ ] Add page/pageSize state; reset page to 1 when filters change.
- [ ] Render only current page rows.
- [ ] Display total filtered count and total node count.
- [ ] Build region/source filter labels from server stats metadata, not current page rows.
- [ ] Add Prev/Next and page size selector.
- [ ] Run: `cd web && npm run typecheck && npm run build`.

---

## Chunk 4: Isolated Runtime Verification

### Task 7: Isolated config and runtime check

**Files:**
- Create under `/tmp/easy-proxies-scale-test/` only; do not touch main service files.

- [ ] Build isolated binary in worktree: `GOCACHE=/tmp/easy-proxies-go-build go build -tags "with_utls with_quic with_grpc with_wireguard with_gvisor with_clash_api" -o /tmp/easy-proxies-scale-test/easy_proxies_scale ./cmd/easy_proxies`.
- [ ] Create isolated config with distinct ports: management `127.0.0.1:19091`, pool `12323`, geoip `11221`, Android disabled or high ports, log and nodes file under `/tmp/easy-proxies-scale-test/`.
- [ ] Avoid public network dependency: use synthetic local file sources and, if subscription behavior is needed, a local test HTTP server or cached `nodes_file` where feasible.
- [ ] Ensure Clash API controller is not colliding with live `9092`; configure high port or disable if implemented.
- [ ] Start isolated binary in background; leave current service on `9091` untouched.
- [ ] Query `http://127.0.0.1:19091/api/nodes?page=1&page_size=10&source=free_proxy`.
- [ ] Query `summary_only=true` and plain `/api/nodes` compatibility.
- [ ] Verify `source_stats.free_proxy` respects cap, response returns only page size rows, and current live `9091` remains reachable with its original node count.
- [ ] Stop isolated process and record evidence.

---

## Review Notes

Plan reviewed by architecture and API/frontend agents once and revised to include:
- shared runtime composition for startup/reload/refresh
- bounded reads/parses before activation caps
- strict legacy `/api/nodes` compatibility
- summary-only mode for auth/topbar
- stats semantics for paged responses
- source propagation duplicate-registration tests
- isolated Clash API/runtime verification
