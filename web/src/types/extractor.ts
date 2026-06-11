// Region codes are server-driven ISO-3166 alpha-2 values plus "all"/"other".
// Keep this open instead of hardcoding a small union, otherwise newly supported
// countries can appear in the selector but fail TypeScript-level plumbing.
export type ExtractorRegion = string
export type ExtractorMode = 'pool'|'geoip'|'multi-port'|'android'
export type ExtractorFormat =
  | 'host_port'
  | 'adb_command'
  | 'http_no_auth'
  | 'socks5_url'
  | 'socks5_no_auth'
  | 'host_port_user_pass'
  | 'user_pass_at_host_port'
  | 'http_url'
  | 'csv'
  | 'pipe'
  | 'curl_command'
  | 'python_requests_json'
  | 'clash_yaml'
  | 'host_port_user_pass_refresh_remark'
  | 'user_pass_at_host_port_refresh_remark'
  | 'json'

export interface ExtractorParams {
  region: ExtractorRegion
  mode: ExtractorMode
  format: ExtractorFormat
  count: number
  reveal: boolean
}

export type ExtractorEntry = string | Record<string, unknown>

export interface ExtractorResponse {
  mode: ExtractorMode | string
  region: ExtractorRegion | string
  requested_count: number
  output_count: number
  effective_format: ExtractorFormat | string
  masked: boolean
  warnings?: string[]
  entries: ExtractorEntry[]
  metadata?: Record<string, unknown>
  error?: string
}
