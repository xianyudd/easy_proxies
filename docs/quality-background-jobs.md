# Quality Background Jobs

`easy_proxies` uses background jobs for large Cloudflare compatibility and IP reputation checks. This keeps the WebUI and management API responsive when the node list grows to thousands of proxies.

## Why

Synchronous all-node checks work for small lists, but 2000-6000+ nodes can block request handlers, overload the browser with one huge result payload, and make cancellation difficult. Background jobs split the workflow into:

1. create a job quickly;
2. run checks with a bounded worker pool;
3. poll progress;
4. page through stable result rows;
5. cancel or retry when needed.

## WebUI flow

Open `节点质量 / Node Quality`:

- `一键后台扫描全部节点` creates a combined Cloudflare + reputation job.
- `重试失败节点` creates a replacement job for failed targets.
- `取消任务` cancels an active job.
- The result table uses server-side pagination while a job is selected.
- `抽样检测 CF` remains a small synchronous check for quick spot testing.

The progress card shows job status, completed/total counts, and percent complete. The table can be paged without loading every result into the browser at once.

## API

### Create a job

```bash
curl --noproxy '*' -sS -X POST 'http://127.0.0.1:9091/api/quality/jobs' \
  -H 'content-type: application/json' \
  -d '{
    "kind": "combined",
    "region": "all",
    "mode": "multi-port",
    "count": 6000,
    "include_unavailable": true,
    "replace": true
  }'
```

Response is `202 Accepted`:

```json
{
  "job_id": "quality-...",
  "status": "queued",
  "kind": "combined",
  "progress_url": "/api/quality/jobs/quality-...",
  "results_url": "/api/quality/jobs/quality-.../results"
}
```

Supported `kind` values:

- `cloudflare`
- `reputation`
- `combined`

Useful request fields:

| Field | Meaning |
|---|---|
| `region` | Region filter, or `all` for all regions. |
| `mode` | Usually `multi-port` for per-node checks. |
| `count` | Maximum targets. API caps this to `10000`. |
| `include_unavailable` | Include inactive/unhealthy nodes in the target set. |
| `retry_failed` | Prefer failed targets when creating the job. |
| `force_refresh` | Bypass eligible cached quality results when supported by the runner. |
| `replace` | Cancel/replace an active job when the service policy allows it. |

### Poll progress

```bash
curl --noproxy '*' -sS 'http://127.0.0.1:9091/api/quality/jobs/<job_id>'
```

Important fields:

| Field | Meaning |
|---|---|
| `status` | `queued`, `running`, `completed`, `failed`, or `cancelled`. |
| `total` | Total targets captured for this job. |
| `completed` / `failed` / `cancelled` | Current result counters. |
| `percent` | Progress percentage. |
| `summary` | Aggregated Cloudflare/reputation level counts. |

### Page results

```bash
curl --noproxy '*' -sS 'http://127.0.0.1:9091/api/quality/jobs/<job_id>/results?page=1&page_size=100'
```

Results are ordered by stable `target_index`, not completion time. Pending targets are represented as pending rows, so pagination does not shift while a job is running.

### Cancel a job

```bash
curl --noproxy '*' -sS -X POST 'http://127.0.0.1:9091/api/quality/jobs/<job_id>/cancel'
```

Cancellation is cooperative. Running checks may finish their current HTTP request, but late result/progress writes are ignored after a terminal job state.

## Legacy async compatibility

Existing check endpoints can create background jobs by passing `background=true` or `async=true`:

```bash
curl --noproxy '*' -sS 'http://127.0.0.1:9091/api/cloudflare/check?region=all&count=6000&background=true'
curl --noproxy '*' -sS 'http://127.0.0.1:9091/api/reputation/check?region=all&count=6000&async=true'
```

## Scale notes

The current design is intended to support 6000+ configured nodes because it avoids the two main bottlenecks:

- the HTTP request that creates the scan returns immediately with a job id;
- result viewing is paginated and stable instead of sending one giant payload.

Actual completion time still depends on checker timeout, network quality, configured worker concurrency, and upstream proxy behavior. For large public free-proxy lists, low availability is expected; quality jobs make this measurable without freezing the service.
