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
  external_ip?: string
  probe_target?: string
  skip_cert_verify?: boolean
}
