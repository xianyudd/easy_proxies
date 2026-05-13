import type { ReactNode } from 'react'
import { Sidebar } from './Sidebar'
import { Topbar } from './Topbar'
import { Toast } from '../ui/Toast'
import { useAppStore } from '../../store/appStore'

export function AppLayout({ children }: {children: ReactNode}) {
  const theme = useAppStore(s => s.theme)
  return <div className="app" data-theme={theme}>
    <Sidebar />
    <main className="main"><Topbar /><div className="content">{children}</div></main>
    <Toast />
  </div>
}
