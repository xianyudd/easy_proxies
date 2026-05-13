import { api } from './client'
import type { ReputationResponse } from '../types/reputation'

export function getReputationCache() { return api.get<ReputationResponse>('/api/reputation/cache') }
export function checkReputation(region: string, count: number) {
  const q = new URLSearchParams({ region, mode: 'multi-port', count: String(count) })
  return api.get<ReputationResponse>(`/api/reputation/check?${q.toString()}`)
}
