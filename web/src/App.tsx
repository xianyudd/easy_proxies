import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AppLayout } from './components/layout/AppLayout'
import { useAppStore } from './store/appStore'
import { getAuthStatus, login } from './api/logs'
import { Button } from './components/ui/Button'
import { useToast } from './components/ui/Toast'
import { ExtractorPage } from './pages/ExtractorPage'
import { NodeOverviewPage } from './pages/NodeOverviewPage'
import { NodeConfigPage } from './pages/NodeConfigPage'
import { QualityPage } from './pages/QualityPage'
import { StatusPage } from './pages/StatusPage'
import { SettingsPage } from './pages/SettingsPage'
import { DiagnosticsPage } from './pages/DiagnosticsPage'

const hashTabMap = new Map<string, ReturnType<typeof useAppStore.getState>['activeTab']>([
  ['#extractor', 'extractor'],
  ['#overview', 'overview'],
  ['#config', 'config'],
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
  const queryClient = useQueryClient()
  const mutation = useMutation({ mutationFn: login, onSuccess: () => { queryClient.setQueryData(['auth-probe'], { authenticated: true, password_required: true }); queryClient.invalidateQueries({ queryKey: ['auth-probe'] }); setAuthed('authenticated'); toast('登录成功', 'ok') }, onError: (e) => toast(e instanceof Error ? e.message : '登录失败', 'error') })
  return <div className="login"><form className="login-box" onSubmit={(e) => { e.preventDefault(); mutation.mutate(password) }}>
    <h2>Easy Proxies</h2><p className="muted">请输入本地管理密码。</p>
    <input className="input" type="password" value={password} onChange={e => setPassword(e.target.value)} autoFocus />
    <Button variant="primary" htmlType="submit" disabled={mutation.isPending}>{mutation.isPending ? '验证中...' : '登录'}</Button>
  </form></div>
}

export default function App() {
  const activeTab = useAppStore(s => s.activeTab)
  const authenticated = useAppStore(s => s.authenticated)
  const setAuthenticated = useAppStore(s => s.setAuthenticated)
  const setActiveTab = useAppStore(s => s.setActiveTab)
  const authProbe = useQuery({ queryKey: ['auth-probe'], queryFn: getAuthStatus, retry: false, enabled: authenticated !== 'unauthenticated' })
  const verifyingAuth = authenticated === 'authenticated' && (authProbe.isLoading || authProbe.isFetching)
  useEffect(() => {
    const syncHash = () => {
      const tab = hashTabMap.get(window.location.hash)
      if (tab) setActiveTab(tab)
    }
    syncHash()
    window.addEventListener('hashchange', syncHash)
    return () => window.removeEventListener('hashchange', syncHash)
  }, [setActiveTab])
  useEffect(() => {
    if (authProbe.isSuccess) {
      setAuthenticated(authProbe.data.authenticated ? 'authenticated' : 'unauthenticated')
    }
    if (authProbe.isError && authenticated !== 'unauthenticated') {
      setAuthenticated('unauthenticated')
    }
  }, [authProbe.data?.authenticated, authProbe.isError, authProbe.isSuccess, authenticated, setAuthenticated])
  if (authenticated === 'unknown' || verifyingAuth) return <LoginPage />
  if (authenticated === 'unauthenticated') return <LoginPage />
  return <AppLayout>
    {activeTab === 'extractor' && <ExtractorPage />}
    {activeTab === 'overview' && <NodeOverviewPage />}
    {activeTab === 'config' && <NodeConfigPage />}
    {activeTab === 'quality' && <QualityPage />}
    {activeTab === 'status' && <StatusPage />}
    {activeTab === 'settings' && <SettingsPage />}
    {activeTab === 'diagnostics' && <DiagnosticsPage />}
  </AppLayout>
}
