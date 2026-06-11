import { Activity, FileSearch, Gauge, List, MapPin, ServerCog, Settings, ShieldCheck, Wifi } from 'lucide-react'
import { useAppStore } from '../../store/appStore'

const items = [
  ['extractor', FileSearch, '代理提取'],
  ['overview', List, '节点总览'],
  ['review', MapPin, '待确认节点'],
  ['config', ServerCog, '节点配置'],
  ['quality', ShieldCheck, '节点质量'],
  ['status', Gauge, '运行状态'],
  ['settings', Settings, '系统设置'],
  ['diagnostics', Activity, '日志诊断'],
] as const

const tabHashes: Record<typeof items[number][0], string> = {
  extractor: 'extractor',
  overview: 'nodes',
  review: 'region-review',
  config: 'config',
  quality: 'quality',
  status: 'status',
  settings: 'settings',
  diagnostics: 'diagnostics',
}

export function Sidebar() {
  const active = useAppStore(s => s.activeTab)
  const setActive = useAppStore(s => s.setActiveTab)
  const activate = (id: typeof items[number][0]) => {
    setActive(id)
    window.history.replaceState(null, '', `#${tabHashes[id]}`)
  }
  return <aside className="sidebar">
    <div className="brand"><span className="brand-mark">EP</span><strong>Easy Proxies</strong></div>
    <nav className="nav">{items.map(([id, Icon, label]) => <button key={id} className={active === id ? 'active' : ''} onClick={() => activate(id)}><span className="nav-code"><Icon size={18} strokeWidth={2.1} /></span><span className="nav-copy"><strong>{label}</strong></span></button>)}</nav>
    <div className="sidebar-status">
      <span className="status-orb"><Wifi size={13} strokeWidth={2.4} /></span>
      <strong>服务在线</strong>
    </div>
  </aside>
}
