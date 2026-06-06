import { api } from './client'
import type { PagedQualityResults, QualityJobCreated, QualityJobRequest, QualityJobSnapshot } from '../types/qualityJob'

export function createQualityJob(request: QualityJobRequest) {
  return api.post<QualityJobCreated>('/api/quality/jobs', request)
}

export function getQualityJob(id: string) {
  return api.get<QualityJobSnapshot>(`/api/quality/jobs/${encodeURIComponent(id)}`)
}

export function getQualityJobResults(id: string, params: { page?: number; page_size?: number } = {}) {
  const search = new URLSearchParams()
  search.set('page', String(params.page || 1))
  search.set('page_size', String(params.page_size || 100))
  return api.get<PagedQualityResults>(`/api/quality/jobs/${encodeURIComponent(id)}/results?${search.toString()}`)
}

export function cancelQualityJob(id: string) {
  return api.post<QualityJobSnapshot>(`/api/quality/jobs/${encodeURIComponent(id)}/cancel`)
}
