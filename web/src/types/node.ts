export interface NodeSnapshot {
  tag?: string
  name?: string
  region?: string
  country?: string
  port?: number
  available?: boolean
  initial_check_done?: boolean
  blacklisted?: boolean
  last_latency_ms?: number
  failure_count?: number
  active_connections?: number
  [key: string]: unknown
}
