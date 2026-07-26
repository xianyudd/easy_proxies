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
export interface ProbeProgress {
  tag?: string
  name?: string
  latency?: number
  status?: string
  error?: string
  current: number
  total: number
  progress: number
}

export interface ProbeComplete {
  total: number
  success: number
  failed: number
}

/**
 * Consume the /api/nodes/probe-all SSE stream, invoking callbacks as events
 * arrive. Resolves with the final complete summary. Pass an AbortSignal to
 * cancel the in-flight probe.
 */
export async function probeAllNodes(
  handlers: {
    onStart?: (total: number) => void
    onProgress?: (progress: ProbeProgress) => void
    onComplete?: (summary: ProbeComplete) => void
  } = {},
  signal?: AbortSignal,
): Promise<ProbeComplete> {
  const path = '/api/nodes/probe-all'
  const res = await fetch(path, { method: 'POST', credentials: 'same-origin', signal })
  if (!res.ok) {
    const payload = await res.text().catch(() => '')
    throw new ApiError(`${path} HTTP ${res.status}${payload ? `: ${payload}` : ''}`, res.status, payload, path)
  }
  if (!res.body) throw new ApiError(`${path} 无响应流`, res.status, '', path)

  const reader = res.body.getReader()
  const decoder = new TextDecoder('utf-8')
  let buf = ''
  let summary: ProbeComplete | null = null

  const handleEvent = (raw: string) => {
    const line = raw.trim()
    if (!line.startsWith('data:')) return
    const json = line.slice(5).trim()
    if (!json) return
    let event: Record<string, unknown>
    try {
      event = JSON.parse(json)
    } catch {
      return
    }
    switch (event.type) {
      case 'start':
        handlers.onStart?.(Number(event.total) || 0)
        break
      case 'progress':
        handlers.onProgress?.({
          tag: event.tag as string | undefined,
          name: event.name as string | undefined,
          latency: Number(event.latency),
          status: event.status as string | undefined,
          error: (event.error as string) || undefined,
          current: Number(event.current) || 0,
          total: Number(event.total) || 0,
          progress: Number(event.progress) || 0,
        })
        break
      case 'complete':
        summary = {
          total: Number(event.total) || 0,
          success: Number(event.success) || 0,
          failed: Number(event.failed) || 0,
        }
        handlers.onComplete?.(summary)
        break
    }
  }

  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buf += decoder.decode(value, { stream: true })
    const parts = buf.split('\n\n')
    buf = parts.pop() || ''
    for (const part of parts) handleEvent(part)
  }
  if (buf.trim()) handleEvent(buf)
  // No complete event means the stream died mid-probe; reporting the zeroed
  // summary would render an aborted run as a successful run over zero nodes.
  if (!summary) throw new ApiError(`${path} 测速流中断，未收到完成事件`, res.status, '', path)
  return summary
}

export function probeNode(tag: string) { return api.post<{latency_ms?: number; error?: string}>(`/api/nodes/${encodeURIComponent(tag)}/probe`) }
export function blacklistNode(tag: string) { return api.post(`/api/nodes/${encodeURIComponent(tag)}/blacklist`, { duration: '24h' }) }
export function releaseNode(tag: string) { return api.post(`/api/nodes/${encodeURIComponent(tag)}/release`) }
export function confirmNodeRegion(tag: string, region: string) {
  return api.post<ConfirmNodeRegionResponse>(`/api/nodes/${encodeURIComponent(tag)}/region`, { region })
}

/**
 * Confirm regions for many nodes with bounded concurrency and per-item fault
 * tolerance: one failed node does not abort the rest. Returns success/failure
 * counts, the collected error messages, and whether any confirmation asked for
 * a core reload.
 */
export async function bulkConfirmRegions(
  items: Array<{ tag: string; region: string }>,
  concurrency = 4,
) {
  let ok = 0
  let fail = 0
  let needReload = false
  const errors: string[] = []
  let idx = 0

  const worker = async () => {
    while (idx < items.length) {
      const i = idx++
      const item = items[i]
      try {
        const res = await confirmNodeRegion(item.tag, item.region)
        if (res.need_reload) needReload = true
        ok++
      } catch (e) {
        fail++
        errors.push(`${item.tag}: ${e instanceof Error ? e.message : '失败'}`)
      }
    }
  }

  const n = Math.max(1, Math.min(concurrency, items.length || 1))
  await Promise.all(Array.from({ length: n }, () => worker()))
  return { ok, fail, need_reload: needReload, errors, total: items.length }
}
