import { useAppStore } from '../../store/appStore'

const items = [
  ['extractor', 'EX', '代理提取', 'Proxy Extractor'],
  ['quality', 'QL', '节点质量', 'Quality Radar'],
  ['status', 'ST', '运行状态', 'Live Telemetry'],
  ['settings', 'CF', '系统设置', 'Control Flags'],
  ['diagnostics', 'LG', '日志诊断', 'Event Stream'],
] as const

export function Sidebar() {
  const active = useAppStore(s => s.activeTab)
  const setActive = useAppStore(s => s.setActiveTab)
  return <aside className="sidebar">
    <div className="brand">Easy Proxies</div>
    <div className="sidebar-status">
      <span className="status-orb" />
      <div><strong>ARRAY ONLINE</strong><span>local control plane</span></div>
    </div>
    <nav className="nav">{items.map(([id, code, label, sub]) => <button key={id} className={active === id ? 'active' : ''} onClick={() => setActive(id)}><span className="nav-code">{code}</span><span className="nav-copy"><strong>{label}</strong><small>{sub}</small></span></button>)}</nav>
  </aside>
}
