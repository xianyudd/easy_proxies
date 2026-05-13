import { api } from './client'
import type { CloudflareCheckResponse } from '../types/cloudflare'

export function getCloudflareCache() { return api.get<CloudflareCheckResponse>('/api/cloudflare/cache') }
export function checkCloudflare(region: string, count: number, includeUnavailable: boolean) {
  const q = new URLSearchParams({ region, mode: 'multi-port', count: String(count) })
  if (includeUnavailable) q.set('include_unavailable', 'true')
  return api.get<CloudflareCheckResponse>(`/api/cloudflare/check?${q.toString()}`)
}
