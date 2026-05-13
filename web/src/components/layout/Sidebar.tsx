import { useAppStore } from '../../store/appStore'

const items = [
  ['extractor', '代理提取'],
  ['quality', '节点质量'],
  ['status', '运行状态'],
  ['settings', '系统设置'],
  ['diagnostics', '日志诊断'],
] as const

export function Sidebar() {
  const active = useAppStore(s => s.activeTab)
  const setActive = useAppStore(s => s.setActiveTab)
  return <aside className="sidebar">
    <div className="brand">Easy Proxies</div>
    <nav className="nav">{items.map(([id, label]) => <button key={id} className={active === id ? 'active' : ''} onClick={() => setActive(id)}>{label}</button>)}</nav>
  </aside>
}
