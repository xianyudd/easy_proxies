export interface NodeSnapshot {
  tag?: string
  name?: string
  region?: string
  country?: string
  source?: string
  port?: number
  available?: boolean
  initial_check_done?: boolean
  blacklisted?: boolean
  last_latency_ms?: number
  failure_count?: number
  active_connections?: number
  [key: string]: unknown
}

export interface NodesPage {
  nodes: NodeSnapshot[]
  total_nodes: number
  total_filtered: number
  page: number
  page_size: number
  has_next: boolean
  region_stats: Record<string, number>
  region_healthy: Record<string, number>
  source_stats: Record<string, number>
}

export interface NodesSummary extends NodesPage {}

export interface NodesQuery {
  page?: number
  page_size?: number
  region?: string
  availability?: string
  latency?: string
  source?: string
  q?: string
  sort?: string
}
