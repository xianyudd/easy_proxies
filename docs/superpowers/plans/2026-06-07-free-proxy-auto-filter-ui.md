# Free Proxy Auto Filter and UI Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to add free proxy sources from the UI and have Easy Proxies automatically fetch, dedupe, validate, tier, and only expose usable free proxies in the runtime node list.

**Architecture:** Treat `free_proxy_sources` as candidate feeds, not trusted runtime nodes. A dedicated bounded prefilter validates candidates during config load/reload, emits source-level stats, and only appends nodes meeting `free_proxy_filter.min_tier`; the existing `/api/nodes` source and availability filters then display only accepted runtime nodes. Settings API/UI persist source and filter configuration, while heavier quality jobs remain separate for Cloudflare/reputation scoring.

**Tech Stack:** Go `net/http.Transport.Proxy`, bounded worker queues, existing `internal/nodesource`, `internal/config`, `internal/monitor` settings API, React/Ant Design settings page, Go unit tests, `pnpm build`, `go test`.

---

## Reference Projects and Design Decisions

### References observed

1. `jhao104/proxy_pool` (`https://github.com/jhao104/proxy_pool`)
   - Stores proxy metadata (`fail_count`, `check_count`, `last_status`, `last_time`, `https`, `source`).
   - Separates fetchers from validators and only serves entries from a validated pool.
   - Useful decision: free proxies should first be candidate records, then verified pool members.

2. ProxyBroker / ProxyBroker2 style libraries
   - Typical pipeline: grab/find/serve.
   - Verifies protocol, anonymity, country, response time, and rejects bad candidates before serving.
   - Useful decision: maintain a cheap prefilter plus optional deeper classification; do not block reload on expensive reputation checks.

3. Go proxy checker snippets from GitHub code search
   - Common approach: `http.Transport{Proxy: http.ProxyURL(proxyURL)}` plus bounded concurrency and timeout.
   - Useful decision: implement prefilter in Go, not Python/curl subprocesses.

### Decisions for this project

- `free_proxy_sources` remain configuration of candidate feeds.
- Runtime nodes only receive candidates that pass `free_proxy_filter`.
- Reload/startup prefilter is intentionally lightweight:
  - `reject`: HTTP probe fails.
  - `http_basic`: HTTP 204 probe passes.
  - `simple_web`: HTTP 204 + HTTPS example probe pass.
- `recommended` / `premium` stay in background quality jobs because reputation/Cloudflare checks are too heavy for reload.
- The default should be safe and opt-in: existing behavior is preserved unless `free_proxy_filter.enabled=true`.
- UI should default new sources to `enabled=true`, `format=text`, `default_scheme=http`, and automatic filtering enabled.

---

## File Structure

### New files

- `internal/nodesource/filter.go`
  - Owns free proxy prefilter types and logic.
  - Exposes `FilterConfig`, `FilterProbes`, `FilterResult`, `FilterSummary`, and `FilterNodes`.
  - Uses Go `http.Transport.Proxy`, no subprocesses.

- `internal/nodesource/filter_test.go`
  - Tests tier classification, ordering, bounded acceptance, and relative probe URL support with `httptest.Server`.

### Modified files

- `internal/config/config.go`
  - Replace inline filtering code with `nodesource.FilterConfig` usage.
  - Add `FreeProxyFilter nodesource.FilterConfig` to `Config`.
  - Track latest filter summaries for settings/API visibility if needed.
  - Persist `FreeProxySources`, `FreeProxyMaxNodes`, and `FreeProxyFilter` in `SaveSettings`.

- `internal/config/free_proxy_sources_test.go`
  - Keep existing source/cap/dedupe tests.
  - Add tests for opt-in filtering and min-tier behavior.

- `internal/monitor/server.go`
  - Settings GET includes `free_proxy_sources`, `free_proxy_max_nodes`, and `free_proxy_filter`.
  - Settings PUT accepts and persists those fields.
  - Validate source entries enough to avoid silently saving empty rows.

- `web/src/types/settings.ts`
  - Add typed `FreeProxySource`, `FreeProxyFilter`, and typed fields to `SettingsResponse`.

- `web/src/pages/SettingsPage.tsx`
  - Add “免费代理源” settings section.
  - Add side-nav anchor.
  - Add source list CRUD controls.
  - Add filter controls: enabled, min tier, max nodes, max candidates, workers, timeout.

- `README.md`, `README_ZH.md`, `config.example.yaml`
  - Document UI-managed free proxy sources and auto-filter behavior.

---

## Chunk 1: Extract Go prefilter into nodesource package

### Task 1: Add prefilter types and tests

**Files:**
- Create: `internal/nodesource/filter.go`
- Create: `internal/nodesource/filter_test.go`
- Modify: none

- [ ] **Step 1: Write failing tests for HTTP basic filtering**

Create `internal/nodesource/filter_test.go`:

```go
package nodesource

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestFilterNodesKeepsHTTPBasicCandidates(t *testing.T) {
    good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/generate_204" {
            t.Fatalf("unexpected path: %s", r.URL.Path)
        }
        w.WriteHeader(http.StatusNoContent)
    }))
    defer good.Close()

    bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusServiceUnavailable)
    }))
    defer bad.Close()

    cfg := FilterConfig{Enabled: true, MinTier: "http_basic", Workers: 4, Timeout: DurationForTest(2), Probes: FilterProbes{HTTP: "/generate_204"}}
    result := FilterNodes([]Node{{URI: good.URL}, {URI: bad.URL}}, cfg)

    if len(result.Accepted) != 1 || result.Accepted[0].URI != good.URL {
        t.Fatalf("unexpected accepted nodes: %#v", result.Accepted)
    }
    if result.Summary.Total != 2 || result.Summary.Accepted != 1 || result.Summary.Rejected != 1 {
        t.Fatalf("unexpected summary: %#v", result.Summary)
    }
}
```

Note: use a helper or normal `2*time.Second`; exact syntax can be adjusted during implementation.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
GOCACHE=/tmp/easy-proxies-go-build go test ./internal/nodesource -run TestFilterNodesKeepsHTTPBasicCandidates -count=1
```

Expected: FAIL because filter types/functions do not exist.

- [ ] **Step 3: Implement `internal/nodesource/filter.go`**

Implement:

```go
type FilterConfig struct {
    Enabled       bool          `yaml:"enabled" json:"enabled"`
    MinTier       string        `yaml:"min_tier" json:"min_tier"`
    Workers       int           `yaml:"workers" json:"workers"`
    Timeout       time.Duration `yaml:"timeout" json:"timeout"`
    MaxCandidates int           `yaml:"max_candidates" json:"max_candidates"`
    Probes        FilterProbes  `yaml:"probes" json:"probes"`
}

type FilterProbes struct {
    HTTP  string `yaml:"http" json:"http"`
    HTTPS string `yaml:"https" json:"https"`
}

type FilterResult struct {
    Accepted []Node
    Summary  FilterSummary
}

type FilterSummary struct {
    Total      int            `json:"total"`
    Accepted   int            `json:"accepted"`
    Rejected   int            `json:"rejected"`
    TierCounts map[string]int `json:"tier_counts"`
}
```

Rules:
- `Normalized()` sets default min tier `http_basic`, workers `80`, max workers `800`, timeout `2s`, HTTP probe `http://cp.cloudflare.com/generate_204`, HTTPS probe `https://example.com/`.
- `FilterNodes(nodes, cfg)` returns original nodes when `cfg.Enabled=false`.
- Worker queue must be bounded and preserve original accepted order.
- `probe()` creates a short-lived transport and calls `CloseIdleConnections`.
- Relative probe paths like `/generate_204` resolve against the proxy URI host. This makes tests cheap and deterministic.

- [ ] **Step 4: Run nodesource tests**

Run:

```bash
GOCACHE=/tmp/easy-proxies-go-build go test ./internal/nodesource -count=1
```

Expected: PASS.

### Task 2: Add simple_web tier test

**Files:**
- Modify: `internal/nodesource/filter_test.go`

- [ ] **Step 1: Write failing test for `min_tier=simple_web`**

Add a test where:
- HTTP probe returns 204.
- HTTPS probe path returns 200 with `Example Domain`.
- One candidate fails HTTPS and is excluded for `min_tier=simple_web`.

- [ ] **Step 2: Run test to verify failure or coverage**

Run:

```bash
GOCACHE=/tmp/easy-proxies-go-build go test ./internal/nodesource -run SimpleWeb -count=1
```

- [ ] **Step 3: Implement/fix tier logic**

Ensure tier ranks:

```text
reject=0
http_basic=1
simple_web=2
recommended/general_web=3
```

- [ ] **Step 4: Run tests**

Run:

```bash
GOCACHE=/tmp/easy-proxies-go-build go test ./internal/nodesource -count=1
```

Expected: PASS.

---

## Chunk 2: Wire prefilter into config loading

### Task 3: Replace inline config filter with nodesource.FilterConfig

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/free_proxy_sources_test.go`

- [ ] **Step 1: Update config struct**

Replace any local `FreeProxyFilterConfig` with:

```go
FreeProxyFilter nodesource.FilterConfig `yaml:"free_proxy_filter"`
```

- [ ] **Step 2: Update normalize**

Call:

```go
c.FreeProxyFilter = c.FreeProxyFilter.Normalized()
```

- [ ] **Step 3: Update appendFreeProxyNodes**

Pseudo-flow:

```go
filter := c.FreeProxyFilter.Normalized()
sourceNodes, err := provider.LoadLimited(filter.LoadLimit(remaining))
if filter.Enabled {
    result := nodesource.FilterNodes(sourceNodes, filter)
    log.Printf("prefilter kept %d/%d", result.Summary.Accepted, result.Summary.Total)
    sourceNodes = result.Accepted
}
```

- [ ] **Step 4: Keep existing tests passing**

Run:

```bash
GOCACHE=/tmp/easy-proxies-go-build go test ./internal/config -count=1
```

Expected: PASS.

### Task 4: Ensure SaveSettings persists free proxy settings

**Files:**
- Modify: `internal/config/config.go`
- Test: add or extend config save test if existing test pattern is available.

- [ ] **Step 1: Write failing test**

Create/extend a test that loads a config with free proxy settings, mutates `FreeProxyFilter.Enabled`, calls `SaveSettings`, reloads, and asserts fields are preserved.

- [ ] **Step 2: Implement SaveSettings persistence**

Add:

```go
saveCfg.FreeProxySources = c.FreeProxySources
saveCfg.FreeProxyMaxNodes = c.FreeProxyMaxNodes
saveCfg.FreeProxyFilter = c.FreeProxyFilter
```

- [ ] **Step 3: Run tests**

Run:

```bash
GOCACHE=/tmp/easy-proxies-go-build go test ./internal/config -count=1
```

---

## Chunk 3: Settings API contract

### Task 5: Expose free proxy fields in `/api/settings`

**Files:**
- Modify: `internal/monitor/server.go`
- Test: `internal/monitor/server_*_test.go` or new `server_settings_free_proxy_test.go`

- [ ] **Step 1: Write GET test**

Assert `GET /api/settings` includes:

```json
{
  "free_proxy_sources": [...],
  "free_proxy_max_nodes": 500,
  "free_proxy_filter": {
    "enabled": true,
    "min_tier": "simple_web",
    "workers": 200,
    "timeout": "2s"
  }
}
```

- [ ] **Step 2: Implement GET fields**

Add response fields from `cfg.FreeProxySources`, `cfg.FreeProxyMaxNodes`, and `cfg.FreeProxyFilter`.

- [ ] **Step 3: Write PUT test**

POST/PUT a payload with one source and filter settings. Assert config object and saved YAML contain updated values.

- [ ] **Step 4: Implement PUT parsing**

Use request DTOs to parse duration strings for source timeout and filter timeout. Ignore fully empty rows.

- [ ] **Step 5: Run monitor tests**

Run:

```bash
GOCACHE=/tmp/easy-proxies-go-build go test ./internal/monitor -run 'Settings|FreeProxy' -count=1
```

Expected: PASS.

---

## Chunk 4: Settings UI for source management

### Task 6: Type settings fields

**Files:**
- Modify: `web/src/types/settings.ts`

- [ ] **Step 1: Add interfaces**

Add:

```ts
export interface FreeProxySource {
  name?: string
  url?: string
  file?: string
  format?: string
  default_scheme?: string
  enabled?: boolean
  timeout?: string
  max_nodes?: number
  max_bytes?: number
}

export interface FreeProxyFilter {
  enabled?: boolean
  min_tier?: string
  workers?: number
  timeout?: string
  max_candidates?: number
  probes?: { http?: string; https?: string }
}
```

- [ ] **Step 2: Extend `SettingsResponse`**

Add typed fields:

```ts
free_proxy_sources?: FreeProxySource[]
free_proxy_max_nodes?: number
free_proxy_filter?: FreeProxyFilter
```

### Task 7: Add SettingsPage free proxy section

**Files:**
- Modify: `web/src/pages/SettingsPage.tsx`

- [ ] **Step 1: Add state helpers**

Derive:

```ts
const freeSources = Array.isArray(draft.free_proxy_sources) ? draft.free_proxy_sources : []
const freeFilter = (draft.free_proxy_filter || {}) as FreeProxyFilter
```

Add helper functions:

```ts
const updateFreeSource = (idx, patch) => ...
const addFreeSource = () => ...
const removeFreeSource = idx => ...
```

- [ ] **Step 2: Add side nav item**

Add:

```tsx
<a href="#free-proxy"><span className="nav-kicker">02</span><strong>免费代理源</strong><span>自动筛选与入池</span></a>
```

Adjust numbering if desired.

- [ ] **Step 3: Add section UI**

Add a card after subscriptions:

- Summary cards:
  - source count
  - enabled source count
  - max runtime nodes
  - min tier
- Filter controls:
  - checkbox enabled
  - min tier select (`http_basic`, `simple_web`)
  - max nodes
  - max candidates
  - workers
  - timeout
- Source rows:
  - enabled checkbox
  - name
  - url
  - file
  - default_scheme select
  - max_nodes
  - remove button
- Add source button.

- [ ] **Step 4: Save uses existing Save Settings button**

Do not create a separate endpoint. Existing `saveSettings(draft)` should persist these fields.

- [ ] **Step 5: Build frontend**

Run:

```bash
pnpm -C web build
```

Expected: PASS.

---

## Chunk 5: Docs and examples

### Task 8: Update config example

**Files:**
- Modify: `config.example.yaml`

- [ ] **Step 1: Add example block**

Add:

```yaml
# free_proxy_max_nodes: 0
# free_proxy_filter:
#   enabled: true
#   min_tier: simple_web
#   workers: 200
#   timeout: 2s
#   max_candidates: 0
#   probes:
#     http: http://cp.cloudflare.com/generate_204
#     https: https://example.com/
# free_proxy_sources:
#   - name: thespeedx-http
#     enabled: true
#     url: https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/http.txt
#     format: text
#     default_scheme: http
#     max_nodes: 0
```

### Task 9: Update README docs

**Files:**
- Modify: `README.md`
- Modify: `README_ZH.md`

- [ ] **Step 1: Document auto-filter semantics**

Explain:
- free sources are candidate feeds.
- filter enabled means only accepted candidates are runtime nodes.
- `min_tier=simple_web` is recommended.
- quality jobs can further rank accepted nodes.

- [ ] **Step 2: Mention UI**

Explain users can configure sources from Settings → 免费代理源.

---

## Chunk 6: Verification

### Task 10: Full test pass

- [ ] **Step 1: Run Go focused tests**

```bash
GOCACHE=/tmp/easy-proxies-go-build go test ./internal/nodesource ./internal/config ./internal/monitor -count=1
```

Expected: PASS.

- [ ] **Step 2: Run all Go tests**

```bash
GOCACHE=/tmp/easy-proxies-go-build go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 3: Run frontend build**

```bash
pnpm -C web build
```

Expected: PASS.

- [ ] **Step 4: Static diff check**

```bash
git diff --check
```

Expected: no output.

### Task 11: Manual runtime check

- [ ] **Step 1: Configure a small known source**

Use a small local file or one public source with `free_proxy_filter.enabled=true`, `max_candidates=100`, `free_proxy_max_nodes=20`.

- [ ] **Step 2: Reload**

```bash
curl -X POST http://127.0.0.1:9091/api/reload
```

- [ ] **Step 3: Verify API**

```bash
curl -s 'http://127.0.0.1:9091/api/nodes?summary_only=true' | jq '.source_stats'
curl -s 'http://127.0.0.1:9091/api/nodes?source=free_proxy&availability=available&page_size=20' | jq '.total_filtered'
```

- [ ] **Step 4: Verify UI**

Open Settings → 免费代理源:
- Add source.
- Enable automatic filter.
- Save settings.
- Reload.
- Go to Node Overview and filter Source = 免费源.

---

## Commit Plan

1. `feat: add free proxy prefilter`
   - `internal/nodesource/filter.go`
   - `internal/nodesource/filter_test.go`
   - config wiring tests

2. `feat: expose free proxy settings api`
   - settings GET/PUT
   - monitor tests

3. `feat: manage free proxy sources in settings ui`
   - TS types
   - settings UI
   - frontend build

4. `docs: document free proxy auto filtering`
   - README/config example docs


---

## Taste-Skill UI Refinement Addendum

Installed skill: `~/.codex/skills/taste-skill`.

### Design Read

Reading this as: 代理管理后台的设置页增强，为技术用户/本地运维使用，偏“可信、紧凑、工程化”的产品 UI，保留现有 Easy Proxies 控制台语言，避免营销页式视觉。

### UI Dials

```text
DESIGN_VARIANCE: 4
MOTION_INTENSITY: 2
VISUAL_DENSITY: 7
```

### Anti-Slop decisions applied

- No AI-purple gradients or decorative glow.
- No duplicate save CTA inside the section; the page-level `保存设置` remains the only save intent.
- Inputs use labels above controls; placeholders are examples only.
- Advanced probe settings are hidden behind `<details>`.
- Source rows use explicit responsive CSS classes, not inline grid hacks.
- Empty state explains the full workflow and includes one `新增源` action.
- Warning state exists for source rows missing URL/file.

### Refined section layout

```text
免费代理源
  ├─ summary strip: 源数量 / 启用源 / 最大入池 / 最低等级
  ├─ auto filter strategy panel
  │   ├─ 启用自动筛选
  │   ├─ 最低等级
  │   ├─ 最大入池数
  │   ├─ 最大候选数
  │   ├─ 筛选并发
  │   └─ 筛选超时
  ├─ 高级探针配置 collapsed details
  ├─ inline warning if any source lacks URL/file
  └─ editable source rows
```

### Additional acceptance checks

- [ ] No inline `style={{gridTemplateColumns: ...}}` remains in free proxy source rows.
- [ ] HTTP/HTTPS probes are not always visible; they live under `高级探针配置`.
- [ ] Empty state contains workflow explanation and exactly one add action.
- [ ] Free proxy section has no second save button.
- [ ] `pnpm -C web build` passes after UI refinement.

