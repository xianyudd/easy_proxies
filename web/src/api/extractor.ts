import { api } from './client'
import type { ExtractorParams, ExtractorResponse } from '../types/extractor'

export function getExtractor(params: ExtractorParams) {
  const q = new URLSearchParams({
    region: params.region,
    mode: params.mode,
    format: params.format,
    count: String(params.count),
    reveal: params.reveal ? 'true' : 'false',
  })
  return api.get<ExtractorResponse>(`/api/extractor?${q.toString()}`)
}
