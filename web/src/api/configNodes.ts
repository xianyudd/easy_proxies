import { api } from './client'
import type { ConfigNode, ConfigNodeMutationResponse, ConfigNodesResponse, DeleteConfigNodeResponse, ReloadCoreResponse } from '../types/configNode'
import type { ReloadStatus } from '../types/settings'

export function getConfigNodes() {
  return api.get<ConfigNodesResponse>('/api/nodes/config')
}

export function createConfigNode(payload: ConfigNode) {
  return api.post<ConfigNodeMutationResponse>('/api/nodes/config', payload)
}

export function updateConfigNode(name: string, payload: ConfigNode) {
  return api.put<ConfigNodeMutationResponse>(`/api/nodes/config/${encodeURIComponent(name)}`, payload)
}

export function deleteConfigNode(name: string) {
  return api.delete<DeleteConfigNodeResponse>(`/api/nodes/config/${encodeURIComponent(name)}`)
}

export function reloadCore() {
  return api.post<ReloadCoreResponse>('/api/reload')
}

export function getReloadStatus() {
  return api.get<ReloadStatus>('/api/reload/status')
}
