import { useQuery } from '@tanstack/react-query'
import { getNodes } from '../../api/nodes'
import { useAppStore } from '../../store/appStore'
import { Button } from '../ui/Button'

export function Topbar() {
  const theme = useAppStore(s => s.theme)
  const setTheme = useAppStore(s => s.setTheme)
  const { data } = useQuery({ queryKey: ['nodes'], queryFn: getNodes, staleTime: 10000 })
  const healthy = (data || []).filter(n => n.available && !n.blacklisted).length
  return <header className="topbar">
    <div><span className="badge badge-good">在线</span> <span className="muted">节点 {healthy}/{data?.length || 0}</span></div>
    <div className="toolbar"><Button variant="ghost" onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}>{theme === 'dark' ? '浅色' : '深色'}</Button></div>
  </header>
}
