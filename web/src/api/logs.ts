import { api } from './client'
export function getLogs() { return api.get<{logs?: string}>('/api/logs') }
export function getDebug() { return api.get<Record<string, unknown>>('/api/debug') }
export function getDebugSummary() { return api.get<Record<string, unknown>>('/api/debug?summary_only=true') }
export function login(password: string) { return api.post<{token?: string; message?: string}>('/api/auth', { password }) }
export function getAuthStatus() { return api.get<{authenticated: boolean; password_required?: boolean; no_password?: boolean}>('/api/auth/status') }
