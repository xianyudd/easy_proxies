import { useQuery } from '@tanstack/react-query'
import { Moon, Sun } from 'lucide-react'
import { getNodesSummary } from '../../api/nodes'
import { getReloadStatus } from '../../api/settings'
import { useAppStore } from '../../store/appStore'
import { APP_ROUTES, hashForTab, type AppTab } from '../../app/routes'

function safeCount(input: unknown) {
  const value = Number(input)
  return Number.isFinite(value) && value >= 0 ? Math.trunc(value) : 0
}

function safeRecord(value: unknown): Record<string, number> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  return Object.fromEntries(Object.entries(value as Record<string, unknown>).map(([key, raw]) => [key, safeCount(raw)]))
}

/**
 * Top-navigation shell header (Vercel/GitHub style):
 * row 1 = brand | telemetry | status | theme, row 2 = horizontal tab nav.
 * Replaces the old Sidebar + Topbar pair.
 */
export function AppHeader() {
  const theme = useAppStore(s => s.theme)
  const setTheme = useAppStore(s => s.setTheme)
  const active = useAppStore(s => s.activeTab)
  const setActive = useAppStore(s => s.setActiveTab)

  const summary = useQuery({ queryKey: ['nodes-summary'], queryFn: getNodesSummary, staleTime: 10000 })
  const reloadStatus = useQuery({ queryKey: ['topbar-reload-status'], queryFn: getReloadStatus, refetchInterval: 1500 })

  const data = summary.data
  const total = safeCount(data?.total_nodes)
  const healthy = Object.values(safeRecord(data?.region_healthy)).reduce((sum, n) => sum + n, 0)
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

  const nodeText = loading ? '…' : failed ? '—' : `${healthy}/${total}`

  const activate = (id: AppTab) => {
    setActive(id)
    window.history.replaceState(null, '', hashForTab(id))
  }

  return (
    <header className="ep-hd">
      <div className="ep-hd-row">
        <div className="ep-hd-brand">
          <span className="brand-mark">EP</span>
          <strong>Easy Proxies</strong>
        </div>
        <div className="ep-hd-side">
          <span className="ep-hd-metric" title={failed ? '节点摘要加载失败' : '可用/总节点'}>
            <em>节点</em>
            <strong>{nodeText}</strong>
          </span>
          <span className={statusClassName}>{statusLabel}</span>
          <button
            type="button"
            className="ep-hd-icon-btn"
            onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
            title={theme === 'dark' ? '切换到浅色' : '切换到深色'}
            aria-label={theme === 'dark' ? '切换到浅色主题' : '切换到深色主题'}
          >
            {theme === 'dark' ? <Sun size={15} strokeWidth={2} /> : <Moon size={15} strokeWidth={2} />}
          </button>
        </div>
      </div>
      <nav className="ep-hd-tabs" aria-label="主导航">
        {APP_ROUTES.map(route => {
          const Icon = route.icon
          const isActive = active === route.id
          return (
            <button
              key={route.id}
              type="button"
              className={`ep-tab${isActive ? ' active' : ''}`}
              onClick={() => activate(route.id)}
              title={route.description}
              aria-current={isActive ? 'page' : undefined}
            >
              <Icon size={15} strokeWidth={2} />
              <span>{route.title}</span>
            </button>
          )
        })}
      </nav>
    </header>
  )
}
