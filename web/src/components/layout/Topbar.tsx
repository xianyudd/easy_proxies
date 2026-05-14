import { useQuery } from '@tanstack/react-query'
import { getNodes } from '../../api/nodes'
import { useAppStore } from '../../store/appStore'
import { Button } from '../ui/Button'

export function Topbar() {
  const theme = useAppStore(s => s.theme)
  const setTheme = useAppStore(s => s.setTheme)
  const { data } = useQuery({ queryKey: ['nodes'], queryFn: getNodes, staleTime: 10000 })
  const total = data?.length || 0
  const healthy = (data || []).filter(n => n.available && !n.blacklisted).length
  const activeConnections = (data || []).reduce((sum, n) => sum + (Number(n.active_connections) || 0), 0)
  const healthRate = total ? Math.round((healthy / total) * 100) : 0
  return <header className="topbar">
    <div className="telemetry-strip">
      <div className="telemetry-item"><span>Health</span><strong>{healthRate}%</strong></div>
      <div className="telemetry-item"><span>Nodes</span><strong>{healthy}/{total}</strong></div>
      <div className="telemetry-item"><span>Sessions</span><strong>{activeConnections}</strong></div>
    </div>
    <div className="toolbar"><span className="badge badge-good">在线</span><Button variant="ghost" onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}>{theme === 'dark' ? '浅色' : '深色'}</Button></div>
  </header>
}
