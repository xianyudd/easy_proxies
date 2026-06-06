export type QualityJobKind = 'quick' | 'cloudflare' | 'reputation' | 'combined' | 'pipeline'
export type QualityJobStatus = 'queued' | 'running' | 'completed' | 'failed' | 'cancelled'

export interface QualityJobRequest {
  kind: QualityJobKind
  region?: string
  mode?: string
  count?: number
  include_unavailable?: boolean
  retry_failed?: boolean
  force_refresh?: boolean
  replace?: boolean
}

export interface QualityJobCreated {
  job_id: string
  status: QualityJobStatus
  kind: QualityJobKind
  progress_url: string
  results_url: string
}

export interface QualityJobSummary {
  quick?: Record<string, number>
  cloudflare?: Record<string, number>
  reputation?: Record<string, number>
  final?: Record<string, number>
  tier?: Record<string, number>
  pool?: Record<string, number>
}

export interface QualityJobSnapshot {
  id: string
  kind: QualityJobKind
  status: QualityJobStatus
  region?: string
  total: number
  queued: number
  running: number
  completed: number
  cached: number
  failed: number
  cancelled: number
  percent: number
  summary?: QualityJobSummary
  message?: string
  error?: string
  created_at?: string
  updated_at?: string
  started_at?: string
  finished_at?: string
}

export interface QualityJobResult {
  job_id: string
  kind: QualityJobKind
  target_index: number
  target_id?: string
  node_name?: string
  node_tag?: string
  source?: string
  proxy_url?: string
  protocol?: string
  host?: string
  port?: number
  region?: string
  status?: 'pending' | 'completed' | 'failed' | string
  success?: boolean
  score?: number
  final_score?: number
  tier?: string
  tier_score?: number
  pool?: string
  capabilities?: string[]
  tier_reasons?: string[]
  recommend?: boolean
  latency_ms?: number
  quick?: Record<string, unknown>
  cf?: Record<string, unknown>
  reputation?: Record<string, unknown>
  error?: string
  checked_at?: string
}

export interface PagedQualityResults {
  data: QualityJobResult[]
  count: number
  page: number
  page_size: number
  total_pages: number
  has_next: boolean
}
