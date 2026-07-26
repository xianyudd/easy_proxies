import { useEffect, useState, type ReactElement } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AppLayout } from './components/layout/AppLayout'
import { useAppStore } from './store/appStore'
import { getAuthStatus, login } from './api/logs'
import { UNAUTHORIZED_EVENT } from './api/client'
import { Button } from './components/ui/Button'
import { Toast, useToast } from './components/ui/Toast'
import { hashForTab, tabFromHash, type AppTab } from './app/routes'
import { ExtractorPage } from './pages/ExtractorPage'
import { NodeOverviewPage } from './pages/NodeOverviewPage'
import { RegionReviewPage } from './pages/RegionReviewPage'
import { NodeConfigPage } from './pages/NodeConfigPage'
import { QualityPage } from './pages/QualityPage'
import { StatusPage } from './pages/StatusPage'
import { SettingsPage } from './pages/SettingsPage'
import { ApiKeysPage } from './pages/ApiKeysPage'
import { DiagnosticsPage } from './pages/DiagnosticsPage'

const PAGE_BY_TAB: Record<AppTab, () => ReactElement> = {
  extractor: ExtractorPage,
  overview: NodeOverviewPage,
  review: RegionReviewPage,
  config: NodeConfigPage,
  quality: QualityPage,
  status: StatusPage,
  'api-keys': ApiKeysPage,
  settings: SettingsPage,
  diagnostics: DiagnosticsPage,
}

function LoginPage({ mode }: { mode: 'boot' | 'login' }) {
  const [password, setPassword] = useState('')
  const setAuthed = useAppStore(s => s.setAuthenticated)
  const setActiveTab = useAppStore(s => s.setActiveTab)
  const theme = useAppStore(s => s.theme)
  const toast = useToast(s => s.show)
  const queryClient = useQueryClient()

  // Full-screen login: never show app chrome; lock body scroll.
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    document.body.classList.add('ep-auth-screen')
    return () => {
      document.body.classList.remove('ep-auth-screen')
    }
  }, [theme])

  const mutation = useMutation({
    mutationFn: login,
    onSuccess: () => {
      queryClient.setQueryData(['auth-probe'], { authenticated: true, password_required: true })
      void queryClient.invalidateQueries({ queryKey: ['auth-probe'] })
      setAuthed('authenticated')
      // Land on extractor workbench after login (product home).
      setActiveTab('extractor')
      window.history.replaceState(null, '', hashForTab('extractor'))
      toast('登录成功', 'ok')
    },
    onError: (e) => toast(e instanceof Error ? e.message : '登录失败', 'error'),
  })

  return (
    <div className="ep-login" data-mode={mode}>
      <form
        className="login-box ep-login-box"
        onSubmit={(e) => {
          e.preventDefault()
          if (!password.trim() || mutation.isPending) return
          mutation.mutate(password)
        }}
      >
        <div className="ep-login-brand">
          <span className="brand-mark">EP</span>
          <div>
            <h2>Easy Proxies</h2>
            {import.meta.env.DEV ? <span className="shell-preview-badge">Design preview</span> : null}
          </div>
        </div>
        <p className="muted">
          {mode === 'boot' ? '正在校验会话…若需要请输入本地管理密码。' : '请输入本地管理密码以进入控制台。'}
        </p>
        <label htmlFor="login-password" className="sr-only">管理密码</label>
        <input
          id="login-password"
          className="input"
          type="password"
          aria-label="管理密码"
          value={password}
          onChange={e => setPassword(e.target.value)}
          autoFocus
          autoComplete="current-password"
          disabled={mutation.isPending}
        />
        <Button variant="primary" htmlType="submit" disabled={mutation.isPending || !password.trim()}>
          {mutation.isPending ? '验证中…' : '登录'}
        </Button>
      </form>
      <Toast />
    </div>
  )
}

export default function App() {
  const queryClient = useQueryClient()
  const activeTab = useAppStore(s => s.activeTab)
  const authenticated = useAppStore(s => s.authenticated)
  const setAuthenticated = useAppStore(s => s.setAuthenticated)
  const setActiveTab = useAppStore(s => s.setActiveTab)
  const authProbe = useQuery({
    queryKey: ['auth-probe'],
    queryFn: getAuthStatus,
    retry: false,
    enabled: authenticated !== 'unauthenticated',
  })
  // Only block on the first auth resolution — never flash LoginPage during background refetch.
  const bootstrappingAuth =
    authenticated === 'unknown' || (authenticated === 'authenticated' && authProbe.isLoading && !authProbe.data)

  useEffect(() => {
    const syncHash = () => {
      setActiveTab(tabFromHash(window.location.hash))
    }
    syncHash()
    window.addEventListener('hashchange', syncHash)
    return () => window.removeEventListener('hashchange', syncHash)
  }, [setActiveTab])

  useEffect(() => {
    const handleUnauthorized = () => {
      queryClient.clear()
      setAuthenticated('unauthenticated')
    }
    window.addEventListener(UNAUTHORIZED_EVENT, handleUnauthorized)
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, handleUnauthorized)
  }, [queryClient, setAuthenticated])

  useEffect(() => {
    if (authProbe.isSuccess) {
      setAuthenticated(authProbe.data.authenticated ? 'authenticated' : 'unauthenticated')
    }
    if (authProbe.isError && authenticated !== 'unauthenticated') {
      setAuthenticated('unauthenticated')
    }
  }, [authProbe.data?.authenticated, authProbe.isError, authProbe.isSuccess, authenticated, setAuthenticated])

  // Strict: no Sidebar/Topbar until authenticated. Full-screen auth only.
  if (bootstrappingAuth) return <LoginPage mode="boot" />
  if (authenticated === 'unauthenticated') return <LoginPage mode="login" />

  const ActivePage = PAGE_BY_TAB[activeTab] || ExtractorPage
  return (
    <AppLayout>
      <ActivePage key={activeTab} />
    </AppLayout>
  )
}
