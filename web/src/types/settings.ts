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
  [key: string]: unknown
}
