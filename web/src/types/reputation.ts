export interface ReputationResult {
  node_name?: string
  node_tag?: string
  region?: string
  port?: number
  exit_ip?: string
  country?: string
  asn?: string | number
  isp?: string
  risk_level?: 'low'|'medium'|'high'|'failed'|string
  risk_score?: number
  cached?: boolean
  duration_ms?: number
  error?: string
  result?: ReputationResult
  [key: string]: unknown
}
export interface ReputationResponse {
  data?: ReputationResult[]
  summary?: Record<string, number>
  count?: number
  checked_count?: number
  error?: string
}
