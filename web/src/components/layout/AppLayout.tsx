import type { ReactNode } from 'react'
import { useEffect } from 'react'
import { AppHeader } from './AppHeader'
import { Toast } from '../ui/Toast'
import { BootSplash } from '../motion/BootSplash'
import { useAppStore } from '../../store/appStore'

/** Top-navigation shell: sticky header + document-scrolled centered canvas. */
export function AppLayout({ children }: {children: ReactNode}) {
  const theme = useAppStore(s => s.theme)
  useEffect(() => {
    document.documentElement.dataset.theme = theme
  }, [theme])
  return <div className="ep-shell">
    {import.meta.env.DEV ? <BootSplash /> : null}
    <AppHeader />
    <main className="ep-canvas">
      <div className="content ep-content">{children}</div>
    </main>
    <Toast />
  </div>
}
