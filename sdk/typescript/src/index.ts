export type EasyProxiesRegion =
  | 'all'
  | 'us'
  | 'jp'
  | 'hk'
  | 'sg'
  | 'tw'
  | 'kr'
  | 'in'
  | 'ae'
  | 'ch'
  | 'au'
  | 'de'
  | 'gb'
  | 'ca'
  | 'other'

export type EasyProxiesMode = 'pool' | 'geoip' | 'multi-port' | 'android'

export type EasyProxiesFormat =
  | 'host_port'
  | 'adb_command'
  | 'http_no_auth'
  | 'socks5_url'
  | 'socks5_no_auth'
  | 'csv'
  | 'pipe'
  | 'curl_command'
  | 'python_requests_json'
  | 'clash_yaml'
  | 'host_port_user_pass'
  | 'user_pass_at_host_port'
  | 'http_url'
  | 'host_port_user_pass_refresh_remark'
  | 'user_pass_at_host_port_refresh_remark'
  | 'json'

export interface EasyProxiesClientOptions {
  baseUrl?: string
  token?: string
  password?: string
  fetchImpl?: typeof fetch
}

export interface ExtractProxiesOptions {
  region?: EasyProxiesRegion
  mode?: EasyProxiesMode
  format?: EasyProxiesFormat
  count?: number
  reveal?: boolean
}

export interface ProxyEntry {
  host: string
  port: number
  username?: string
  password?: string
  path?: string
  region?: string
  mode?: string
  remark?: string
  refresh_url?: string
  node_name?: string
  node_tag?: string
  url: string
}

export type ExtractorEntry = string | ProxyEntry | Record<string, unknown>

export interface ExtractorResponse<TEntry = ExtractorEntry> {
  region: string
  mode: string
  requested_format: string
  effective_format: string
  masked: boolean
  requested_count: number
  output_count: number
  warnings: string[]
  entries: TEntry[]
  supports_reveal: boolean
  copy_requires_confirm: boolean
}

export interface NodeInfo {
  tag?: string
  name?: string
  region?: string
  country?: string
  port?: number
  available?: boolean
  blacklisted?: boolean
  last_latency_ms?: number
  active_connections?: number
  [key: string]: unknown
}

export interface NodesResponse {
  nodes: NodeInfo[]
  total_nodes?: number
  region_stats?: Record<string, number>
  region_healthy?: Record<string, number>
  [key: string]: unknown
}

export interface CloudflareResult {
  node_name?: string
  node_tag?: string
  region?: string
  host?: string
  port?: number
  exit_ip?: string
  score?: number
  level?: string
  latency_ms?: number
  cached?: boolean
  checked_at?: string
  error?: string
  [key: string]: unknown
}

export interface ReputationResult {
  node_name?: string
  node_tag?: string
  region?: string
  port?: number
  exit_ip?: string
  risk_level?: string
  risk_score?: number
  cached?: boolean
  checked_at?: string
  error?: string
  result?: ReputationResult
  [key: string]: unknown
}

export interface CacheResponse<T> {
  data: T[]
  count: number
  [key: string]: unknown
}

export class EasyProxiesError extends Error {
  status: number
  payload: unknown

  constructor(message: string, status: number, payload: unknown) {
    super(message)
    this.name = 'EasyProxiesError'
    this.status = status
    this.payload = payload
  }
}

export class EasyProxiesClient {
  private baseUrl: string
  private token?: string
  private password?: string
  private fetchImpl: typeof fetch

  constructor(options: EasyProxiesClientOptions = {}) {
    this.baseUrl = (options.baseUrl || 'http://127.0.0.1:9091').replace(/\/+$/, '')
    this.token = options.token
    this.password = options.password
    this.fetchImpl = options.fetchImpl || fetch
  }

  async login(password = this.password): Promise<{ token?: string; message?: string }> {
    if (!password) throw new Error('password is required')
    const result = await this.request<{ token?: string; message?: string }>('/api/auth', {
      method: 'POST',
      body: JSON.stringify({ password }),
    })
    if (result.token) this.token = result.token
    return result
  }

  async extract(options: ExtractProxiesOptions = {}): Promise<ExtractorResponse> {
    return this.get<ExtractorResponse>('/api/extractor', {
      region: options.region || 'all',
      mode: options.mode || 'pool',
      format: options.format || 'http_url',
      count: String(options.count || 1),
      reveal: options.reveal ? 'true' : undefined,
    })
  }

  async getProxyUrls(options: Omit<ExtractProxiesOptions, 'format'> = {}): Promise<string[]> {
    const response = await this.extract({ ...options, format: 'http_url' })
    return response.entries.map(String)
  }

  async getProxyEntries(options: Omit<ExtractProxiesOptions, 'format'> = {}): Promise<ProxyEntry[]> {
    const response = await this.extract({ ...options, format: 'json' })
    return response.entries as ProxyEntry[]
  }

  async getNodes(): Promise<NodesResponse> {
    return this.get<NodesResponse>('/api/nodes')
  }

  async getCloudflareCache(): Promise<CacheResponse<CloudflareResult>> {
    return this.get<CacheResponse<CloudflareResult>>('/api/cloudflare/cache')
  }

  async getReputationCache(): Promise<CacheResponse<ReputationResult>> {
    return this.get<CacheResponse<ReputationResult>>('/api/reputation/cache')
  }

  async checkCloudflare(options: { region?: EasyProxiesRegion; count?: number; includeUnavailable?: boolean; retryFailed?: boolean } = {}) {
    return this.get<CacheResponse<CloudflareResult>>('/api/cloudflare/check', {
      region: options.region || 'all',
      mode: 'multi-port',
      count: String(options.count || 20),
      include_unavailable: options.includeUnavailable ? 'true' : undefined,
      retry_failed: options.retryFailed ? 'true' : undefined,
    })
  }

  async checkReputation(options: { region?: EasyProxiesRegion; count?: number; includeUnavailable?: boolean; retryFailed?: boolean } = {}) {
    return this.get<CacheResponse<ReputationResult>>('/api/reputation/check', {
      region: options.region || 'all',
      mode: 'multi-port',
      count: String(options.count || 20),
      include_unavailable: options.includeUnavailable ? 'true' : undefined,
      retry_failed: options.retryFailed ? 'true' : undefined,
    })
  }

  private get<T>(path: string, query: Record<string, string | undefined> = {}): Promise<T> {
    const params = new URLSearchParams()
    for (const [key, value] of Object.entries(query)) {
      if (value !== undefined && value !== '') params.set(key, value)
    }
    const suffix = params.toString() ? `?${params.toString()}` : ''
    return this.request<T>(`${path}${suffix}`)
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(init.headers)
    if (!headers.has('Content-Type') && init.body) headers.set('Content-Type', 'application/json')
    if (!headers.has('Accept')) headers.set('Accept', 'application/json')
    if (this.token) headers.set('Authorization', `Bearer ${this.token}`)

    const response = await this.fetchImpl(`${this.baseUrl}${path}`, {
      ...init,
      headers,
    })
    const contentType = response.headers.get('content-type') || ''
    const payload = contentType.includes('application/json')
      ? await response.json().catch(() => ({}))
      : await response.text()

    if (!response.ok) {
      const message = typeof payload === 'object' && payload && 'error' in payload
        ? String((payload as { error: unknown }).error)
        : `Easy Proxies request failed with HTTP ${response.status}`
      throw new EasyProxiesError(message, response.status, payload)
    }

    return payload as T
  }
}
