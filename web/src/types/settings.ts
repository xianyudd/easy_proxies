export interface FreeProxySource {
  name?: string
  url?: string
  file?: string
  format?: string
  default_scheme?: string
  enabled?: boolean
  timeout?: string
  max_nodes?: number
  max_bytes?: number
}

export interface FreeProxyFilter {
  enabled?: boolean
  min_tier?: string
  workers?: number
  timeout?: string
  max_candidates?: number
  max_probe_candidates?: number
  probes?: { http?: string; https?: string }
}

export interface FreeProxyCache {
  enabled?: boolean
  path?: string
  refresh_on_start?: boolean
  auto_reload?: boolean
  workers?: number
  max_age?: string
}

export interface SettingsResponse {
  mode?: string
  external_ip?: string
  listener?: Record<string, unknown>
  multi_port?: Record<string, unknown>
  android_proxy?: Record<string, unknown>
  geoip?: Record<string, unknown>
  management?: Record<string, unknown>
  log?: Record<string, unknown>
  subscription_refresh?: Record<string, unknown>
  subscriptions?: string[]
  free_proxy_sources?: FreeProxySource[]
  free_proxy_max_nodes?: number
  free_proxy_filter?: FreeProxyFilter
  free_proxy_cache?: FreeProxyCache
  [key: string]: unknown
}

export interface SaveSettingsResponse {
  message?: string
  need_reload?: boolean
  reload_started?: boolean
  reload_status?: {
    state?: string
    started_at?: string
    finished_at?: string
    duration_ms?: number
    error?: string
    requested_by?: string
    reload_pending?: boolean
  }
  reload_error?: string
  free_proxy_refresh_needed?: boolean
  free_proxy_refresh_started?: boolean
  free_proxy_refresh_status?: FreeProxyRefreshStatus
  free_proxy_refresh_error?: string
  external_ip?: string
  probe_target?: string
  skip_cert_verify?: boolean
  management_rebound?: boolean
  management_listen?: string
  management_url_hint?: string
}


export interface SubscriptionConfigResponse {
  message?: string
  subscriptions?: string[]
  enabled?: boolean
  interval?: string
  node_count?: number
  config_changed?: boolean
  refresh_triggered?: boolean
  refresh_error?: string
}

export interface StartFreeProxyRefreshResponse {
  message?: string
  started?: boolean
  status?: FreeProxyRefreshStatus
}

export interface ReloadStatus {
  state?: string
  started_at?: string
  finished_at?: string
  duration_ms?: number
  error?: string
  requested_by?: string
  reload_pending?: boolean
}

export interface FreeProxyRefreshStatus {
  state?: string
  started_at?: string
  finished_at?: string
  duration_ms?: number
  error?: string
  accepted?: number
  cache_updated?: boolean
  sources?: Array<{
    name?: string
    enabled?: boolean
    candidates?: number
    accepted?: number
    duration_ms?: number
    error?: string
  }>
  reload_started?: boolean
  reload_status?: ReloadStatus
  requested_by?: string
  cache_path?: string
  cache_max_age?: string
  cache_node_count?: number
  cache_fresh?: boolean
  cache_checked_at?: string
  cache_enabled?: boolean
  refresh_on_start?: boolean
  auto_reload?: boolean
  total_sources?: number
  enabled_sources?: number
  filter_enabled?: boolean
  filter_min_tier?: string
  filter_probe_budget?: number
  filter_max_candidates?: number
}
