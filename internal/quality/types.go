package quality

import "time"

// CheckKind identifies which quality checks a job should run.
type CheckKind string

const (
	CheckQuick      CheckKind = "quick"
	CheckCloudflare CheckKind = "cloudflare"
	CheckReputation CheckKind = "reputation"
	CheckCombined   CheckKind = "combined"
	CheckPipeline   CheckKind = "pipeline"
)

// JobStatus describes the lifecycle state of a quality job.
type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

// Target is a proxy/node selected for quality checks.
type Target struct {
	Index    int    `json:"target_index"`
	ID       string `json:"target_id,omitempty"`
	NodeName string `json:"node_name,omitempty"`
	NodeTag  string `json:"node_tag,omitempty"`
	Source   string `json:"source,omitempty"`
	ProxyURL string `json:"proxy_url,omitempty"`
	// UpstreamURL is the original proxy URI. Quality checks prefer this when
	// the URI is directly usable by Go's HTTP proxy transport, avoiding a large
	// dependency on per-node local multi-port listeners during full scans.
	UpstreamURL string `json:"upstream_url,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	Host        string `json:"host,omitempty"`
	Port        int    `json:"port,omitempty"`
	Region      string `json:"region,omitempty"`
	Retry       bool   `json:"retry,omitempty"`
}

// TargetQuery filters target selection before a job is created.
type TargetQuery struct {
	Kind               CheckKind `json:"kind,omitempty"`
	NodeTag            string    `json:"node_tag,omitempty"`
	Protocol           string    `json:"protocol,omitempty"`
	Region             string    `json:"region,omitempty"`
	Mode               string    `json:"mode,omitempty"`
	IncludeUnavailable bool      `json:"include_unavailable,omitempty"`
	RetryFailed        bool      `json:"retry_failed,omitempty"`
	ForceRefresh       bool      `json:"force_refresh,omitempty"`
	IDs                []string  `json:"ids,omitempty"`
}

// JobRequest is the API/store request to create a quality job.
type JobRequest struct {
	Kind               CheckKind   `json:"kind"`
	Region             string      `json:"region,omitempty"`
	Mode               string      `json:"mode,omitempty"`
	Count              int         `json:"count,omitempty"`
	IncludeUnavailable bool        `json:"include_unavailable,omitempty"`
	RetryFailed        bool        `json:"retry_failed,omitempty"`
	ForceRefresh       bool        `json:"force_refresh,omitempty"`
	Replace            bool        `json:"replace,omitempty"`
	Query              TargetQuery `json:"query,omitempty"`
	Targets            []Target    `json:"targets,omitempty"`
}

// JobSnapshot is an immutable view of job state for polling/API responses.
type JobSnapshot struct {
	ID         string      `json:"id"`
	Kind       CheckKind   `json:"kind"`
	Status     JobStatus   `json:"status"`
	Region     string      `json:"region,omitempty"`
	Query      TargetQuery `json:"query,omitempty"`
	Total      int         `json:"total"`
	Queued     int         `json:"queued"`
	Running    int         `json:"running"`
	Completed  int         `json:"completed"`
	Cached     int         `json:"cached"`
	Failed     int         `json:"failed"`
	Cancelled  int         `json:"cancelled"`
	Percent    float64     `json:"percent"`
	Summary    JobSummary  `json:"summary,omitempty"`
	Message    string      `json:"message,omitempty"`
	Error      string      `json:"error,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
	StartedAt  time.Time   `json:"started_at,omitempty"`
	FinishedAt time.Time   `json:"finished_at,omitempty"`
}

// JobSummary aggregates quality outcomes for full-job UI cards/charts.
type JobSummary struct {
	Cloudflare map[string]int `json:"cloudflare,omitempty"`
	Reputation map[string]int `json:"reputation,omitempty"`
	Quick      map[string]int `json:"quick,omitempty"`
	Final      map[string]int `json:"final,omitempty"`
	Tier       map[string]int `json:"tier,omitempty"`
	Pool       map[string]int `json:"pool,omitempty"`
}

// Result is one target's quality-check output.
type Result struct {
	JobID       string         `json:"job_id"`
	Kind        CheckKind      `json:"kind"`
	Target      Target         `json:"-"`
	TargetIndex int            `json:"target_index"`
	TargetID    string         `json:"target_id,omitempty"`
	NodeName    string         `json:"node_name,omitempty"`
	NodeTag     string         `json:"node_tag,omitempty"`
	Source      string         `json:"source,omitempty"`
	ProxyURL    string         `json:"proxy_url,omitempty"`
	Protocol    string         `json:"protocol,omitempty"`
	Host        string         `json:"host,omitempty"`
	Port        int            `json:"port,omitempty"`
	Region      string         `json:"region,omitempty"`
	Status      string         `json:"status,omitempty"`
	Success     bool           `json:"success"`
	Score       int            `json:"score,omitempty"`
	FinalScore  int            `json:"final_score,omitempty"`
	Tier        string         `json:"tier,omitempty"`
	TierScore   int            `json:"tier_score,omitempty"`
	Pool        string         `json:"pool,omitempty"`
	Capabilities []string      `json:"capabilities,omitempty"`
	TierReasons []string       `json:"tier_reasons,omitempty"`
	Recommend   bool           `json:"recommend,omitempty"`
	LatencyMS   int64          `json:"latency_ms,omitempty"`
	Quick       map[string]any `json:"quick,omitempty"`
	CF          map[string]any `json:"cf,omitempty"`
	Reputation  map[string]any `json:"reputation,omitempty"`
	Error       string         `json:"error,omitempty"`
	CheckedAt   time.Time      `json:"checked_at,omitempty"`
}

// ResultQuery controls paginated result listing.
type ResultQuery struct {
	Page     int `json:"page,omitempty"`
	PageSize int `json:"page_size,omitempty"`
}

// PagedResults is a stable, paginated result response.
type PagedResults struct {
	Data       []Result `json:"data"`
	Count      int      `json:"count"`
	Page       int      `json:"page"`
	PageSize   int      `json:"page_size"`
	TotalPages int      `json:"total_pages"`
	HasNext    bool     `json:"has_next"`
}
