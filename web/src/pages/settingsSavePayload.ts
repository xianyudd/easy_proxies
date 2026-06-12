import type { FreeProxySource, SettingsResponse } from '../types/settings'

function freeSourceKey(src: FreeProxySource) {
  return `${src.name || ''}|${src.url || ''}|${src.file || ''}`
}

export function freeSourceDefaultScheme(src: FreeProxySource) {
  const explicit = String(src.default_scheme || '').trim().toLowerCase()
  if (explicit === 'http' || explicit === 'socks5') return explicit
  const hint = `${src.name || ''} ${src.url || ''} ${src.file || ''}`.toLowerCase()
  return hint.includes('socks5') ? 'socks5' : 'http'
}

function normalizeFreeSourcesForSave(draftSources: FreeProxySource[], serverSources: FreeProxySource[]) {
  const serverEnabled = new Map(serverSources.map(src => [freeSourceKey(src), src.enabled !== false]))
  return draftSources.map(src => {
    const key = freeSourceKey(src)
    const knownEnabled = serverEnabled.get(key)
    return { ...src, enabled: src.enabled ?? knownEnabled ?? true, default_scheme: freeSourceDefaultScheme(src) }
  })
}

function normalizeManagementForSave(management: Record<string, unknown>, passwordDraft: string, clearPassword: boolean) {
  const next = { ...management }
  if (clearPassword) {
    next.clear_password = true
    delete next.password
  } else if (passwordDraft.trim()) {
    next.password = passwordDraft
    delete next.clear_password
  } else {
    delete next.password
    delete next.clear_password
  }
  delete next.password_set
  return next
}

function stable(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(stable).join(',')}]`
  if (value && typeof value === 'object') {
    const obj = value as Record<string, unknown>
    return `{${Object.keys(obj).sort().map(key => `${JSON.stringify(key)}:${stable(obj[key])}`).join(',')}}`
  }
  return JSON.stringify(value)
}

function sameSettingsValue(a: unknown, b: unknown) {
  return stable(a) === stable(b)
}

export function isSettingsDraftDirty(draft: SettingsResponse, serverSettings?: SettingsResponse) {
  if (!serverSettings) return false
  return !sameSettingsValue(draft, serverSettings)
}

function normalizeSubscriptionDraftText(value: string) {
  return String(value || '').replace(/\r\n/g, '\n').split('\n').map(item => item.trim()).join('\n')
}

export function subscriptionText(subscriptions: unknown) {
  return Array.isArray(subscriptions) ? subscriptions.map(item => String(item).trim()).join('\n') : ''
}

export function isSubscriptionDraftDirty(draft: string, serverSubscriptions: unknown) {
  return normalizeSubscriptionDraftText(draft) !== subscriptionText(serverSubscriptions)
}

export function buildSettingsSavePayload({
  draft,
  serverSettings,
  management,
  managementPasswordDraft,
  managementPasswordClear,
  subscriptions,
}: {
  draft: SettingsResponse
  serverSettings?: SettingsResponse
  management: Record<string, unknown>
  managementPasswordDraft: string
  managementPasswordClear: boolean
  subscriptions: string[]
}): SettingsResponse {
  const payload: SettingsResponse = {
    ...draft,
    management: normalizeManagementForSave(management, managementPasswordDraft, managementPasswordClear),
    subscriptions,
  }

  if (serverSettings && sameSettingsValue(draft.free_proxy_sources, serverSettings.free_proxy_sources)) {
    delete payload.free_proxy_sources
  } else {
    const serverSources = Array.isArray(serverSettings?.free_proxy_sources) ? serverSettings.free_proxy_sources as FreeProxySource[] : []
    const draftSources = Array.isArray(draft.free_proxy_sources) ? draft.free_proxy_sources as FreeProxySource[] : []
    payload.free_proxy_sources = normalizeFreeSourcesForSave(draftSources, serverSources)
  }
  if (serverSettings && sameSettingsValue(draft.free_proxy_filter, serverSettings.free_proxy_filter)) delete payload.free_proxy_filter
  if (serverSettings && sameSettingsValue(draft.free_proxy_cache, serverSettings.free_proxy_cache)) delete payload.free_proxy_cache
  if (serverSettings && sameSettingsValue(draft.free_proxy_max_nodes, serverSettings.free_proxy_max_nodes)) delete payload.free_proxy_max_nodes

  return payload
}
