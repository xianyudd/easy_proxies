import { api } from './client'
import type { NodeSnapshot } from '../types/node'

interface NodesResponse {
  nodes?: NodeSnapshot[]
  total_nodes?: number
  region_stats?: Record<string, number>
  region_healthy?: Record<string, number>
}

export async function getNodes() {
  const data = await api.get<NodesResponse | NodeSnapshot[]>('/api/nodes')
  return Array.isArray(data) ? data : (data.nodes || [])
}
export function probeAllNodes() { return fetch('/api/nodes/probe-all', { method: 'POST', credentials: 'same-origin' }) }
export function probeNode(tag: string) { return api.post<{latency_ms?: number; error?: string}>(`/api/nodes/${encodeURIComponent(tag)}/probe`) }
export function blacklistNode(tag: string) { return api.post(`/api/nodes/${encodeURIComponent(tag)}/blacklist`, { duration: '24h' }) }
export function releaseNode(tag: string) { return api.post(`/api/nodes/${encodeURIComponent(tag)}/release`) }
