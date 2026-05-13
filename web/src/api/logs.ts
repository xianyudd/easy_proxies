import { api } from './client'
export function getLogs() { return api.get<{logs?: string}>('/api/logs') }
export function getDebug() { return api.get<Record<string, unknown>>('/api/debug') }
export function login(password: string) { return api.post<{token?: string; message?: string}>('/api/auth', { password }) }
