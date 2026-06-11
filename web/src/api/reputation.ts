import { api } from './client'
import type { ReputationResponse } from '../types/reputation'

export function getReputationCache() { return api.get<ReputationResponse>('/api/reputation/cache') }
export function checkReputation(region: string, count: number, retryFailed = false, includeUnavailable = false, source = 'all', updateRegions = false) {
  const q = new URLSearchParams({ region, mode: 'multi-port', count: String(count) })
  if (source && source !== 'all') q.set('source', source)
  if (retryFailed) q.set('retry_failed', 'true')
  if (includeUnavailable) q.set('include_unavailable', 'true')
  if (updateRegions) q.set('update_regions', 'true')
  return api.get<ReputationResponse>(`/api/reputation/check?${q.toString()}`)
}
