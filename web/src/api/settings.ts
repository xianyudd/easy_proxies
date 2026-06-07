import { api } from './client'
import type { FreeProxyRefreshStatus, ReloadStatus, SaveSettingsResponse, SettingsResponse, StartFreeProxyRefreshResponse } from '../types/settings'

export function getSettings() { return api.get<SettingsResponse>('/api/settings') }
export function saveSettings(payload: SettingsResponse) { return api.put<SaveSettingsResponse>('/api/settings', payload) }
export function reloadCore() { return api.post<{ message?: string }>('/api/reload') }
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
export function saveSubscriptionConfig(payload: {subscriptions: string[]; enabled: boolean; interval: string}) { return api.put('/api/subscription/config', payload) }
