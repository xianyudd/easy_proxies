import { api } from './client'
import type { NodeSnapshot, NodesPage, NodesQuery, NodesSummary } from '../types/node'

interface NodesResponse {
  nodes?: NodeSnapshot[]
  total_nodes?: number
  region_stats?: Record<string, number>
  region_healthy?: Record<string, number>
  source_stats?: Record<string, number>
}

export async function getNodes() {
  const data = await api.get<NodesResponse | NodeSnapshot[]>('/api/nodes')
  return Array.isArray(data) ? data : (data.nodes || [])
}

function normalizeNodesPage(data: Partial<NodesPage>): NodesPage {
  return {
    nodes: data.nodes || [],
    total_nodes: data.total_nodes || 0,
    total_filtered: data.total_filtered ?? data.total_nodes ?? 0,
    page: data.page || 1,
    page_size: data.page_size || 100,
    has_next: !!data.has_next,
    region_stats: data.region_stats || {},
    region_healthy: data.region_healthy || {},
    source_stats: data.source_stats || {},
  }
}

export async function getNodesPage(params: NodesQuery = {}) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '' || value === 'all') return
    search.set(key, String(value))
  })
  if (!search.has('page')) search.set('page', String(params.page || 1))
  if (!search.has('page_size')) search.set('page_size', String(params.page_size || 100))
  const data = await api.get<NodesPage>(`/api/nodes?${search.toString()}`)
  return normalizeNodesPage(data)
}

export async function getNodesSummary() {
  const data = await api.get<NodesSummary>('/api/nodes?summary_only=true')
  return normalizeNodesPage(data)
}
export function probeAllNodes() { return api.post('/api/nodes/probe-all') }
export function probeNode(tag: string) { return api.post<{latency_ms?: number; error?: string}>(`/api/nodes/${encodeURIComponent(tag)}/probe`) }
export function blacklistNode(tag: string) { return api.post(`/api/nodes/${encodeURIComponent(tag)}/blacklist`, { duration: '24h' }) }
export function releaseNode(tag: string) { return api.post(`/api/nodes/${encodeURIComponent(tag)}/release`) }
