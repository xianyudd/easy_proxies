import { api, request } from './client'
import type { LocalSourceDetail, LocalSourcePreview, LocalSourcesState } from '../types/localSource'
const root = '/api/local-sources'
export const getLocalSources = () => api.get<LocalSourcesState>(root)
export const getLocalSource = (id: string) => api.get<LocalSourceDetail>(`${root}/${encodeURIComponent(id)}`)
export const previewLocalSource = (content: string, id: string | null, revision: number) => api.post<LocalSourcePreview>(`${root}/preview`, { content, source_id: id || undefined, expected_revision: revision })
export const saveLocalSource = (id: string | null, body: { expected_revision: number; name: string; enabled: boolean; content?: string }) => id
  ? api.put<LocalSourcesState>(`${root}/${encodeURIComponent(id)}`, body)
  : api.post<LocalSourcesState>(root, body)
export const deleteLocalSource = (id: string, revision: number) => request<LocalSourcesState>(`${root}/${encodeURIComponent(id)}`, { method: 'DELETE', body: JSON.stringify({ expected_revision: revision }) })
export const applyLocalSources = (revision: number) => api.post(`${root}/apply`, { expected_revision: revision })
