export type EasyProxiesRegion = 'all' | 'us' | 'jp' | 'hk' | 'sg' | 'tw' | 'kr' | 'in' | 'ae' | 'ch' | 'au' | 'de' | 'gb' | 'ca' | 'other';
export type EasyProxiesMode = 'pool' | 'geoip' | 'multi-port' | 'android';
export type EasyProxiesFormat = 'host_port' | 'adb_command' | 'http_no_auth' | 'socks5_url' | 'socks5_no_auth' | 'csv' | 'pipe' | 'curl_command' | 'python_requests_json' | 'clash_yaml' | 'host_port_user_pass' | 'user_pass_at_host_port' | 'http_url' | 'host_port_user_pass_refresh_remark' | 'user_pass_at_host_port_refresh_remark' | 'json';
export interface EasyProxiesClientOptions {
    baseUrl?: string;
    token?: string;
    password?: string;
    fetchImpl?: typeof fetch;
}
export interface ExtractProxiesOptions {
    region?: EasyProxiesRegion;
    mode?: EasyProxiesMode;
    format?: EasyProxiesFormat;
    count?: number;
    reveal?: boolean;
}
export interface ProxyEntry {
    host: string;
    port: number;
    username?: string;
    password?: string;
    path?: string;
    region?: string;
    mode?: string;
    remark?: string;
    refresh_url?: string;
    node_name?: string;
    node_tag?: string;
    url: string;
}
export type ExtractorEntry = string | ProxyEntry | Record<string, unknown>;
export interface ExtractorResponse<TEntry = ExtractorEntry> {
    region: string;
    mode: string;
    requested_format: string;
    effective_format: string;
    masked: boolean;
    requested_count: number;
    output_count: number;
    warnings: string[];
    entries: TEntry[];
    supports_reveal: boolean;
    copy_requires_confirm: boolean;
}
export interface NodeInfo {
    tag?: string;
    name?: string;
    region?: string;
    country?: string;
    port?: number;
    available?: boolean;
    blacklisted?: boolean;
    last_latency_ms?: number;
    active_connections?: number;
    [key: string]: unknown;
}
export interface NodesResponse {
    nodes: NodeInfo[];
    total_nodes?: number;
    region_stats?: Record<string, number>;
    region_healthy?: Record<string, number>;
    [key: string]: unknown;
}
export interface CloudflareResult {
    node_name?: string;
    node_tag?: string;
    region?: string;
    host?: string;
    port?: number;
    exit_ip?: string;
    score?: number;
    level?: string;
    latency_ms?: number;
    cached?: boolean;
    checked_at?: string;
    error?: string;
    [key: string]: unknown;
}
export interface ReputationResult {
    node_name?: string;
    node_tag?: string;
    region?: string;
    port?: number;
    exit_ip?: string;
    risk_level?: string;
    risk_score?: number;
    cached?: boolean;
    checked_at?: string;
    error?: string;
    result?: ReputationResult;
    [key: string]: unknown;
}
export interface CacheResponse<T> {
    data: T[];
    count: number;
    [key: string]: unknown;
}
export declare class EasyProxiesError extends Error {
    status: number;
    payload: unknown;
    constructor(message: string, status: number, payload: unknown);
}
export declare class EasyProxiesClient {
    private baseUrl;
    private token?;
    private password?;
    private fetchImpl;
    constructor(options?: EasyProxiesClientOptions);
    login(password?: string | undefined): Promise<{
        token?: string;
        message?: string;
    }>;
    extract(options?: ExtractProxiesOptions): Promise<ExtractorResponse>;
    getProxyUrls(options?: Omit<ExtractProxiesOptions, 'format'>): Promise<string[]>;
    getProxyEntries(options?: Omit<ExtractProxiesOptions, 'format'>): Promise<ProxyEntry[]>;
    getNodes(): Promise<NodesResponse>;
    getCloudflareCache(): Promise<CacheResponse<CloudflareResult>>;
    getReputationCache(): Promise<CacheResponse<ReputationResult>>;
    checkCloudflare(options?: {
        region?: EasyProxiesRegion;
        count?: number;
        includeUnavailable?: boolean;
        retryFailed?: boolean;
    }): Promise<CacheResponse<CloudflareResult>>;
    checkReputation(options?: {
        region?: EasyProxiesRegion;
        count?: number;
        includeUnavailable?: boolean;
        retryFailed?: boolean;
    }): Promise<CacheResponse<ReputationResult>>;
    private get;
    private request;
}
