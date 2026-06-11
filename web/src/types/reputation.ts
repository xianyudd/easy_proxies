export interface ReputationResult {
  node_name?: string
  node_tag?: string
  region?: string
  port?: number
  ip?: string
  exit_ip?: string
  country?: string
  asn?: string | number
  isp?: string
  risk_level?: 'low'|'medium'|'high'|'failed'|string
  risk_score?: number
  cached?: boolean
  checked_at?: string
  duration_ms?: number
  error?: string
  result?: ReputationResult
  [key: string]: unknown
}
export interface ReputationRegionUpdateSummary {
  checked?: number
  updated?: number
  unchanged?: number
  skipped?: number
  persisted?: number
  need_reload?: boolean
  errors?: string[]
}
export interface ReputationResponse {
  data?: ReputationResult[]
  summary?: Record<string, number>
  region_updates?: ReputationRegionUpdateSummary
  count?: number
  checked_count?: number
  error?: string
}
