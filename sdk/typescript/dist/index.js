export class EasyProxiesError extends Error {
    constructor(message, status, payload) {
        super(message);
        this.name = 'EasyProxiesError';
        this.status = status;
        this.payload = payload;
    }
}
export class EasyProxiesClient {
    constructor(options = {}) {
        this.baseUrl = (options.baseUrl || 'http://127.0.0.1:9091').replace(/\/+$/, '');
        this.token = options.token;
        this.password = options.password;
        this.fetchImpl = options.fetchImpl || fetch;
    }
    async login(password = this.password) {
        if (!password)
            throw new Error('password is required');
        const result = await this.request('/api/auth', {
            method: 'POST',
            body: JSON.stringify({ password }),
        });
        if (result.token)
            this.token = result.token;
        return result;
    }
    async extract(options = {}) {
        return this.get('/api/extractor', {
            region: options.region || 'all',
            mode: options.mode || 'pool',
            format: options.format || 'http_url',
            count: String(options.count || 1),
            reveal: options.reveal ? 'true' : undefined,
        });
    }
    async getProxyUrls(options = {}) {
        const response = await this.extract({ ...options, format: 'http_url' });
        return response.entries.map(String);
    }
    async getProxyEntries(options = {}) {
        const response = await this.extract({ ...options, format: 'json' });
        return response.entries;
    }
    async getNodes() {
        return this.get('/api/nodes');
    }
    async getCloudflareCache() {
        return this.get('/api/cloudflare/cache');
    }
    async getReputationCache() {
        return this.get('/api/reputation/cache');
    }
    async checkCloudflare(options = {}) {
        return this.get('/api/cloudflare/check', {
            region: options.region || 'all',
            mode: 'multi-port',
            count: String(options.count || 20),
            include_unavailable: options.includeUnavailable ? 'true' : undefined,
            retry_failed: options.retryFailed ? 'true' : undefined,
        });
    }
    async checkReputation(options = {}) {
        return this.get('/api/reputation/check', {
            region: options.region || 'all',
            mode: 'multi-port',
            count: String(options.count || 20),
            include_unavailable: options.includeUnavailable ? 'true' : undefined,
            retry_failed: options.retryFailed ? 'true' : undefined,
        });
    }
    get(path, query = {}) {
        const params = new URLSearchParams();
        for (const [key, value] of Object.entries(query)) {
            if (value !== undefined && value !== '')
                params.set(key, value);
        }
        const suffix = params.toString() ? `?${params.toString()}` : '';
        return this.request(`${path}${suffix}`);
    }
    async request(path, init = {}) {
        const headers = new Headers(init.headers);
        if (!headers.has('Content-Type') && init.body)
            headers.set('Content-Type', 'application/json');
        if (!headers.has('Accept'))
            headers.set('Accept', 'application/json');
        if (this.token)
            headers.set('Authorization', `Bearer ${this.token}`);
        const response = await this.fetchImpl(`${this.baseUrl}${path}`, {
            ...init,
            headers,
        });
        const contentType = response.headers.get('content-type') || '';
        const payload = contentType.includes('application/json')
            ? await response.json().catch(() => ({}))
            : await response.text();
        if (!response.ok) {
            const message = typeof payload === 'object' && payload && 'error' in payload
                ? String(payload.error)
                : `Easy Proxies request failed with HTTP ${response.status}`;
            throw new EasyProxiesError(message, response.status, payload);
        }
        return payload;
    }
}
