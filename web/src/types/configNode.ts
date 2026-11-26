import type { ReloadStatus } from './settings'

export interface ConfigNode {
  name?: string
  uri?: string
  port?: number
  username?: string
  password?: string
  source?: string
  subscription_name?: string
  local_sources?: { id: string; name: string }[]
  [key: string]: unknown
}

export interface ConfigNodesResponse {
  nodes?: ConfigNode[]
}

export interface ConfigNodeMutationResponse {
  node?: ConfigNode
  message?: string
  need_reload?: boolean
}

export interface DeleteConfigNodeResponse {
  message?: string
  need_reload?: boolean
}

export interface ReloadCoreResponse {
  message?: string
  started?: boolean
  reload_status?: ReloadStatus
}
