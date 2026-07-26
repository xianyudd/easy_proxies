import { useMemo } from 'react'
import { Wifi } from 'lucide-react'
import { useAppStore } from '../../store/appStore'
import { APP_ROUTES, NAV_GROUPS, hashForTab, type AppTab } from '../../app/routes'

export function Sidebar() {
  const active = useAppStore(s => s.activeTab)
  const setActive = useAppStore(s => s.setActiveTab)

  const groups = useMemo(
    () =>
      NAV_GROUPS.map(group => ({
        ...group,
        items: APP_ROUTES.filter(r => r.group === group.id),
      })).filter(g => g.items.length > 0),
    [],
  )

  const activate = (id: AppTab) => {
    setActive(id)
    window.history.replaceState(null, '', hashForTab(id))
  }

  return (
    <aside className="sidebar ep-shell-sidebar">
      <div className="brand">
        <span className="brand-mark">EP</span>
        <div className="brand-copy">
          <strong>Easy Proxies</strong>
          {import.meta.env.DEV ? <span className="shell-preview-badge">Design preview</span> : null}
        </div>
      </div>

      <nav className="nav ep-shell-nav" aria-label="主导航">
        {groups.map(group => (
          <div key={group.id} className="ep-nav-group">
            <div className="ep-nav-group-label">{group.label}</div>
            {group.items.map(route => {
              const Icon = route.icon
              const isActive = active === route.id
              return (
                <button
                  key={route.id}
                  type="button"
                  className={isActive ? 'active' : ''}
                  onClick={() => activate(route.id)}
                  title={route.description}
                  aria-current={isActive ? 'page' : undefined}
                >
                  <span className="nav-code">
                    <Icon size={18} strokeWidth={2.1} />
                  </span>
                  <span className="nav-copy">
                    <strong>{route.title}</strong>
                  </span>
                </button>
              )
            })}
          </div>
        ))}
      </nav>

      <div className="sidebar-status">
        <span className="status-orb">
          <Wifi size={13} strokeWidth={2.4} />
        </span>
        <div>
          <strong>管理面</strong>
          <span className="ep-sidebar-status-sub">本地控制台</span>
        </div>
      </div>
    </aside>
  )
}
