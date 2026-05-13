export interface CloudflareResult {
  node_name?: string
  node_tag?: string
  region?: string
  host?: string
  port?: number
  exit_ip?: string
  cf_loc?: string
  cf_colo?: string
  http_protocol?: string
  tls_version?: string
  warp?: string
  http_204_ok?: boolean
  trace_ok?: boolean
  challenge_status?: string
  score?: number
  level?: 'excellent'|'good'|'fair'|'poor'|'failed'|string
  latency_ms?: number
  cached?: boolean
  error?: string
}
export interface CloudflareCheckResponse {
  data?: CloudflareResult[]
  summary?: Record<string, number>
  checked_count?: number
  region?: string
  include_unavailable?: boolean
  error?: string
}
