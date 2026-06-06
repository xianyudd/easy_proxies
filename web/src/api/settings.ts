import { api } from './client'
import type { SaveSettingsResponse, SettingsResponse } from '../types/settings'

export function getSettings() { return api.get<SettingsResponse>('/api/settings') }
export function saveSettings(payload: SettingsResponse) { return api.put<SaveSettingsResponse>('/api/settings', payload) }
export function reloadCore() { return api.post<{ message?: string }>('/api/reload') }
export function getSubscriptionStatus() { return api.get<Record<string, unknown>>('/api/subscription/status') }
export function saveSubscriptionConfig(payload: {subscriptions: string[]; enabled: boolean; interval: string}) { return api.put('/api/subscription/config', payload) }
