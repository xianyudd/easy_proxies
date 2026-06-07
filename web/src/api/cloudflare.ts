import { api } from './client'
import type { CloudflareCheckResponse } from '../types/cloudflare'

export function getCloudflareCache() { return api.get<CloudflareCheckResponse>('/api/cloudflare/cache') }
export function checkCloudflare(region: string, count: number, includeUnavailable: boolean, retryFailed = false, source = 'all') {
  const q = new URLSearchParams({ region, mode: 'multi-port', count: String(count) })
  if (source && source !== 'all') q.set('source', source)
  if (includeUnavailable) q.set('include_unavailable', 'true')
  if (retryFailed) q.set('retry_failed', 'true')
  return api.get<CloudflareCheckResponse>(`/api/cloudflare/check?${q.toString()}`)
}
