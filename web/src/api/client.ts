export class ApiError extends Error {
  status: number
  payload: unknown
  path: string
  constructor(message: string, status: number, payload: unknown, path: string) {
    super(message)
    this.status = status
    this.payload = payload
    this.path = path
  }
}

function truncateMessage(text: string, limit = 240) {
  const normalized = text.replace(/\s+/g, ' ').trim()
  if (!normalized) return ''
  return normalized.length > limit ? `${normalized.slice(0, limit)}…` : normalized
}

export const UNAUTHORIZED_EVENT = 'easy-proxies:unauthorized'

function notifyUnauthorized(path: string, payload: unknown) {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent(UNAUTHORIZED_EVENT, { detail: { path, payload } }))
}

function errorMessage(status: number, payload: unknown, path: string) {
  if (typeof payload === 'object' && payload) {
    const record = payload as Record<string, unknown>
    const detail = record.error ?? record.message ?? record.code
    if (detail !== undefined && detail !== null && String(detail).trim()) {
      return `${path} HTTP ${status}: ${String(detail)}`
    }
  }
  if (typeof payload === 'string') {
    const detail = truncateMessage(payload)
    if (detail) return `${path} HTTP ${status}: ${detail}`
  }
  return `${path} HTTP ${status}`
}

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(path, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...(init.headers || {}) },
    ...init,
  })
  const contentType = res.headers.get('content-type') || ''
  const payload = contentType.includes('application/json') ? await res.json().catch(() => ({})) : parseTextPayload(await res.text())
  if (!res.ok) {
    if (res.status === 401) notifyUnauthorized(path, payload)
    throw new ApiError(errorMessage(res.status, payload, path), res.status, payload, path)
  }
  return payload as T
}

function parseTextPayload(text: string): unknown {
  const trimmed = text.trim()
  if (!trimmed) return text
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return text
  try {
    return JSON.parse(trimmed)
  } catch {
    return text
  }
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) => request<T>(path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) }),
  put: <T>(path: string, body?: unknown) => request<T>(path, { method: 'PUT', body: body === undefined ? undefined : JSON.stringify(body) }),
  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
}
