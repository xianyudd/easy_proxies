import { useEffect, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Checkbox, Input, Select } from 'antd'
import { AlertCircle, Clock3, Database, Plus, Save, Trash2, Wifi } from 'lucide-react'
import { getFreeProxyRefreshStatus, getReloadStatus, getSettings, saveSettings, getSubscriptionStatus, saveSubscriptionConfig, startFreeProxyRefresh } from '../api/settings'
import { getCloudflareCache } from '../api/cloudflare'
import { getReputationCache } from '../api/reputation'
import { Button } from '../components/ui/Button'
import { QueryErrorBanner } from '../components/ui/QueryErrorBanner'
import { Badge } from '../components/ui/Badge'
import { useToast } from '../components/ui/Toast'
import type { FreeProxyCache, FreeProxyFilter, FreeProxyRefreshStatus, FreeProxySource, SettingsResponse } from '../types/settings'
import type { CloudflareResult } from '../types/cloudflare'
import type { ReputationResult } from '../types/reputation'

function listValue(value: unknown) { return Array.isArray(value) ? value.join('\n') : '' }
function boolValue(value: unknown) { return value === true || value === 'true' }
function clampNumber(value: unknown, fallback: number, min: number, max: number) {
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) return fallback
  return Math.max(min, Math.min(max, Math.trunc(parsed)))
}
function shortDate(value: unknown) {
  const text = String(value || '')
  if (!text || text.startsWith('0001-')) return '未执行'
  return new Date(text).toLocaleString()
}
function splitLines(value: string) { return value.split('\n').map(s => s.trim()).filter(Boolean) }
function editableLines(value: string) { return value === '' ? [] : value.split('\n') }
function isWideOpen(value: unknown) { return String(value || '').trim() === '0.0.0.0' }
function freeSourceKey(src: FreeProxySource) { return `${src.name || ''}|${src.url || ''}|${src.file || ''}` }
function normalizeFreeSourcesForSave(draftSources: FreeProxySource[], serverSources: FreeProxySource[]) {
  const serverEnabled = new Map(serverSources.map(src => [freeSourceKey(src), src.enabled !== false]))
  return draftSources.map(src => {
    const key = freeSourceKey(src)
    const knownEnabled = serverEnabled.get(key)
    return {...src, enabled: src.enabled ?? knownEnabled ?? true}
  })
}
function latestCheckedAt(rows: Array<{ checked_at?: unknown; result?: unknown }>) {
  const latest = rows
    .map(row => {
      const nested = row.result && typeof row.result === 'object' ? row.result as Record<string, unknown> : {}
      return String(row.checked_at || nested.checked_at || '')
    })
    .filter(Boolean)
    .sort()
  return shortDate(latest[latest.length - 1])
}
function cacheFreshLabel(value?: boolean) { return value ? '新鲜' : '需刷新' }
function freeProxyRefreshTitle(state: 'idle' | 'refreshing' | 'failed', status?: FreeProxyRefreshStatus) {
  if (state === 'failed') return '免费源扫描失败'
  if (status?.state === 'succeeded' && status.reload_started) return '免费源缓存已更新，正在重载入池'
  if (status?.state === 'succeeded' && status.cache_updated === false) return status.cache_fresh ? '免费源缓存仍新鲜，已复用本地缓存' : '免费源未产生新缓存，已复用旧缓存'
  return '免费源正在后台扫描'
}
function freeProxyRefreshDescription(state: 'idle' | 'refreshing' | 'failed', status?: FreeProxyRefreshStatus) {
  if (state === 'failed') return '免费源刷新未产生可入池节点，系统已保留现有缓存且不会自动重载；请检查源地址、探针或降低筛选等级。'
  if (status?.state === 'succeeded' && status.reload_started) return '候选代理已写入本地缓存；代理核心正在后台重载，完成后新节点才会出现在节点列表和质量检测中。'
  if (status?.state === 'succeeded' && status.cache_updated === false) {
    return status.cache_fresh ? '本地免费源缓存未过期，本次跳过远程下载和筛选，也不需要重载代理核心。' : '远程刷新没有产生可替换缓存，系统保留并复用旧缓存，避免清空当前节点池。'
  }
  return '系统正在下载、去重、预筛并写入缓存；完成后会按配置自动重载。'
}

export function SettingsPage() {
  const settings = useQuery({ queryKey:['settings'], queryFn:getSettings })
  const subStatus = useQuery({ queryKey:['sub-status'], queryFn:getSubscriptionStatus })
  const cfCache = useQuery({ queryKey:['cf-cache'], queryFn:getCloudflareCache })
  const repCache = useQuery({ queryKey:['rep-cache'], queryFn:getReputationCache })
  const [draft, setDraft] = useState<SettingsResponse>({})
  const [subs, setSubs] = useState('')
  const [reloadState, setReloadState] = useState<'idle' | 'reloading' | 'failed'>('idle')
  const [freeProxyRefreshState, setFreeProxyRefreshState] = useState<'idle' | 'refreshing' | 'failed'>('idle')
  const reloadStatus = useQuery({ queryKey:['reload-status'], queryFn:getReloadStatus, enabled: reloadState === 'reloading', refetchInterval: reloadState === 'reloading' ? 800 : false })
  const freeProxyRefreshStatus = useQuery({ queryKey:['free-proxy-refresh-status'], queryFn:getFreeProxyRefreshStatus, refetchInterval: freeProxyRefreshState === 'refreshing' ? 800 : false })
  const toast = useToast(s=>s.show)
  useEffect(()=>{ if(settings.data){ setDraft(settings.data); setSubs(listValue(settings.data.subscriptions)) } }, [settings.data])
  useEffect(() => {
    const id = window.location.hash.slice(1)
    if (!id) return
    window.setTimeout(() => document.getElementById(id)?.scrollIntoView({ block: 'start' }), 0)
  }, [])
  useEffect(() => {
    const state = String(reloadStatus.data?.state || '')
    if (state === 'succeeded') {
      if (reloadStatus.data?.requested_by === 'free-proxy-refresh') {
        setReloadState('idle')
        void settings.refetch()
        return
      }
      const duration = Number(reloadStatus.data?.duration_ms || 0)
      toast(duration > 0 ? `设置已在后台生效（${duration}ms）` : '设置已在后台生效', 'ok')
      setReloadState('idle')
      void settings.refetch()
      void subStatus.refetch()
    } else if (state === 'failed') {
      setReloadState('failed')
      toast(reloadStatus.data?.error ? `后台生效失败：${reloadStatus.data.error}` : '后台生效失败', 'error')
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reloadStatus.data?.state, reloadStatus.data?.duration_ms, reloadStatus.data?.error])
  useEffect(() => {
    const state = String(freeProxyRefreshStatus.data?.state || '')
    if (state === 'succeeded') {
      const duration = Number(freeProxyRefreshStatus.data?.duration_ms || 0)
      const accepted = Number(freeProxyRefreshStatus.data?.accepted || 0)
      if (freeProxyRefreshStatus.data?.reload_started) {
        const reloadState = String(freeProxyRefreshStatus.data?.reload_status?.state || '')
        if (reloadState === 'failed') {
          setFreeProxyRefreshState('failed')
          toast(freeProxyRefreshStatus.data.reload_status?.error ? `免费源已写入缓存，但入池重载失败：${freeProxyRefreshStatus.data.reload_status.error}` : '免费源已写入缓存，但入池重载失败', 'error')
          return
        }
        if (reloadState !== 'succeeded') {
          setFreeProxyRefreshState('refreshing')
          setReloadState('reloading')
          void reloadStatus.refetch()
          return
        }
      }
      if (freeProxyRefreshStatus.data?.cache_updated === false && !freeProxyRefreshStatus.data?.reload_started) {
        toast(`免费源缓存仍新鲜，已复用 ${accepted} 条，无需重新入池`, 'ok')
      } else if (freeProxyRefreshStatus.data?.cache_updated === false) {
        toast(`免费源未产生新缓存，已保留并复用 ${accepted} 条现有节点`, 'ok')
      } else {
        toast(`免费源扫描并入池完成：${accepted} 条，扫描用时 ${duration}ms`, 'ok')
      }
      setFreeProxyRefreshState('idle')
      setReloadState('idle')
      void settings.refetch()
      void freeProxyRefreshStatus.refetch()
    } else if (state === 'failed') {
      setFreeProxyRefreshState('failed')
      toast(freeProxyRefreshStatus.data?.error ? `免费源扫描失败：${freeProxyRefreshStatus.data.error}` : '免费源扫描失败', 'error')
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [freeProxyRefreshStatus.data?.state, freeProxyRefreshStatus.data?.duration_ms, freeProxyRefreshStatus.data?.accepted, freeProxyRefreshStatus.data?.cache_updated, freeProxyRefreshStatus.data?.reload_started, freeProxyRefreshStatus.data?.error, freeProxyRefreshStatus.data?.reload_status?.state, freeProxyRefreshStatus.data?.reload_status?.error])
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    if (params.get('autoReload') !== '1') return
    params.delete('autoReload')
    const nextSearch = params.toString()
    window.history.replaceState(null, '', `${window.location.pathname}${nextSearch ? `?${nextSearch}` : ''}${window.location.hash || '#settings'}`)
    setReloadState('reloading')
    void reloadStatus.refetch()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  const save = useMutation({ mutationFn: saveSettings, onSuccess:(res)=>{
    if (res?.management_rebound && res.management_url_hint) {
      toast('管理端口已热切换，正在跳转到新地址...', 'ok')
      const target = new URL(res.management_url_hint)
      if (res?.need_reload) target.searchParams.set('autoReload', '1')
      else target.searchParams.delete('autoReload')
      target.hash = 'settings'
      window.setTimeout(() => { window.location.href = target.toString() }, 300)
      return
    }
    void settings.refetch()
    if (res?.subscription_refresh_started) {
      toast('设置已保存，订阅后台刷新已启动...', 'ok')
      void subStatus.refetch()
      return
    }
    if (res?.free_proxy_refresh_needed) {
      if (res.free_proxy_refresh_error) {
        setFreeProxyRefreshState('failed')
        toast(`设置已保存，但免费源扫描启动失败：${res.free_proxy_refresh_error}`, 'error')
        return
      }
      toast(res.free_proxy_refresh_started ? '设置已保存，免费源开始后台扫描...' : '设置已保存，已有免费源扫描在运行...', 'ok')
      setFreeProxyRefreshState('refreshing')
      void freeProxyRefreshStatus.refetch()
      return
    }
    if (res?.need_reload) {
      if (res.reload_error) {
        setReloadState('failed')
        toast(`设置已保存，但后台生效启动失败：${res.reload_error}`, 'error')
        return
      }
      toast(res.reload_status?.reload_pending ? '设置已保存，已排队等待当前后台生效完成后继续生效...' : res.reload_started ? '设置已保存，后台正在生效...' : '设置已保存，已有后台生效任务在运行...', 'ok')
      setReloadState('reloading')
      void reloadStatus.refetch()
    } else {
      toast('设置已保存，已立即生效', 'ok')
      setReloadState('idle')
    }
  }, onError:e=>toast(e instanceof Error ? e.message:'保存失败','error') })
  const manualFreeRefresh = useMutation({ mutationFn: startFreeProxyRefresh, onSuccess:(res)=>{
    toast(res.started ? '免费源已开始后台刷新...' : '已有免费源刷新在运行...', 'ok')
    setFreeProxyRefreshState('refreshing')
    void freeProxyRefreshStatus.refetch()
  }, onError:e=>toast(e instanceof Error ? e.message:'免费源刷新启动失败','error') })
  const saveSub = useMutation({ mutationFn: saveSubscriptionConfig, onSuccess:(res)=>{ const changed = res.config_changed !== false; const refreshed = !!res.refresh_triggered; toast(refreshed ? '订阅配置已保存，后台刷新已启动' : changed ? '订阅配置已保存，调度已更新' : '订阅配置未变化，已保持当前状态', 'ok'); setReloadState('idle'); void settings.refetch(); void subStatus.refetch() }, onError:e=>toast(e instanceof Error ? e.message:'订阅保存失败','error') })
  const input = (label:string, value:string, onChange:(v:string)=>void, type='text') => <div className="field settings-form-item"><label>{label}</label><Input className="settings-input" aria-label={label} type={type} autoComplete={type === 'password' ? 'current-password' : label.includes('用户名') ? 'username' : undefined} value={value} onChange={e=>onChange(e.target.value)} /></div>
  const toggle = (label:string, checked:boolean, onChange:(v:boolean)=>void) => <Checkbox className="settings-checkbox" checked={checked} onChange={e=>onChange(e.target.checked)}>{label}</Checkbox>
  const listener = (draft.listener || {}) as Record<string, unknown>
  const mp = (draft.multi_port || {}) as Record<string, unknown>
  const geo = (draft.geoip || {}) as Record<string, unknown>
  const android = (draft.android_proxy || {}) as Record<string, unknown>
  const mgmt = (draft.management || {}) as Record<string, unknown>
  const log = (draft.log || {}) as Record<string, unknown>
  const quality = (draft.quality_check || {}) as Record<string, unknown>
  const subRefresh = (draft.subscription_refresh || {}) as Record<string, unknown>
  const freeSources = Array.isArray(draft.free_proxy_sources) ? draft.free_proxy_sources as FreeProxySource[] : []
  const freeFilter = (draft.free_proxy_filter || {}) as FreeProxyFilter
  const freeCache = (draft.free_proxy_cache || {}) as FreeProxyCache
  const freeFilterProbes = freeFilter.probes || {}
  const status = subStatus.data || {}
  const subItems = editableLines(subs)
  const cleanSubItems = splitLines(subs)
  const cfRows = (cfCache.data?.data || []) as CloudflareResult[]
  const repRows = (repCache.data?.data || []) as ReputationResult[]
  const settingsUnavailable = settings.isLoading || (settings.isError && !settings.data)
  const subStatusUnavailable = subStatus.isError && !subStatus.data
  const cfCacheUnavailable = cfCache.isError && !cfCache.data
  const repCacheUnavailable = repCache.isError && !repCache.data
  const freeRefresh = freeProxyRefreshStatus.data
  const freeRefreshCacheNodes = Number(freeRefresh?.cache_node_count || 0)
  const freeRefreshSources = `${Number(freeRefresh?.enabled_sources ?? freeSources.filter(s => s.enabled !== false).length)}/${Number(freeRefresh?.total_sources ?? freeSources.length)}`
  const freeRefreshProbeBudget = Number(freeRefresh?.filter_probe_budget ?? freeFilter.max_probe_candidates ?? 0)
  const freeRefreshAutoReload = freeRefresh?.auto_reload ?? freeCache.auto_reload !== false
  const freeRefreshFilterEnabled = freeRefresh?.filter_enabled ?? freeFilter.enabled === true
  const isPublicAdmin = String(mgmt.listen || '').startsWith('0.0.0.0') && !String(mgmt.password || '').trim()
  const isPublicProxy = isWideOpen(listener.address) || isWideOpen(mp.address) || isWideOpen(android.listen)
  const updateSub = (idx: number, value: string) => setSubs(subItems.map((item, i) => i === idx ? value : item).join('\n'))
  const removeSub = (idx: number) => setSubs(subItems.filter((_, i) => i !== idx).join('\n'))
  const addSub = () => setSubs([...subItems, ''].join('\n'))
  const updateFreeSource = (idx: number, patch: Partial<FreeProxySource>) => setDraft({...draft, free_proxy_sources: freeSources.map((item, i) => i === idx ? {...item, ...patch} : item)})
  const removeFreeSource = (idx: number) => setDraft({...draft, free_proxy_sources: freeSources.filter((_, i) => i !== idx)})
  const addFreeSource = () => setDraft({...draft, free_proxy_sources: [...freeSources, { name: 'new-free-source', enabled: true, url: '', format: 'text', default_scheme: 'http' }]})
  const freeSourceIssues = freeSources.map((src, idx) => ({idx, issue: !String(src.url || src.file || '').trim() ? `#${idx + 1} 请填写 URL 或文件路径` : ''})).filter(item => item.issue)
  const updateFreeFilter = (patch: Partial<FreeProxyFilter>) => setDraft({...draft, free_proxy_filter: {...freeFilter, ...patch}})
  const updateFreeCache = (patch: Partial<FreeProxyCache>) => setDraft({...draft, free_proxy_cache: {...freeCache, ...patch}})
  const saveAllSettings = () => {
    if (freeSourceIssues.length > 0) {
      toast('请先补全免费代理源的 URL 或文件路径', 'error')
      return
    }
    const serverSources = Array.isArray(settings.data?.free_proxy_sources) ? settings.data.free_proxy_sources as FreeProxySource[] : []
    const normalizedDraft = {
      ...draft,
      subscriptions: cleanSubItems,
      free_proxy_sources: normalizeFreeSourcesForSave(freeSources, serverSources),
    }
    save.mutate(normalizedDraft)
  }
  const saveSubscriptions = () => saveSub.mutate({
    subscriptions: cleanSubItems,
    enabled: subRefresh.enabled !== false,
    interval: String(subRefresh.interval || '1h0m0s'),
  })

  return <div className="page settings-page">
    <div className="page-header settings-hero">
      <div><h1>系统设置</h1><p>集中管理订阅来源、代理入口、地区路由、质量检测和日志策略。</p></div>
      <div className="toolbar"><Button variant="primary" onClick={saveAllSettings} disabled={save.isPending || settingsUnavailable}><Save size={16} />{save.isPending ? '保存中...' : '保存设置'}</Button></div>
    </div>
    {settings.isError && <QueryErrorBanner title="设置加载失败" error={settings.error} onRetry={() => { void settings.refetch() }} />}
    {subStatus.isError && <QueryErrorBanner title="订阅状态加载失败" error={subStatus.error} onRetry={() => { void subStatus.refetch() }} />}
    {cfCache.isError && <QueryErrorBanner title="CF 缓存状态加载失败" error={cfCache.error} onRetry={() => { void cfCache.refetch() }} />}
    {repCache.isError && <QueryErrorBanner title="IP 风险缓存状态加载失败" error={repCache.error} onRetry={() => { void repCache.refetch() }} />}
    {reloadStatus.isError && <QueryErrorBanner title="设置生效状态加载失败" error={reloadStatus.error} onRetry={() => { void reloadStatus.refetch() }} />}
    {freeProxyRefreshStatus.isError && <QueryErrorBanner title="免费源刷新状态加载失败" error={freeProxyRefreshStatus.error} onRetry={() => { void freeProxyRefreshStatus.refetch() }} />}
    {(isPublicAdmin || isPublicProxy) && <div className="settings-alert modern-settings-alert" role="alert">
      <AlertCircle size={18} />
      <div>
        <strong>{isPublicAdmin ? '管理端口当前对所有网卡开放且未设置密码' : '代理入口当前对所有网卡开放'}</strong>
        <span>如果只在本机使用，建议把监听地址改为 127.0.0.1；管理端口建议设置密码后再对局域网开放。</span>
      </div>
    </div>}
    {reloadState !== 'idle' && <div className="settings-alert modern-settings-alert settings-reload-alert" role="status">
      <Clock3 size={18} />
      <div>
        <strong>{reloadState === 'reloading' ? '设置已保存，后台正在生效' : '后台生效失败'}</strong>
        <span>{reloadState === 'reloading' ? `页面无需等待；代理核心会在后台完成重载，已运行 ${Math.floor(Number(reloadStatus.data?.elapsed_ms || 0) / 1000)} 秒，完成后自动刷新状态。` : '配置已经保存，但核心重载失败；请检查日志后再次保存或使用 epctl 重启隔离实例。'}</span>
      </div>
    </div>}
    {freeProxyRefreshState !== 'idle' && <div className="settings-alert modern-settings-alert settings-reload-alert" role="status">
      <Clock3 size={18} />
      <div>
        <strong>{freeProxyRefreshTitle(freeProxyRefreshState, freeRefresh)}</strong>
        <span>{freeProxyRefreshDescription(freeProxyRefreshState, freeRefresh)}</span>
        <span>缓存：{freeRefresh?.cache_path || String(freeCache.path || '未配置')} · {freeRefreshCacheNodes} 条 · {cacheFreshLabel(freeRefresh?.cache_fresh)} · 最大年龄 {freeRefresh?.cache_max_age || String(freeCache.max_age || '6h0m0s')}</span>
        <span>策略：启用源 {freeRefreshSources} · 自动重载 {freeRefreshAutoReload ? '开' : '关'} · 筛选 {freeRefreshFilterEnabled ? '开' : '关'} · 最低等级 {freeRefresh?.filter_min_tier || String(freeFilter.min_tier || 'http_basic')} · 探测预算 {freeRefreshProbeBudget > 0 ? freeRefreshProbeBudget : '全量'}</span>
        {freeRefresh?.sources?.length ? <span>
          源结果：{freeRefresh.sources.map(src => `${src.name || 'unnamed'} ${src.accepted || 0}/${src.candidates || 0}${src.error ? ` 失败: ${src.error}` : ''}`).join('；')}
        </span> : null}
      </div>
    </div>}
    <div className="settings-layout refined-settings-layout">
      <div className="side-nav settings-anchor-nav">
        <a href="#subscriptions"><span className="nav-kicker">01</span><strong>订阅</strong><span>节点来源与刷新</span></a>
        <a href="#free-proxy"><span className="nav-kicker">02</span><strong>免费代理源</strong><span>自动筛选与入池</span></a>
        <a href="#pool"><span className="nav-kicker">03</span><strong>默认代理池</strong><span>监听地址和认证</span></a>
        <a href="#multi-port"><span className="nav-kicker">04</span><strong>多端口</strong><span>批量端口出口</span></a>
        <a href="#routing"><span className="nav-kicker">05</span><strong>地区 / Android</strong><span>GeoIP 与移动端代理</span></a>
        <a href="#quality-check"><span className="nav-kicker">06</span><strong>质量检测</strong><span>定时刷新评分缓存</span></a>
        <a href="#management"><span className="nav-kicker">07</span><strong>管理与日志</strong><span>控制面与日志策略</span></a>
      </div>
      <div className="settings-stack refined-settings-stack">
        <section className="card settings-section settings-section-featured" id="subscriptions">
          <div className="panel-header settings-section-header"><div><div className="panel-title">订阅</div><div className="panel-subtitle">每条订阅独立编辑，长 URL 不再挤在一个文本框里。</div></div><div className="toolbar"><Button onClick={addSub}><Plus size={16} />新增订阅</Button><Button variant="primary" onClick={saveSubscriptions} disabled={saveSub.isPending}><Save size={16} />{saveSub.isPending ? '保存中...' : '保存订阅'}</Button></div></div>
          <div className="settings-status-grid">
            <div className="status-card"><Database size={16} /><span>订阅条目</span><strong>{cleanSubItems.length}</strong></div>
            <div className="status-card"><Wifi size={16} /><span>刷新状态</span><strong>{subStatusUnavailable ? '加载失败' : boolValue(status.is_refreshing) ? '刷新中' : '空闲'}</strong></div>
            <div className="status-card"><Clock3 size={16} /><span>下次刷新</span><strong>{subStatusUnavailable ? '-' : shortDate(status.next_refresh)}</strong></div>
            <div className="status-card"><Database size={16} /><span>最近节点数</span><strong>{subStatusUnavailable ? '-' : Number(status.node_count || 0)}</strong></div>
          </div>
          <div className="subscription-controls">
            {toggle('启用订阅刷新', subRefresh.enabled !== false, v=>setDraft({...draft, subscription_refresh:{...subRefresh,enabled:v}}))}
            {input('刷新间隔', String(subRefresh.interval || '1h0m0s'), v=>setDraft({...draft, subscription_refresh:{...subRefresh,interval:v}}))}
          </div>
          <div className="subscription-list">
            {subItems.length ? subItems.map((url, idx) => <div className="subscription-item modern-subscription-item" key={`${idx}-${url.slice(0, 16)}`}><div className="subscription-index">#{idx + 1}</div><Input className="settings-input mono subscription-url-input" value={url} title={url} onChange={e=>updateSub(idx, e.target.value)} /><Button variant="danger" onClick={()=>removeSub(idx)}><Trash2 size={15} />删除</Button></div>) : <div className="empty-state compact-empty"><strong>暂无订阅 URL</strong><span>点击“新增订阅”添加一条订阅地址。</span></div>}
          </div>
          <details className="raw-editor"><summary>批量编辑原始文本</summary><label htmlFor="subscriptions-raw-editor">批量编辑订阅原始文本</label><Input.TextArea id="subscriptions-raw-editor" className="settings-input mono subscription-textarea" rows={4} value={subs} onChange={e=>setSubs(e.target.value)} /></details>
          <div className="settings-inline-note"><Badge tone={boolValue(status.enabled) ? 'good' : 'neutral'}>{boolValue(status.enabled) ? '已启用' : '未启用'}</Badge><span>上次刷新：{shortDate(status.last_refresh)}</span><span>刷新次数：{Number(status.refresh_count || 0)}</span>{String(status.last_error || '') && <span className="danger-text">错误：{String(status.last_error)}</span>}</div>
        </section>

        <section className="card settings-section" id="free-proxy">
          <div className="panel-header settings-section-header">
            <div><div className="panel-title">免费代理源</div><div className="panel-subtitle">把公开代理列表作为候选源。保存并重载后，系统会先抓取、去重和预筛，只让通过最低等级的代理进入运行节点。</div></div>
            <div className="toolbar">
              <Button onClick={()=>manualFreeRefresh.mutate()} disabled={manualFreeRefresh.isPending || freeProxyRefreshState === 'refreshing'}><Wifi size={16} />{manualFreeRefresh.isPending ? '启动中...' : '手动刷新'}</Button>
              <Button onClick={addFreeSource}><Plus size={16} />新增源</Button>
            </div>
          </div>
          <div className="settings-status-grid">
            <div className="status-card"><Database size={16} /><span>源数量</span><strong>{freeSources.length}</strong></div>
            <div className="status-card"><Wifi size={16} /><span>启用源</span><strong>{freeSources.filter(s => s.enabled !== false).length}</strong></div>
            <div className="status-card"><Database size={16} /><span>入池上限</span><strong>{Number(draft.free_proxy_max_nodes || 0) > 0 ? Number(draft.free_proxy_max_nodes) : '不限'}</strong></div>
            <div className="status-card"><Clock3 size={16} /><span>最低等级</span><strong>{String(freeFilter.min_tier || 'http_basic')}</strong></div>
            <div className="status-card"><Database size={16} /><span>本地缓存</span><strong>{freeRefresh ? `${freeRefreshCacheNodes} 条` : '未读取'}</strong></div>
            <div className="status-card"><Clock3 size={16} /><span>缓存状态</span><strong>{freeRefresh ? cacheFreshLabel(freeRefresh.cache_fresh) : '待刷新'}</strong></div>
          </div>
          <div className="free-proxy-filter-panel">
            <div className="quality-toggle-row">
              {toggle('启用自动筛选', freeFilter.enabled === true, v=>updateFreeFilter({enabled:v}))}
              <span className="settings-helper-text">开启后，免费源不会直接进入运行池；只有通过预筛的代理会展示。</span>
            </div>
          <div className="form-grid-3 compact-form-grid">
            <div className="field settings-form-item"><label>最低等级</label><Select className="settings-input" value={String(freeFilter.min_tier || 'http_basic')} onChange={v=>updateFreeFilter({min_tier:v})} options={[{value:'http_basic',label:'HTTP 基础可用'}, {value:'simple_web',label:'普通 Web 可用'}]} /></div>
            {input('入池上限（0=不限）', String(draft.free_proxy_max_nodes || 0), v=>setDraft({...draft, free_proxy_max_nodes:Number(v)||0}), 'number')}
            {input('解析上限（0=全量）', String(freeFilter.max_candidates || 0), v=>updateFreeFilter({max_candidates:Number(v)||0}), 'number')}
            {input('探测预算（0=全量）', String(freeFilter.max_probe_candidates || 0), v=>updateFreeFilter({max_probe_candidates:Number(v)||0}), 'number')}
              {input('筛选并发', String(freeFilter.workers || 200), v=>updateFreeFilter({workers:Number(v)||200}), 'number')}
            {input('筛选超时', String(freeFilter.timeout || '2s'), v=>updateFreeFilter({timeout:v}))}
          </div>
          <details className="raw-editor free-proxy-advanced"><summary>缓存与高级探针配置</summary>
            <div className="quality-toggle-row free-proxy-cache-row">
              {toggle('启动仅读取缓存', freeCache.enabled !== false, v=>updateFreeCache({enabled:v}))}
              {toggle('启动后后台刷新', freeCache.refresh_on_start !== false, v=>updateFreeCache({refresh_on_start:v}))}
              {toggle('刷新后自动重载', freeCache.auto_reload !== false, v=>updateFreeCache({auto_reload:v}))}
            </div>
            <div className="form-grid-3 compact-form-grid free-proxy-probes">
              {input('缓存文件', String(freeCache.path || ''), v=>updateFreeCache({path:v}))}
              {input('源下载并发', String(freeCache.workers || 8), v=>updateFreeCache({workers:Number(v)||8}), 'number')}
              {input('缓存最大年龄', String(freeCache.max_age || '6h0m0s'), v=>updateFreeCache({max_age:v}))}
              {input('HTTP 探针', String(freeFilterProbes.http || 'http://cp.cloudflare.com/generate_204'), v=>updateFreeFilter({probes:{...freeFilterProbes,http:v}}))}
              {input('HTTPS 探针', String(freeFilterProbes.https || 'https://example.com/'), v=>updateFreeFilter({probes:{...freeFilterProbes,https:v}}))}
            </div>
            <div className="settings-helper-text">推荐保持“启动仅读取缓存”：WebUI 先启动，免费源在后台下载和筛选，完成后自动重载。</div>
          </details>
          </div>
          {freeSourceIssues.length > 0 && <div className="settings-inline-note free-proxy-warning"><AlertCircle size={15} /><span>{freeSourceIssues.map(item => item.issue).join('；')}</span></div>}
          <div className="subscription-list free-source-list">
            {freeSources.length ? freeSources.map((src, idx) => <div className="subscription-item modern-subscription-item free-source-row" key={`${idx}-${String(src.name || src.url || src.file).slice(0, 16)}`}>
              <div className="free-source-enable"><label>启用</label><Checkbox checked={src.enabled !== false} onChange={e=>updateFreeSource(idx,{enabled:e.target.checked})} /></div>
              <div className="field settings-form-item"><label>名称</label><Input className="settings-input" placeholder="new-free-source" value={src.name || ''} onChange={e=>updateFreeSource(idx,{name:e.target.value})} /></div>
              <div className="field settings-form-item"><label>URL / 文件路径</label><Input className="settings-input mono" placeholder="https://raw.githubusercontent.com/..." value={src.url || src.file || ''} onChange={e=>updateFreeSource(idx, e.target.value.startsWith('http') ? {url:e.target.value,file:''} : {file:e.target.value,url:''})} /></div>
              <div className="field settings-form-item"><label>协议</label><Select className="settings-input" value={src.default_scheme || 'http'} onChange={v=>updateFreeSource(idx,{default_scheme:v})} options={[{value:'http',label:'http'}, {value:'socks5',label:'socks5'}]} /></div>
              <div className="field settings-form-item"><label>单源上限</label><Input className="settings-input" type="number" title="0 表示全量解析该源" value={String(src.max_nodes ?? 0)} onChange={e=>updateFreeSource(idx,{max_nodes:Number(e.target.value)||0})} /></div>
              <Button variant="danger" onClick={()=>removeFreeSource(idx)}><Trash2 size={15} />删除</Button>
            </div>) : <div className="empty-state compact-empty"><strong>暂无免费代理源</strong><span>添加 GitHub raw、远程文本列表或本地文件。保存并重载后，系统会自动筛选，通过后才进入节点总览。</span><Button onClick={addFreeSource}><Plus size={15} />新增源</Button></div>}
          </div>
          <div className="settings-inline-note"><Badge tone={freeFilter.enabled ? 'good' : 'neutral'}>{freeFilter.enabled ? '自动筛选已启用' : '未启用自动筛选'}</Badge><span>建议先使用 HTTP 基础可用入池，再通过 CF/IP 风险检测筛质量；解析上限/入池上限填 0 表示不截断，探测预算填 0 表示全量探测。</span></div>
        </section>


        <section className="settings-card-grid">
          <div className="card settings-section" id="pool"><div className="panel-header"><div><div className="panel-title">默认代理池</div><div className="panel-subtitle">常规入口和基础认证。</div></div></div><form className="form-grid-2 compact-form-grid" onSubmit={e=>e.preventDefault()}>{input('监听地址', String(listener.address || ''), v=>setDraft({...draft, listener:{...listener,address:v}}))}{input('端口', String(listener.port || ''), v=>setDraft({...draft, listener:{...listener,port:Number(v)||0}}), 'number')}{input('用户名', String(listener.username||''), v=>setDraft({...draft, listener:{...listener,username:v}}))}{input('密码', String(listener.password||''), v=>setDraft({...draft, listener:{...listener,password:v}}), 'password')}</form></div>
          <div className="card settings-section" id="multi-port"><div className="panel-header"><div><div className="panel-title">多端口</div><div className="panel-subtitle">批量分配独立出口。</div></div></div><form className="form-grid-2 compact-form-grid" onSubmit={e=>e.preventDefault()}>{input('监听地址', String(mp.address || ''), v=>setDraft({...draft, multi_port:{...mp,address:v}}))}{input('起始端口', String(mp.base_port || ''), v=>setDraft({...draft, multi_port:{...mp,base_port:Number(v)||0}}), 'number')}{input('用户名', String(mp.username||''), v=>setDraft({...draft, multi_port:{...mp,username:v}}))}{input('密码', String(mp.password||''), v=>setDraft({...draft, multi_port:{...mp,password:v}}), 'password')}</form></div>
        </section>

        <section className="settings-card-grid">
          <div className="card settings-section" id="routing"><div className="panel-header"><div><div className="panel-title">地区路由 / Android</div><div className="panel-subtitle">GeoIP 路由和移动端代理端口。</div></div></div><div className="form-grid-2 compact-form-grid">{input('GeoIP listen', String(geo.listen || ''), v=>setDraft({...draft, geoip:{...geo,listen:v}}))}{input('GeoIP port', String(geo.port || ''), v=>setDraft({...draft, geoip:{...geo,port:Number(v)||0}}))}{input('Android listen', String(android.listen || ''), v=>setDraft({...draft, android_proxy:{...android,listen:v}}))}{input('Android base port', String(android.base_port || ''), v=>setDraft({...draft, android_proxy:{...android,base_port:Number(v)||0}}))}</div></div>
          <div className="card settings-section" id="quality-check"><div className="panel-header"><div><div className="panel-title">质量检测定时任务</div><div className="panel-subtitle">自动刷新 CF 评分和 IP 风险缓存。</div></div><div className="toolbar"><Button onClick={() => { void cfCache.refetch(); void repCache.refetch() }}>刷新缓存状态</Button></div></div><div className="settings-status-grid quality-cache-grid"><div className="status-card"><Database size={16} /><span>CF 缓存</span><strong>{cfCacheUnavailable ? '加载失败' : cfRows.length}</strong></div><div className="status-card"><Database size={16} /><span>IP 风险缓存</span><strong>{repCacheUnavailable ? '加载失败' : repRows.length}</strong></div><div className="status-card"><Clock3 size={16} /><span>CF 最近检测</span><strong>{cfCacheUnavailable ? '-' : latestCheckedAt(cfRows)}</strong></div><div className="status-card"><Clock3 size={16} /><span>IP 最近检测</span><strong>{repCacheUnavailable ? '-' : latestCheckedAt(repRows)}</strong></div></div><div className="quality-toggle-row">{toggle('启用定时检测', boolValue(quality.enabled), v=>setDraft({...draft, quality_check:{...quality,enabled:v}}))}{toggle('包含不可用节点', quality.include_unavailable !== false, v=>setDraft({...draft, quality_check:{...quality,include_unavailable:v}}))}{toggle('优先重试失败节点', boolValue(quality.retry_failed), v=>setDraft({...draft, quality_check:{...quality,retry_failed:v}}))}</div><div className="form-grid-3 compact-form-grid">{input('检测间隔', String(quality.interval || '24h'), v=>setDraft({...draft, quality_check:{...quality,interval:v}}))}{input('地区范围', String(quality.region || 'all'), v=>setDraft({...draft, quality_check:{...quality,region:v}}))}{input('检测数量（1-500）', String(quality.count || 500), v=>setDraft({...draft, quality_check:{...quality,count:clampNumber(v, 500, 1, 500)}}), 'number')}{input('CF 超时', String(quality.cloudflare_timeout || '5s'), v=>setDraft({...draft, quality_check:{...quality,cloudflare_timeout:v}}))}{input('CF 并发', String(quality.cloudflare_concurrency || 24), v=>setDraft({...draft, quality_check:{...quality,cloudflare_concurrency:Number(v)||24}}), 'number')}</div><div className="settings-inline-note">建议间隔不要低于 1 小时；检测数量上限为 500；CF 超时越短扫描越快但失败率可能升高，并发建议 24-32。</div></div>
        </section>

        <section className="card settings-section" id="management"><div className="panel-header"><div><div className="panel-title">管理与日志</div><div className="panel-subtitle">控制面入口和日志滚动策略。</div></div></div><div className="form-grid-2 compact-form-grid">{input('管理 listen', String(mgmt.listen || ''), v=>setDraft({...draft, management:{...mgmt,listen:v}}))}{input('管理 password', String(mgmt.password||''), v=>setDraft({...draft, management:{...mgmt,password:v}}))}{input('日志 output', String(log.output || ''), v=>setDraft({...draft, log:{...log,output:v}}))}{input('日志 max size', String(log.max_size || ''), v=>setDraft({...draft, log:{...log,max_size:Number(v)||0}}))}</div></section>
      </div>
    </div>
  </div>
}
