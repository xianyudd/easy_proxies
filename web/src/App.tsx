import { useEffect, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { AppLayout } from './components/layout/AppLayout'
import { useAppStore } from './store/appStore'
import { getNodesSummary } from './api/nodes'
import { login } from './api/logs'
import { ApiError } from './api/client'
import { Button } from './components/ui/Button'
import { useToast } from './components/ui/Toast'
import { ExtractorPage } from './pages/ExtractorPage'
import { NodeOverviewPage } from './pages/NodeOverviewPage'
import { QualityPage } from './pages/QualityPage'
import { StatusPage } from './pages/StatusPage'
import { SettingsPage } from './pages/SettingsPage'
import { DiagnosticsPage } from './pages/DiagnosticsPage'

const hashTabMap = new Map<string, ReturnType<typeof useAppStore.getState>['activeTab']>([
  ['#extractor', 'extractor'],
  ['#overview', 'overview'],
  ['#quality', 'quality'],
  ['#status', 'status'],
  ['#settings', 'settings'],
  ['#subscriptions', 'settings'],
  ['#pool', 'settings'],
  ['#multi-port', 'settings'],
  ['#routing', 'settings'],
  ['#quality-check', 'settings'],
  ['#management', 'settings'],
  ['#diagnostics', 'diagnostics'],
])

function LoginPage() {
  const [password, setPassword] = useState('')
  const setAuthed = useAppStore(s => s.setAuthenticated)
  const toast = useToast(s => s.show)
  const mutation = useMutation({ mutationFn: login, onSuccess: () => { setAuthed(true); toast('登录成功', 'ok') }, onError: (e) => toast(e instanceof Error ? e.message : '登录失败', 'error') })
  return <div className="login"><form className="login-box" onSubmit={(e) => { e.preventDefault(); mutation.mutate(password) }}>
    <h2>Easy Proxies</h2><p className="muted">请输入本地管理密码。</p>
    <input className="input" type="password" value={password} onChange={e => setPassword(e.target.value)} autoFocus />
    <Button variant="primary" disabled={mutation.isPending}>{mutation.isPending ? '验证中...' : '登录'}</Button>
  </form></div>
}

export default function App() {
  const activeTab = useAppStore(s => s.activeTab)
  const authenticated = useAppStore(s => s.authenticated)
  const setAuthenticated = useAppStore(s => s.setAuthenticated)
  const setActiveTab = useAppStore(s => s.setActiveTab)
  const authProbe = useQuery({ queryKey: ['auth-probe'], queryFn: getNodesSummary, retry: false })
  useEffect(() => {
    const syncHash = () => {
      const tab = hashTabMap.get(window.location.hash)
      if (tab) setActiveTab(tab)
    }
    syncHash()
    window.addEventListener('hashchange', syncHash)
    return () => window.removeEventListener('hashchange', syncHash)
  }, [setActiveTab])
  if (authProbe.error instanceof ApiError && authProbe.error.status === 401 && authenticated) setAuthenticated(false)
  if (!authenticated) return <LoginPage />
  return <AppLayout>
    {activeTab === 'extractor' && <ExtractorPage />}
    {activeTab === 'overview' && <NodeOverviewPage />}
    {activeTab === 'quality' && <QualityPage />}
    {activeTab === 'status' && <StatusPage />}
    {activeTab === 'settings' && <SettingsPage />}
    {activeTab === 'diagnostics' && <DiagnosticsPage />}
  </AppLayout>
}
