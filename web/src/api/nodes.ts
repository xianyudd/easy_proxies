import { ApiError, api } from './client'
import type { ConfirmNodeRegionResponse, NodeSnapshot, NodesPage, NodesQuery, NodesSummary } from '../types/node'

interface NodesResponse {
  nodes?: NodeSnapshot[]
  total_nodes?: number
  region_stats?: Record<string, number>
  region_healthy?: Record<string, number>
  source_stats?: Record<string, number>
}

export async function getNodes() {
  const data = await api.get<NodesResponse | NodeSnapshot[]>('/api/nodes')
  return Array.isArray(data) ? safeNodes(data) : safeNodes(data.nodes)
}

function safeNodes(value: unknown): NodeSnapshot[] {
  return Array.isArray(value) ? value : []
}

function safeRecord(value: unknown): Record<string, number> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>).map(([key, raw]) => [key, safeCount(raw)]),
  )
}

function safeCount(input: unknown, fallback = 0) {
  const value = Number(input)
  return Number.isFinite(value) && value >= 0 ? Math.trunc(value) : fallback
}

function normalizeNodesPage(data: Partial<NodesPage> | unknown): NodesPage {
  const source = data && typeof data === 'object' && !Array.isArray(data) ? data as Partial<NodesPage> : {}
  return {
    nodes: safeNodes(source.nodes),
    total_nodes: safeCount(source.total_nodes),
    total_filtered: safeCount(source.total_filtered ?? source.total_nodes),
    page: safeCount(source.page, 1) || 1,
    page_size: safeCount(source.page_size, 100) || 100,
    has_next: source.has_next === true,
    region_stats: safeRecord(source.region_stats),
    region_healthy: safeRecord(source.region_healthy),
    source_stats: safeRecord(source.source_stats),
  }
}

export async function getNodesPage(params: NodesQuery = {}) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return
    if (value === 'all' && key !== 'availability') return
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
export async function probeAllNodesStream() {
  const path = '/api/nodes/probe-all'
  const res = await fetch(path, { method: 'POST', credentials: 'same-origin' })
  if (!res.ok) {
    const payload = await res.text().catch(() => '')
    throw new ApiError(`${path} HTTP ${res.status}${payload ? `: ${payload}` : ''}`, res.status, payload, path)
  }
  return res
}
export function probeNode(tag: string) { return api.post<{latency_ms?: number; error?: string}>(`/api/nodes/${encodeURIComponent(tag)}/probe`) }
export function blacklistNode(tag: string) { return api.post(`/api/nodes/${encodeURIComponent(tag)}/blacklist`, { duration: '24h' }) }
export function releaseNode(tag: string) { return api.post(`/api/nodes/${encodeURIComponent(tag)}/release`) }
export function confirmNodeRegion(tag: string, region: string) {
  return api.post<ConfirmNodeRegionResponse>(`/api/nodes/${encodeURIComponent(tag)}/region`, { region })
}
