import { useQuery } from '@tanstack/react-query'
import { getNodesSummary } from '../../api/nodes'
import { getReloadStatus } from '../../api/settings'
import { useAppStore } from '../../store/appStore'
import { routeById } from '../../app/routes'
import { Button } from '../ui/Button'

function safeCount(input: unknown) {
  const value = Number(input)
  return Number.isFinite(value) && value >= 0 ? Math.trunc(value) : 0
}

function safeRecord(value: unknown): Record<string, number> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  return Object.fromEntries(Object.entries(value as Record<string, unknown>).map(([key, raw]) => [key, safeCount(raw)]))
}

function fmtCount(n: number, loading: boolean) {
  if (loading) return '…'
  if (n <= 0) return '—'
  return String(n)
}

export function Topbar() {
  const theme = useAppStore(s => s.theme)
  const setTheme = useAppStore(s => s.setTheme)
  const activeTab = useAppStore(s => s.activeTab)
  const route = routeById(activeTab)
  const summary = useQuery({ queryKey: ['nodes-summary'], queryFn: getNodesSummary, staleTime: 10000 })
  const reloadStatus = useQuery({ queryKey: ['topbar-reload-status'], queryFn: getReloadStatus, refetchInterval: 1500 })
  const data = summary.data
  const total = safeCount(data?.total_nodes)
  const regionHealthy = safeRecord(data?.region_healthy)
  const healthy = Object.values(regionHealthy).reduce((sum, n) => sum + safeCount(n), 0)
  const filtered = safeCount(data?.total_filtered) || total
  const loading = summary.isLoading && !data
  const failed = summary.isError && !data
  const reloadRunning = reloadStatus.data?.state === 'running'
  const probeRecoveryThreshold = Math.max(5, Math.ceil(total * 0.1))
  const probeRecovering = !reloadRunning && total > 0 && healthy < probeRecoveryThreshold

  let statusLabel = '在线'
  let statusClassName = 'badge badge-good'
  if (failed) {
    statusLabel = '摘要失败'
    statusClassName = 'badge badge-warn'
  } else if (reloadRunning) {
    statusLabel = '重载中'
    statusClassName = 'badge badge-warn'
  } else if (probeRecovering) {
    statusLabel = '探测恢复中'
    statusClassName = 'badge badge-warn'
  } else if (loading) {
    statusLabel = '同步中'
    statusClassName = 'badge badge-warn'
  }

  const nodeText = loading ? '…' : failed ? '—' : `${fmtCount(healthy, false)}/${fmtCount(total, false)}`
  const filterText = loading || failed ? '—' : fmtCount(filtered, false)

  return (
    <header className="topbar ep-shell-topbar">
      <div className="ep-topbar-context">
        <span className="ep-topbar-eyebrow">{route.eyebrow}</span>
        <strong className="ep-topbar-title">{route.title}</strong>
      </div>
      <div className="telemetry-strip" aria-label="节点摘要">
        <div className="telemetry-item">
          <span>节点</span>
          <strong title={failed ? '节点摘要加载失败' : '可用/总节点'}>{nodeText}</strong>
        </div>
        <div className="telemetry-item">
          <span>筛选</span>
          <strong title="筛选后节点数">{filterText}</strong>
        </div>
      </div>
      <div className="toolbar">
        <span className={statusClassName}>{statusLabel}</span>
        <Button variant="ghost" onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}>
          {theme === 'dark' ? '浅色' : '深色'}
        </Button>
      </div>
    </header>
  )
}
