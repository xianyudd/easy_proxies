import { useQuery } from '@tanstack/react-query'
import { getNodesSummary } from '../../api/nodes'
import { useAppStore } from '../../store/appStore'
import { Button } from '../ui/Button'

function safeCount(input: unknown) {
  const value = Number(input)
  return Number.isFinite(value) && value >= 0 ? Math.trunc(value) : 0
}

function safeRecord(value: unknown): Record<string, number> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  return Object.fromEntries(Object.entries(value as Record<string, unknown>).map(([key, raw]) => [key, safeCount(raw)]))
}

export function Topbar() {
  const theme = useAppStore(s => s.theme)
  const setTheme = useAppStore(s => s.setTheme)
  const { data } = useQuery({ queryKey: ['nodes-summary'], queryFn: getNodesSummary, staleTime: 10000 })
  const total = safeCount(data?.total_nodes)
  const regionHealthy = safeRecord(data?.region_healthy)
  const healthy = Object.values(regionHealthy).reduce((sum, n) => sum + safeCount(n), 0)
  return <header className="topbar">
    <div className="telemetry-strip">
      <div className="telemetry-item"><span>节点</span><strong>{healthy}/{total}</strong></div>
      <div className="telemetry-item"><span>筛选</span><strong>{data?.total_filtered || total}</strong></div>
    </div>
    <div className="toolbar"><span className="badge badge-good">在线</span><Button variant="ghost" onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}>{theme === 'dark' ? '浅色' : '深色'}</Button></div>
  </header>
}
