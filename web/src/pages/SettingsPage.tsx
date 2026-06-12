import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Checkbox, Input, Select } from 'antd'
import { AlertCircle, Clock3, Database, Plus, Save, Trash2, Wifi } from 'lucide-react'
import { getFreeProxyRefreshStatus, getReloadStatus, getSettings, saveSettings, getSubscriptionStatus, saveSubscriptionConfig, startFreeProxyRefresh } from '../api/settings'
import { getCloudflareCache } from '../api/cloudflare'
import { getReputationCache } from '../api/reputation'
import { Button } from '../components/ui/Button'
import { QueryErrorBanner } from '../components/ui/QueryErrorBanner'
import { Badge } from '../components/ui/Badge'
import { useToast } from '../components/ui/Toast'
import { buildSettingsSavePayload, freeSourceDefaultScheme, isSettingsDraftDirty, isSubscriptionDraftDirty } from './settingsSavePayload'
import type { FreeProxyCache, FreeProxyFilter, FreeProxyRefreshStatus, FreeProxySource, SettingsResponse } from '../types/settings'
import type { CloudflareResult } from '../types/cloudflare'
import type { ReputationResult } from '../types/reputation'

type FreeProxyRefreshSource = NonNullable<FreeProxyRefreshStatus['sources']>[number]

type SettingsSectionId = 'subscriptions' | 'free-proxy' | 'pool' | 'multi-port' | 'routing' | 'quality-check' | 'management'

const SETTINGS_SECTIONS: Array<{ id: SettingsSectionId; index: string; title: string; subtitle: string }> = [
  { id: 'subscriptions', index: '01', title: '订阅', subtitle: '节点来源与刷新' },
  { id: 'free-proxy', index: '02', title: '免费代理源', subtitle: '自动筛选与入池' },
  { id: 'pool', index: '03', title: '默认代理池', subtitle: '监听地址和认证' },
  { id: 'multi-port', index: '04', title: '多端口', subtitle: '批量端口出口' },
  { id: 'routing', index: '05', title: '地区 / Android', subtitle: 'GeoIP 与移动端代理' },
  { id: 'quality-check', index: '06', title: '质量检测', subtitle: '定时刷新评分缓存' },
  { id: 'management', index: '07', title: '管理与日志', subtitle: '控制面与日志策略' },
]

function isSettingsSectionId(value: string): value is SettingsSectionId {
  return SETTINGS_SECTIONS.some(section => section.id === value)
}

function settingsSectionFromHash(hash: string): SettingsSectionId | '' {
  const id = hash.replace(/^#/, '')
  if (id === 'settings' || id === '') return 'subscriptions'
  return isSettingsSectionId(id) ? id : ''
}

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
function safeRows<T>(rows: unknown): T[] { return Array.isArray(rows) ? rows : [] }

function freeSourceTargetPatch(value: string): Pick<FreeProxySource, 'url' | 'file'> {
  const text = value.trim()
  if (/^https?:\/\//i.test(text)) return { url: text, file: '' }
  return { file: text, url: '' }
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
  if (status?.refresh_pending) return '免费源扫描已排队'
  if (status?.state === 'succeeded' && status.reload_started) return '免费源缓存已更新，正在重载入池'
  if (status?.state === 'succeeded' && status.cache_updated === false) return status.cache_fresh ? '免费源缓存仍新鲜，已复用本地缓存' : '免费源未产生新缓存，已复用旧缓存'
  return '免费源正在后台扫描'
}
function freeProxyRefreshDescription(state: 'idle' | 'refreshing' | 'failed', status?: FreeProxyRefreshStatus) {
  if (state === 'failed') return '免费源刷新未产生可入池节点，系统已保留现有缓存且不会自动重载；请检查源地址、探针或降低筛选等级。'
  if (status?.refresh_pending) return '已有免费源扫描在运行，新配置刷新已排队；当前扫描完成后会自动继续下载、筛选并写入最新缓存。'
  if (status?.state === 'succeeded' && status.reload_started) return '候选代理已写入本地缓存；代理核心正在后台重载，完成后新节点才会出现在节点列表和质量检测中。'
  if (status?.state === 'succeeded' && status.cache_updated === false) {
    return status.cache_fresh ? '本地免费源缓存未过期，本次跳过远程下载和筛选，也不需要重载代理核心。' : '远程刷新没有产生可替换缓存，系统保留并复用旧缓存，避免清空当前节点池。'
  }
  return '系统正在下载、去重、预筛并写入缓存；完成后会按配置自动重载。'
}

function isSafeManagementRedirectTarget(target: URL) {
  const isHttp = target.protocol === 'http:' || target.protocol === 'https:'
  const isSameHost = target.hostname === window.location.hostname
  const isLocalHost = ['127.0.0.1', 'localhost', '::1'].includes(target.hostname)
  return isHttp && (isSameHost || isLocalHost)
}

function buildManagementRedirectUrl(hint: string, needReload?: boolean) {
  try {
    const target = new URL(hint, window.location.href)
    if (!isSafeManagementRedirectTarget(target)) return ''
    if (needReload) target.searchParams.set('autoReload', '1')
    else target.searchParams.delete('autoReload')
    target.hash = 'settings'
    return target.href
  } catch {
    return ''
  }
}

export function SettingsPage() {
  const queryClient = useQueryClient()
  const settings = useQuery({ queryKey:['settings'], queryFn:getSettings })
  const cfCache = useQuery({ queryKey:['cf-cache'], queryFn:getCloudflareCache })
  const repCache = useQuery({ queryKey:['rep-cache'], queryFn:getReputationCache })
  const [draft, setDraft] = useState<SettingsResponse>({})
  const [activeSettingsSection, setActiveSettingsSection] = useState<SettingsSectionId>(() => {
    if (typeof window === 'undefined') return 'subscriptions'
    return settingsSectionFromHash(window.location.hash) || 'subscriptions'
  })
  const [subs, setSubs] = useState('')
  const [settingsDirty, setSettingsDirty] = useState(false)
  const [subsDirty, setSubsDirty] = useState(false)
  const [managementPasswordDraft, setManagementPasswordDraft] = useState('')
  const [managementPasswordClear, setManagementPasswordClear] = useState(false)
  const [reloadState, setReloadState] = useState<'idle' | 'reloading' | 'failed'>('idle')
  const [subscriptionRefreshState, setSubscriptionRefreshState] = useState<'idle' | 'refreshing'>('idle')
  const [subscriptionRefreshObservedRunning, setSubscriptionRefreshObservedRunning] = useState(false)
  const [freeProxyRefreshState, setFreeProxyRefreshState] = useState<'idle' | 'refreshing' | 'failed'>('idle')
  const reloadStatus = useQuery({ queryKey:['reload-status'], queryFn:getReloadStatus, enabled: reloadState === 'reloading', refetchInterval: reloadState === 'reloading' ? 800 : false })
  const subStatus = useQuery({ queryKey:['sub-status'], queryFn:getSubscriptionStatus, refetchInterval: subscriptionRefreshState === 'refreshing' ? 800 : false })
  const freeProxyRefreshStatus = useQuery({ queryKey:['free-proxy-refresh-status'], queryFn:getFreeProxyRefreshStatus, refetchInterval: freeProxyRefreshState === 'refreshing' ? 800 : false })
  const toast = useToast(s=>s.show)
  const refreshRuntimeNodeCaches = () => {
    void queryClient.invalidateQueries({ queryKey:['nodes-page'] })
    void queryClient.invalidateQueries({ queryKey:['nodes-summary'] })
    void queryClient.invalidateQueries({ queryKey:['nodes'] })
    void queryClient.invalidateQueries({ queryKey:['status-nodes-all'] })
  }
  const refreshSettingsCache = () => {
    void queryClient.invalidateQueries({ queryKey:['settings'] })
  }
  useEffect(()=>{
    if (!settings.data) return
    if (!settingsDirty) {
      setDraft(settings.data)
      setManagementPasswordDraft('')
      setManagementPasswordClear(false)
    }
    if (!subsDirty) setSubs(listValue(settings.data.subscriptions))
  }, [settings.data, settingsDirty, subsDirty])
  useEffect(() => {
    if (subscriptionRefreshState !== 'refreshing') return
    if (subStatus.data?.is_refreshing === true) {
      setSubscriptionRefreshObservedRunning(true)
      return
    }
    if (!subscriptionRefreshObservedRunning) return
    setSubscriptionRefreshState('idle')
    setSubscriptionRefreshObservedRunning(false)
    if (subStatus.data?.nodes_modified) refreshRuntimeNodeCaches()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subscriptionRefreshState, subscriptionRefreshObservedRunning, subStatus.data?.is_refreshing, subStatus.data?.nodes_modified])
  useEffect(() => {
    const syncHashSection = () => {
      const id = settingsSectionFromHash(window.location.hash)
      if (!id) return
      setActiveSettingsSection(id)
      window.setTimeout(() => document.getElementById(id)?.scrollIntoView({ block: 'start' }), 0)
    }
    syncHashSection()
    window.addEventListener('hashchange', syncHashSection)
    return () => window.removeEventListener('hashchange', syncHashSection)
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
      refreshSettingsCache()
      refreshRuntimeNodeCaches()
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
      refreshSettingsCache()
      refreshRuntimeNodeCaches()
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
    setSettingsDirty(false)
    setSubsDirty(false)
    if (res?.management_rebound && res.management_url_hint) {
      const target = buildManagementRedirectUrl(res.management_url_hint, res?.need_reload)
      if (!target) {
        toast('管理端口已热切换，但后端返回的跳转地址无效；请手动刷新管理页面。', 'error')
        return
      }
      toast('管理端口已热切换，正在跳转到新地址...', 'ok')
      window.setTimeout(() => { window.location.href = target }, 300)
      return
    }
    refreshSettingsCache()
    if (res?.subscription_refresh_started) {
      setSubscriptionRefreshState('refreshing')
      setSubscriptionRefreshObservedRunning(false)
      void subStatus.refetch()
    }
    if (res?.free_proxy_refresh_needed) {
      if (res.free_proxy_refresh_error) {
        setFreeProxyRefreshState('failed')
        toast(`设置已保存，但免费源扫描启动失败：${res.free_proxy_refresh_error}`, 'error')
        return
      }
      const prefix = res?.subscription_refresh_started ? '设置已保存，订阅后台刷新已启动；' : '设置已保存，'
      const refreshPending = res.free_proxy_refresh_status?.refresh_pending || res.free_proxy_refresh_pending
      toast(res.free_proxy_refresh_started ? `${prefix}免费源开始后台扫描...` : refreshPending ? `${prefix}已有免费源扫描在运行，新配置刷新已排队...` : `${prefix}已有免费源扫描在运行...`, 'ok')
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
      const prefix = res?.subscription_refresh_started ? '设置已保存，订阅后台刷新已启动；' : '设置已保存，'
      toast(res.reload_status?.reload_pending ? `${prefix}已排队等待当前后台生效完成后继续生效...` : res.reload_started ? `${prefix}后台正在生效...` : `${prefix}已有后台生效任务在运行...`, 'ok')
      setReloadState('reloading')
      void reloadStatus.refetch()
    } else {
      toast(res?.subscription_refresh_started ? '设置已保存，订阅后台刷新已启动' : '设置已保存，已立即生效', 'ok')
      setReloadState('idle')
    }
  }, onError:e=>toast(e instanceof Error ? e.message:'保存失败','error') })
  const manualFreeRefresh = useMutation({ mutationFn: startFreeProxyRefresh, onSuccess:(res)=>{
    const statusState = String(res.status?.state || '')
    const started = !!res.started
    const message = String(res.message || (started ? '免费源已开始后台刷新...' : '免费源刷新未启动'))
    toast(message, statusState === 'failed' ? 'error' : 'ok')
    setFreeProxyRefreshState(started || statusState === 'running' ? 'refreshing' : 'idle')
    void freeProxyRefreshStatus.refetch()
  }, onError:e=>toast(e instanceof Error ? e.message:'免费源刷新启动失败','error') })
  const saveSub = useMutation({ mutationFn: saveSubscriptionConfig, onSuccess:(res)=>{ const changed = res.config_changed !== false; const refreshed = !!res.refresh_triggered; setSubsDirty(false); toast(refreshed ? '订阅配置已保存，后台刷新已启动' : changed ? '订阅配置已保存，调度已更新' : '订阅配置未变化，已保持当前状态', 'ok'); setReloadState('idle'); if (refreshed) { setSubscriptionRefreshState('refreshing'); setSubscriptionRefreshObservedRunning(false) }; refreshSettingsCache(); void subStatus.refetch() }, onError:e=>toast(e instanceof Error ? e.message:'订阅保存失败','error') })
  const updateDirtyState = (nextDraft: SettingsResponse, passwordDraft = managementPasswordDraft, clearPassword = managementPasswordClear) => {
    setSettingsDirty(isSettingsDraftDirty(nextDraft, settings.data) || Boolean(passwordDraft.trim()) || clearPassword)
  }
  const updateDraft = (next: SettingsResponse) => {
    updateDirtyState(next)
    setDraft(next)
  }
  const updateManagementPasswordDraft = (value: string) => {
    updateDirtyState(draft, value, false)
    setManagementPasswordClear(false)
    setManagementPasswordDraft(value)
  }
  const updateManagementPasswordClear = (value: boolean) => {
    const nextPasswordDraft = value ? '' : managementPasswordDraft
    updateDirtyState(draft, nextPasswordDraft, value)
    setManagementPasswordClear(value)
    if (value) setManagementPasswordDraft('')
  }
  const updateSubsDraft = (next: string) => {
    setSubs(next)
    setSubsDirty(isSubscriptionDraftDirty(next, settings.data?.subscriptions))
  }
  const input = (label:string, value:string, onChange:(v:string)=>void, type='text') => <div className="field settings-form-item"><label>{label}</label><Input className="settings-input" aria-label={label} type={type} autoComplete={type === 'password' ? label.includes('新管理') ? 'new-password' : 'current-password' : label.includes('用户名') ? 'username' : undefined} value={value} onChange={e=>onChange(e.target.value)} /></div>
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
  const cfRows = safeRows<CloudflareResult>(cfCache.data?.data)
  const repRows = safeRows<ReputationResult>(repCache.data?.data)
  const settingsUnavailable = settings.isLoading || (settings.isError && !settings.data)
  const subStatusUnavailable = subStatus.isError && !subStatus.data
  const cfCacheUnavailable = cfCache.isError && !cfCache.data
  const repCacheUnavailable = repCache.isError && !repCache.data
  const freeRefresh = freeProxyRefreshStatus.data
  const freeRefreshSourceRows = safeRows<FreeProxyRefreshSource>(freeRefresh?.sources)
  const freeSourcesEnabledCount = freeSources.filter(src => src.enabled !== false).length
  const freeProxyHasNoEnabledSources = freeSources.length > 0 && freeSourcesEnabledCount === 0
  const hasUnsavedChanges = settingsDirty || subsDirty
  const freeProxyRefreshNeedsSavedDraft = settingsDirty
  const freeRefreshCacheNodes = Number(freeRefresh?.cache_node_count || 0)
  const freeRefreshSources = `${Number(freeRefresh?.enabled_sources ?? freeSourcesEnabledCount)}/${Number(freeRefresh?.total_sources ?? freeSources.length)}`
  const freeRefreshProbeBudget = Number(freeRefresh?.filter_probe_budget ?? freeFilter.max_probe_candidates ?? 0)
  const freeRefreshAutoReload = freeRefresh?.auto_reload ?? freeCache.auto_reload !== false
  const freeRefreshFilterEnabled = freeRefresh?.filter_enabled ?? freeFilter.enabled === true
  const managementPasswordEffective = managementPasswordClear ? false : Boolean(managementPasswordDraft.trim() || mgmt.password_set)
  const isPublicProxy = isWideOpen(listener.address) || isWideOpen(mp.address) || isWideOpen(android.listen)
  const updateSub = (idx: number, value: string) => updateSubsDraft(subItems.map((item, i) => i === idx ? value : item).join('\n'))
  const removeSub = (idx: number) => updateSubsDraft(subItems.filter((_, i) => i !== idx).join('\n'))
  const addSub = () => updateSubsDraft([...subItems, ''].join('\n'))
  const updateFreeSource = (idx: number, patch: Partial<FreeProxySource>) => updateDraft({...draft, free_proxy_sources: freeSources.map((item, i) => i === idx ? {...item, ...patch} : item)})
  const removeFreeSource = (idx: number) => updateDraft({...draft, free_proxy_sources: freeSources.filter((_, i) => i !== idx)})
  const addFreeSource = () => updateDraft({...draft, free_proxy_sources: [...freeSources, { name: 'new-free-source', enabled: true, url: '', format: 'text', default_scheme: 'http' }]})
  const enableAllFreeSources = () => updateDraft({...draft, free_proxy_sources: freeSources.map(item => ({...item, enabled: true}))})
  const disableAllFreeSources = () => updateDraft({...draft, free_proxy_sources: freeSources.map(item => ({...item, enabled: false}))})
  const resetFreeSourcesToDefaultEnabled = () => updateDraft({...draft, free_proxy_sources: freeSources.map(item => {
    const next = {...item}
    delete next.enabled
    return next
  })})
  const resetDrafts = () => {
    if (!settings.data) return
    setDraft(settings.data)
    setSubs(listValue(settings.data.subscriptions))
    setManagementPasswordDraft('')
    setManagementPasswordClear(false)
    setSettingsDirty(false)
    setSubsDirty(false)
  }
  const freeSourceIssues = freeSources.map((src, idx) => ({idx, issue: src.enabled !== false && !String(src.url || src.file || '').trim() ? `#${idx + 1} 请填写 URL 或文件路径，或先关闭启用` : ''})).filter(item => item.issue)
  const activeSectionMeta = SETTINGS_SECTIONS.find(section => section.id === activeSettingsSection) || SETTINGS_SECTIONS[0]
  const sectionClass = (id: SettingsSectionId, extra = '') => `card settings-section ${extra} ${activeSettingsSection === id ? 'settings-section-active' : 'settings-section-hidden'}`.trim()
  const gridClass = (...ids: SettingsSectionId[]) => `settings-card-grid ${ids.includes(activeSettingsSection) ? 'settings-card-grid-active' : 'settings-card-grid-hidden'}`
  const updateFreeFilter = (patch: Partial<FreeProxyFilter>) => updateDraft({...draft, free_proxy_filter: {...freeFilter, ...patch}})
  const updateFreeCache = (patch: Partial<FreeProxyCache>) => updateDraft({...draft, free_proxy_cache: {...freeCache, ...patch}})
  const saveAllSettings = () => {
    if (!settingsDirty) {
      if (subsDirty) saveSubscriptions()
      return
    }
    if (freeSourceIssues.length > 0) {
      toast('请先补全免费代理源的 URL 或文件路径', 'error')
      return
    }
    const normalizedDraft = buildSettingsSavePayload({
      draft,
      serverSettings: settings.data,
      management: mgmt,
      managementPasswordDraft,
      managementPasswordClear,
      subscriptions: cleanSubItems,
    })
    save.mutate(normalizedDraft)
  }
  const saveSubscriptions = () => {
    if (!subsDirty) return
    saveSub.mutate({
      subscriptions: cleanSubItems,
      enabled: subRefresh.enabled !== false,
      interval: String(subRefresh.interval || '1h0m0s'),
    })
  }
  const qualityCacheLoading = cfCache.isFetching || repCache.isFetching
  const refreshQualityCache = async () => {
    if (qualityCacheLoading) return
    try {
      const [cf, rep] = await Promise.all([cfCache.refetch(), repCache.refetch()])
      const failed = [cf.error, rep.error].find(Boolean)
      if (failed) throw failed
      toast('质量缓存状态已刷新', 'ok')
    } catch (error) {
      toast(error instanceof Error ? `质量缓存状态刷新失败：${error.message}` : '质量缓存状态刷新失败', 'error')
    }
  }

  if (!settings.data && (settings.isLoading || settings.isError)) {
    return <div className="page settings-page">
      <div className="page-header settings-hero">
        <div>
          <h1>系统设置</h1>
          <p>{settings.isError ? '当前配置加载失败，修复前不会展示可编辑空表单，避免误覆盖已有设置。' : '正在加载当前配置，加载完成前不会展示可编辑空表单，避免误覆盖已有设置。'}</p>
        </div>
        <div className="toolbar"><Button variant="primary" disabled><Save size={16} />{settings.isError ? '不可保存' : '加载中...'}</Button></div>
      </div>
      {settings.isError
        ? <QueryErrorBanner title="设置加载失败" error={settings.error} onRetry={() => { void settings.refetch() }} />
        : <div className="card settings-section" role="status">
          <div className="panel-title">正在读取设置</div>
          <div className="panel-subtitle">请稍候，系统正在从后端加载订阅、免费源、端口和质量检测配置。</div>
        </div>}
    </div>
  }

  return <div className="page settings-page">
    <div className="page-header settings-hero">
      <div><h1>系统设置</h1><p>集中管理订阅来源、代理入口、地区路由、质量检测和日志策略。</p></div>
      <div className="toolbar"><Button variant="primary" onClick={saveAllSettings} disabled={save.isPending || saveSub.isPending || settingsUnavailable || !hasUnsavedChanges}><Save size={16} />{save.isPending || saveSub.isPending ? '保存中...' : '保存更改'}</Button></div>
    </div>
    {settings.isError && <QueryErrorBanner title="设置加载失败" error={settings.error} onRetry={() => { void settings.refetch() }} />}
    {subStatus.isError && <QueryErrorBanner title="订阅状态加载失败" error={subStatus.error} onRetry={() => { void subStatus.refetch() }} />}
    {cfCache.isError && <QueryErrorBanner title="CF 缓存状态加载失败" error={cfCache.error} onRetry={() => { void cfCache.refetch() }} />}
    {repCache.isError && <QueryErrorBanner title="IP 风险缓存状态加载失败" error={repCache.error} onRetry={() => { void repCache.refetch() }} />}
    {reloadStatus.isError && <QueryErrorBanner title="设置生效状态加载失败" error={reloadStatus.error} onRetry={() => { void reloadStatus.refetch() }} />}
    {freeProxyRefreshStatus.isError && <QueryErrorBanner title="免费源刷新状态加载失败" error={freeProxyRefreshStatus.error} onRetry={() => { void freeProxyRefreshStatus.refetch() }} />}
    {hasUnsavedChanges && <div className="settings-inline-note" role="status"><Badge tone="warn">有未保存更改</Badge><span>{settingsDirty && subsDirty ? '设置和订阅都有草稿；点击保存更改会一起保存。' : settingsDirty ? '设置草稿尚未保存；涉及免费源时请先保存再刷新。' : '订阅列表尚未保存；点击保存更改或保存订阅后生效。'}后台状态刷新不会覆盖当前表单草稿。</span></div>}
    {((String(mgmt.listen || '').startsWith('0.0.0.0') && !managementPasswordEffective) || isPublicProxy) && <div className="settings-alert modern-settings-alert" role="alert">
      <AlertCircle size={18} />
      <div>
        <strong>{String(mgmt.listen || '').startsWith('0.0.0.0') && !managementPasswordEffective ? '管理端口当前对所有网卡开放且未设置密码' : '代理入口当前对所有网卡开放'}</strong>
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
        {freeRefresh?.refresh_pending ? <span>排队：新配置刷新已排队 · 来源 {freeRefresh.pending_requested_by || 'unknown'}</span> : null}
        {freeRefreshSourceRows.length ? <span>
          源结果：{freeRefreshSourceRows.map(src => `${src.name || 'unnamed'} ${src.accepted || 0}/${src.candidates || 0}${src.error ? ` 失败: ${src.error}` : ''}`).join('；')}
        </span> : null}
      </div>
    </div>}
    <div className="settings-layout refined-settings-layout">
      <div className="side-nav settings-anchor-nav" aria-label="设置分区导航">
        {SETTINGS_SECTIONS.map(section => <a
          key={section.id}
          href={`#${section.id}`}
          className={activeSettingsSection === section.id ? 'active' : ''}
          aria-current={activeSettingsSection === section.id ? 'page' : undefined}
          onClick={() => setActiveSettingsSection(section.id)}
        ><span className="nav-kicker">{section.index}</span><strong>{section.title}</strong><span>{section.subtitle}</span></a>)}
      </div>
      <div className="settings-stack refined-settings-stack">
        <div className="settings-focus-strip" role="status" aria-live="polite">
          <div>
            <span className="nav-kicker">{activeSectionMeta.index}</span>
            <div><strong>{activeSectionMeta.title}</strong><span>{activeSectionMeta.subtitle}</span></div>
          </div>
          <div className="settings-focus-hint">当前只展示一个设置分区，避免长页面连续滚动；左侧切换不会丢失未保存草稿。</div>
        </div>
        <section className={sectionClass('subscriptions', 'settings-section-featured')} id="subscriptions">
          <div className="panel-header settings-section-header"><div><div className="panel-title">订阅</div><div className="panel-subtitle">每条订阅独立编辑，长 URL 不再挤在一个文本框里。</div></div><div className="toolbar"><Button onClick={addSub}><Plus size={16} />新增订阅</Button><Button variant="primary" onClick={saveSubscriptions} disabled={saveSub.isPending || settingsUnavailable || !subsDirty}><Save size={16} />{saveSub.isPending ? '保存中...' : '保存订阅'}</Button></div></div>
          <div className="settings-status-grid">
            <div className="status-card"><Database size={16} /><span>订阅条目</span><strong>{cleanSubItems.length}</strong></div>
            <div className="status-card"><Wifi size={16} /><span>刷新状态</span><strong>{subStatusUnavailable ? '加载失败' : boolValue(status.is_refreshing) ? '刷新中' : '空闲'}</strong></div>
            <div className="status-card"><Clock3 size={16} /><span>下次刷新</span><strong>{subStatusUnavailable ? '-' : shortDate(status.next_refresh)}</strong></div>
            <div className="status-card"><Database size={16} /><span>最近节点数</span><strong>{subStatusUnavailable ? '-' : Number(status.node_count || 0)}</strong></div>
          </div>
          <div className="subscription-controls">
            {toggle('启用订阅刷新', subRefresh.enabled !== false, v=>updateDraft({...draft, subscription_refresh:{...subRefresh,enabled:v}}))}
            {input('刷新间隔', String(subRefresh.interval || '1h0m0s'), v=>updateDraft({...draft, subscription_refresh:{...subRefresh,interval:v}}))}
          </div>
          <div className="subscription-list">
            {subItems.length ? subItems.map((url, idx) => <div className="subscription-item modern-subscription-item" key={`${idx}-${url.slice(0, 16)}`}><div className="subscription-index">#{idx + 1}</div><Input aria-label={`订阅 URL #${idx + 1}`} className="settings-input mono subscription-url-input" value={url} title={url} onChange={e=>updateSub(idx, e.target.value)} /><Button variant="danger" onClick={()=>removeSub(idx)}><Trash2 size={15} />删除</Button></div>) : <div className="empty-state compact-empty"><strong>暂无订阅 URL</strong><span>点击“新增订阅”添加一条订阅地址。</span></div>}
          </div>
          <details className="raw-editor"><summary>批量编辑原始文本</summary><label htmlFor="subscriptions-raw-editor">批量编辑订阅原始文本</label><Input.TextArea id="subscriptions-raw-editor" className="settings-input mono subscription-textarea" rows={4} value={subs} onChange={e=>updateSubsDraft(e.target.value)} /></details>
          <div className="settings-inline-note"><Badge tone={boolValue(status.enabled) ? 'good' : 'neutral'}>{boolValue(status.enabled) ? '已启用' : '未启用'}</Badge><span>上次刷新：{shortDate(status.last_refresh)}</span><span>刷新次数：{Number(status.refresh_count || 0)}</span>{String(status.last_error || '') && <span className="danger-text">错误：{String(status.last_error)}</span>}</div>
        </section>

        <section className={sectionClass('free-proxy')} id="free-proxy">
          <div className="panel-header settings-section-header">
            <div><div className="panel-title">免费代理源</div><div className="panel-subtitle">把公开代理列表作为候选源。保存并重载后，系统会先抓取、去重和预筛，只让通过最低等级的代理进入运行节点。</div></div>
            <div className="toolbar">
              {freeProxyRefreshNeedsSavedDraft && <Button variant="primary" onClick={saveAllSettings} disabled={save.isPending || settingsUnavailable}><Save size={16} />先保存设置</Button>}<Button onClick={()=>manualFreeRefresh.mutate()} title={freeProxyRefreshNeedsSavedDraft ? '存在未保存设置草稿。手动刷新只会使用后端已保存配置，请先保存。' : '使用后端已保存的免费源配置启动后台刷新'} disabled={manualFreeRefresh.isPending || freeProxyRefreshState === 'refreshing' || freeProxyHasNoEnabledSources || freeProxyRefreshNeedsSavedDraft}><Wifi size={16} />{manualFreeRefresh.isPending ? '启动中...' : '手动刷新'}</Button>
              <Button onClick={enableAllFreeSources} disabled={!freeSources.length || freeSourcesEnabledCount === freeSources.length}>启用全部</Button>
              <Button onClick={disableAllFreeSources} disabled={!freeSources.length || freeSourcesEnabledCount === 0}>全部停用</Button>
              <Button onClick={resetFreeSourcesToDefaultEnabled} disabled={!freeSources.length}>默认启用</Button>
              <Button onClick={addFreeSource}><Plus size={16} />新增源</Button>
            </div>
          </div>
          <div className="settings-status-grid">
            <div className="status-card"><Database size={16} /><span>源数量</span><strong>{freeSources.length}</strong></div>
            <div className="status-card"><Wifi size={16} /><span>启用源</span><strong>{freeSourcesEnabledCount}</strong></div>
            <div className="status-card"><Database size={16} /><span>入池上限</span><strong>{Number(draft.free_proxy_max_nodes || 0) > 0 ? Number(draft.free_proxy_max_nodes) : '不限'}</strong></div>
            <div className="status-card"><Clock3 size={16} /><span>最低等级</span><strong>{String(freeFilter.min_tier || 'http_basic')}</strong></div>
            <div className="status-card"><Database size={16} /><span>本地缓存</span><strong>{freeRefresh ? `${freeRefreshCacheNodes} 条` : '未读取'}</strong></div>
            <div className="status-card"><Clock3 size={16} /><span>缓存状态</span><strong>{freeRefresh ? cacheFreshLabel(freeRefresh.cache_fresh) : '待刷新'}</strong></div>
          </div>
          {freeProxyHasNoEnabledSources && <div className="settings-inline-note free-proxy-disabled-note" role="status"><AlertCircle size={15} /><span>当前免费代理源都未启用，手动刷新不会下载任何源；启用至少一个源并保存后，系统才会后台下载、筛选、写缓存并按配置自动重载。</span></div>}
          {freeProxyRefreshNeedsSavedDraft && <div className="settings-inline-note free-proxy-disabled-note" role="status"><AlertCircle size={15} /><span>当前页面有未保存设置。手动刷新只读取后端已保存配置，为避免误以为草稿已参与刷新，请先保存设置。</span></div>}
          <div className="free-proxy-filter-panel">
            <div className="quality-toggle-row">
              {toggle('启用自动筛选', freeFilter.enabled === true, v=>updateFreeFilter({enabled:v}))}
              <span className="settings-helper-text">开启后，免费源不会直接进入运行池；只有通过预筛的代理会展示。</span>
            </div>
          <div className="form-grid-3 compact-form-grid">
            <div className="field settings-form-item"><label>最低等级</label><Select aria-label="免费源最低等级" className="settings-input" value={String(freeFilter.min_tier || 'http_basic')} onChange={v=>updateFreeFilter({min_tier:v})} options={[{value:'http_basic',label:'HTTP 基础可用'}, {value:'simple_web',label:'普通 Web 可用'}]} /></div>
            {input('入池上限（0=不限）', String(draft.free_proxy_max_nodes || 0), v=>updateDraft({...draft, free_proxy_max_nodes:Number(v)||0}), 'number')}
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
          <div className="settings-inline-note free-source-list-note"><Badge tone="info">源配置</Badge><span>补全协议只作用于 host:port 这类无协议条目；源内已有 http://、https://、socks5:// 会原样保留。单源上限填 0 表示全量解析。</span></div>
          <div className="subscription-list free-source-list">
            {freeSources.length ? freeSources.map((src, idx) => <div className="subscription-item modern-subscription-item free-source-row" key={`${idx}-${String(src.name || src.url || src.file).slice(0, 16)}`}>
              <div className="free-source-card-head">
                <label className="free-source-title">
                  <Checkbox checked={src.enabled !== false} onChange={e=>updateFreeSource(idx,{enabled:e.target.checked})} />
                  <span><strong>#{idx + 1} {src.name || '未命名源'}</strong><em>{src.enabled !== false ? '已启用' : '已停用'} · {src.url ? 'URL' : src.file ? '文件' : '待填写'} · {Number(src.max_nodes || 0) > 0 ? `最多 ${src.max_nodes} 条` : '全量解析'}</em></span>
                </label>
                <Button variant="danger" onClick={()=>removeFreeSource(idx)}><Trash2 size={15} />删除</Button>
              </div>
              <div className="free-source-card-grid">
                <div className="field settings-form-item"><label>名称</label><Input aria-label={`免费源 #${idx + 1} 名称`} className="settings-input" placeholder="new-free-source" value={src.name || ''} onChange={e=>updateFreeSource(idx,{name:e.target.value})} /></div>
                <div className="field settings-form-item free-source-url-field"><label>URL / 文件路径</label><Input aria-label={`免费源 #${idx + 1} URL 或文件路径`} className="settings-input mono" placeholder="https://raw.githubusercontent.com/..." value={src.url || src.file || ''} onChange={e=>updateFreeSource(idx, freeSourceTargetPatch(e.target.value))} /></div>
                <div className="field settings-form-item"><label>无协议条目的补全协议</label><Select aria-label={`免费源 #${idx + 1} 无协议条目的补全协议`} className="settings-input" value={freeSourceDefaultScheme(src)} onChange={v=>updateFreeSource(idx,{default_scheme:v})} options={[{value:'http',label:'http'}, {value:'socks5',label:'socks5'}]} /><div className="settings-helper-text">只补全 host:port；源内已有 http://、https://、socks5:// 会原样保留；未显式设置时会根据源名/URL 中的 socks5 自动显示。</div></div>
                <div className="field settings-form-item"><label>单源上限</label><Input aria-label={`免费源 #${idx + 1} 单源上限`} className="settings-input" type="number" title="0 表示全量解析该源" value={String(src.max_nodes ?? 0)} onChange={e=>updateFreeSource(idx,{max_nodes:Number(e.target.value)||0})} /></div>
              </div>
            </div>) : <div className="empty-state compact-empty"><strong>暂无免费代理源</strong><span>添加 GitHub raw、远程文本列表或本地文件。保存并重载后，系统会自动筛选，通过后才进入节点总览。</span><Button onClick={addFreeSource}><Plus size={15} />新增源</Button></div>}
          </div>
          <div className="settings-inline-note"><Badge tone={freeFilter.enabled ? 'good' : 'neutral'}>{freeFilter.enabled ? '自动筛选已启用' : '未启用自动筛选'}</Badge><span>建议先使用 HTTP 基础可用入池，再通过 CF/IP 风险检测筛质量；解析上限/入池上限填 0 表示不截断，探测预算填 0 表示全量探测。</span></div>
        </section>


        <section className={gridClass('pool', 'multi-port')}>
          <div className={sectionClass('pool')} id="pool"><div className="panel-header"><div><div className="panel-title">默认代理池</div><div className="panel-subtitle">常规入口和基础认证。</div></div></div><form className="form-grid-2 compact-form-grid" onSubmit={e=>e.preventDefault()}>{input('监听地址', String(listener.address || ''), v=>updateDraft({...draft, listener:{...listener,address:v}}))}{input('端口', String(listener.port || ''), v=>updateDraft({...draft, listener:{...listener,port:Number(v)||0}}), 'number')}{input('用户名', String(listener.username||''), v=>updateDraft({...draft, listener:{...listener,username:v}}))}{input('密码', String(listener.password||''), v=>updateDraft({...draft, listener:{...listener,password:v}}), 'password')}</form></div>
          <div className={sectionClass('multi-port')} id="multi-port"><div className="panel-header"><div><div className="panel-title">多端口</div><div className="panel-subtitle">批量分配独立出口。</div></div></div><form className="form-grid-2 compact-form-grid" onSubmit={e=>e.preventDefault()}>{input('监听地址', String(mp.address || ''), v=>updateDraft({...draft, multi_port:{...mp,address:v}}))}{input('起始端口', String(mp.base_port || ''), v=>updateDraft({...draft, multi_port:{...mp,base_port:Number(v)||0}}), 'number')}{input('用户名', String(mp.username||''), v=>updateDraft({...draft, multi_port:{...mp,username:v}}))}{input('密码', String(mp.password||''), v=>updateDraft({...draft, multi_port:{...mp,password:v}}), 'password')}</form></div>
        </section>

        <section className={gridClass('routing', 'quality-check')}>
          <div className={sectionClass('routing')} id="routing"><div className="panel-header"><div><div className="panel-title">地区路由 / Android</div><div className="panel-subtitle">GeoIP 路由和移动端代理端口。</div></div></div><div className="form-grid-2 compact-form-grid">{input('GeoIP listen', String(geo.listen || ''), v=>updateDraft({...draft, geoip:{...geo,listen:v}}))}{input('GeoIP port', String(geo.port || ''), v=>updateDraft({...draft, geoip:{...geo,port:Number(v)||0}}))}{input('Android listen', String(android.listen || ''), v=>updateDraft({...draft, android_proxy:{...android,listen:v}}))}{input('Android base port', String(android.base_port || ''), v=>updateDraft({...draft, android_proxy:{...android,base_port:Number(v)||0}}))}</div></div>
          <div className={sectionClass('quality-check')} id="quality-check"><div className="panel-header"><div><div className="panel-title">质量检测定时任务</div><div className="panel-subtitle">自动刷新 CF 评分和 IP 风险缓存。</div></div><div className="toolbar"><Button onClick={() => { void refreshQualityCache() }} disabled={qualityCacheLoading}>{qualityCacheLoading ? '刷新中...' : '刷新缓存状态'}</Button></div></div><div className="settings-status-grid quality-cache-grid"><div className="status-card"><Database size={16} /><span>CF 缓存</span><strong>{cfCacheUnavailable ? '加载失败' : cfRows.length}</strong></div><div className="status-card"><Database size={16} /><span>IP 风险缓存</span><strong>{repCacheUnavailable ? '加载失败' : repRows.length}</strong></div><div className="status-card"><Clock3 size={16} /><span>CF 最近检测</span><strong>{cfCacheUnavailable ? '-' : latestCheckedAt(cfRows)}</strong></div><div className="status-card"><Clock3 size={16} /><span>IP 最近检测</span><strong>{repCacheUnavailable ? '-' : latestCheckedAt(repRows)}</strong></div></div><div className="quality-toggle-row">{toggle('启用定时检测', boolValue(quality.enabled), v=>updateDraft({...draft, quality_check:{...quality,enabled:v}}))}{toggle('包含不可用节点', quality.include_unavailable !== false, v=>updateDraft({...draft, quality_check:{...quality,include_unavailable:v}}))}{toggle('优先重试失败节点', boolValue(quality.retry_failed), v=>updateDraft({...draft, quality_check:{...quality,retry_failed:v}}))}</div><div className="form-grid-3 compact-form-grid">{input('检测间隔', String(quality.interval || '24h'), v=>updateDraft({...draft, quality_check:{...quality,interval:v}}))}{input('地区范围', String(quality.region || 'all'), v=>updateDraft({...draft, quality_check:{...quality,region:v}}))}{input('检测数量（1-500）', String(quality.count || 500), v=>updateDraft({...draft, quality_check:{...quality,count:clampNumber(v, 500, 1, 500)}}), 'number')}{input('CF 超时', String(quality.cloudflare_timeout || '5s'), v=>updateDraft({...draft, quality_check:{...quality,cloudflare_timeout:v}}))}{input('CF 并发', String(quality.cloudflare_concurrency || 24), v=>updateDraft({...draft, quality_check:{...quality,cloudflare_concurrency:Number(v)||24}}), 'number')}</div><div className="settings-inline-note">建议间隔不要低于 1 小时；检测数量上限为 500；CF 超时越短扫描越快但失败率可能升高，并发建议 24-32。</div></div>
        </section>

        <section className={sectionClass('management')} id="management"><div className="panel-header"><div><div className="panel-title">管理与日志</div><div className="panel-subtitle">控制面入口和日志滚动策略。</div></div></div><div className="form-grid-2 compact-form-grid">{input('管理 listen', String(mgmt.listen || ''), v=>updateDraft({...draft, management:{...mgmt,listen:v}}))}{input('新管理 password（留空保持不变）', managementPasswordDraft, updateManagementPasswordDraft, 'password')}{input('日志 output', String(log.output || ''), v=>updateDraft({...draft, log:{...log,output:v}}))}{input('日志 max size', String(log.max_size || ''), v=>updateDraft({...draft, log:{...log,max_size:Number(v)||0}}))}</div><div className="settings-inline-note">{toggle('清空管理密码', managementPasswordClear, updateManagementPasswordClear)}<span>{mgmt.password_set ? '当前已设置管理密码；密码不会从接口回显。' : '当前未设置管理密码。'}</span></div></section>
      </div>
    </div>
    {hasUnsavedChanges && <div className="settings-sticky-actions" role="region" aria-label="未保存设置操作条">
      <div>
        <Badge tone="warn">未保存</Badge>
        <strong>{activeSectionMeta.title}</strong>
        <span>{settingsDirty && subsDirty ? '设置与订阅都有草稿' : settingsDirty ? '设置草稿待保存' : '订阅草稿待保存'}</span>
      </div>
      <div className="toolbar">
        <Button onClick={resetDrafts} disabled={save.isPending || saveSub.isPending}>放弃更改</Button>
        <Button variant="primary" onClick={saveAllSettings} disabled={save.isPending || saveSub.isPending || settingsUnavailable}><Save size={16} />{save.isPending || saveSub.isPending ? '保存中...' : '保存更改'}</Button>
      </div>
    </div>}
  </div>
}
