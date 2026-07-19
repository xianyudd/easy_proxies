import { api } from './client'
import type { FreeProxyRefreshStatus, ReloadStatus, SaveSettingsResponse, SettingsResponse, StartFreeProxyRefreshResponse, SubscriptionConfigResponse } from '../types/settings'

export function getSettings() { return api.get<SettingsResponse>('/api/settings') }
export function saveSettings(payload: SettingsResponse) { return api.put<SaveSettingsResponse>('/api/settings', payload) }

export interface ApiKeyMeta {
  name?: string
  role?: string
  enabled?: boolean
  key_set?: boolean
  key?: string
  hint?: string
  rotated?: boolean
}

export function listApiKeys() {
  return api.get<{ api_keys?: ApiKeyMeta[] }>('/api/management/api-keys')
}

/** Server generates epk_… secret; plaintext returned once in response.api_key.key */
export function createApiKey(payload: { name?: string; role?: 'read' | 'admin'; enabled?: boolean } = {}) {
  return api.post<{ message?: string; api_key?: ApiKeyMeta }>('/api/management/api-keys', payload)
}

export function deleteApiKey(name: string) {
  return api.delete<{ message?: string; name?: string }>(`/api/management/api-keys?name=${encodeURIComponent(name)}`)
}

export function updateApiKey(
  name: string,
  payload: { name?: string; role?: 'read' | 'admin'; enabled?: boolean; rotate?: boolean },
) {
  return api.patch<{ message?: string; api_key?: ApiKeyMeta }>(
    `/api/management/api-keys?name=${encodeURIComponent(name)}`,
    payload,
  )
}

export type ApiKeyBulkAction =
  | { type: 'enable' }
  | { type: 'disable' }
  | { type: 'set_role'; role: 'read' | 'admin' }
  | { type: 'delete' }

/** Client-side bulk over single-key endpoints; concurrent with a small cap. */
export async function runBulkApiKeyAction(names: string[], action: ApiKeyBulkAction, concurrency = 4) {
  const unique = [...new Set(names.map(n => n.trim()).filter(Boolean))]
  let ok = 0
  let fail = 0
  const errors: string[] = []
  let idx = 0

  const worker = async () => {
    while (idx < unique.length) {
      const i = idx++
      const name = unique[i]
      try {
        if (action.type === 'delete') {
          await deleteApiKey(name)
        } else if (action.type === 'enable') {
          await updateApiKey(name, { enabled: true })
        } else if (action.type === 'disable') {
          await updateApiKey(name, { enabled: false })
        } else {
          await updateApiKey(name, { role: action.role })
        }
        ok++
      } catch (e) {
        fail++
        errors.push(`${name}: ${e instanceof Error ? e.message : '失败'}`)
      }
    }
  }

  const n = Math.max(1, Math.min(concurrency, unique.length || 1))
  await Promise.all(Array.from({ length: n }, () => worker()))
  return { ok, fail, errors, total: unique.length }
}

/** Local admin helper: reveal current management password (requires admin session). */
export function getManagementPassword() {
  return api.get<{ password_set?: boolean; password?: string }>('/api/auth/password')
}
export function reloadCore() { return api.post<{ message?: string; started?: boolean; reload_status?: ReloadStatus }>('/api/reload') }
export function getReloadStatus() { return api.get<ReloadStatus>('/api/reload/status') }
export function getFreeProxyRefreshStatus() { return api.get<FreeProxyRefreshStatus>('/api/free-proxy/refresh/status') }
export function startFreeProxyRefresh() { return api.post<StartFreeProxyRefreshResponse>('/api/free-proxy/refresh') }

function sleep(ms: number) { return new Promise(resolve => window.setTimeout(resolve, ms)) }

export async function reloadCoreWithRetry(attempts = 3) {
  let lastError: unknown
  for (let attempt = 1; attempt <= attempts; attempt += 1) {
    try {
      return await reloadCore()
    } catch (error) {
      lastError = error
      if (attempt === attempts) break
      await sleep(800 * attempt)
    }
  }
  throw lastError
}
export function getSubscriptionStatus() { return api.get<Record<string, unknown>>('/api/subscription/status') }
export function saveSubscriptionConfig(payload: {subscriptions: string[]; enabled: boolean; interval: string}) { return api.put<SubscriptionConfigResponse>('/api/subscription/config', payload) }
