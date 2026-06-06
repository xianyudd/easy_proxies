import { useEffect, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Checkbox, Input, Select } from 'antd'
import { AlertCircle, Clock3, Database, Plus, Save, Trash2, Wifi } from 'lucide-react'
import { getSettings, saveSettings, getSubscriptionStatus, saveSubscriptionConfig, reloadCore } from '../api/settings'
import { getCloudflareCache } from '../api/cloudflare'
import { getReputationCache } from '../api/reputation'
import { Button } from '../components/ui/Button'
import { Badge } from '../components/ui/Badge'
import { useToast } from '../components/ui/Toast'
import type { FreeProxyCache, FreeProxyFilter, FreeProxySource, SettingsResponse } from '../types/settings'
import type { CloudflareResult } from '../types/cloudflare'
import type { ReputationResult } from '../types/reputation'

function listValue(value: unknown) { return Array.isArray(value) ? value.join('\n') : '' }
function boolValue(value: unknown) { return value === true || value === 'true' }
function shortDate(value: unknown) {
  const text = String(value || '')
  if (!text || text.startsWith('0001-')) return '未执行'
  return new Date(text).toLocaleString()
}
function splitLines(value: string) { return value.split('\n').map(s => s.trim()).filter(Boolean) }
function isWideOpen(value: unknown) { return String(value || '').trim() === '0.0.0.0' }
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

export function SettingsPage() {
  const settings = useQuery({ queryKey:['settings'], queryFn:getSettings })
  const subStatus = useQuery({ queryKey:['sub-status'], queryFn:getSubscriptionStatus })
  const cfCache = useQuery({ queryKey:['cf-cache'], queryFn:getCloudflareCache })
  const repCache = useQuery({ queryKey:['rep-cache'], queryFn:getReputationCache })
  const [draft, setDraft] = useState<SettingsResponse>({})
  const [subs, setSubs] = useState('')
  const [needsReload, setNeedsReload] = useState(false)
  const toast = useToast(s=>s.show)
  useEffect(()=>{ if(settings.data){ setDraft(settings.data); setSubs(listValue(settings.data.subscriptions)) } }, [settings.data])
  useEffect(() => {
    const id = window.location.hash.slice(1)
    if (!id) return
    window.setTimeout(() => document.getElementById(id)?.scrollIntoView({ block: 'start' }), 0)
  }, [])
  const save = useMutation({ mutationFn: saveSettings, onSuccess:(res)=>{ toast('设置已保存', 'ok'); setNeedsReload(Boolean(res?.need_reload)); void settings.refetch() }, onError:e=>toast(e instanceof Error ? e.message:'保存失败','error') })
  const saveSub = useMutation({ mutationFn: saveSubscriptionConfig, onSuccess:()=>{ toast('订阅配置已保存','ok'); setNeedsReload(true); void settings.refetch(); void subStatus.refetch() }, onError:e=>toast(e instanceof Error ? e.message:'订阅保存失败','error') })
  const reload = useMutation({ mutationFn: reloadCore, onSuccess:()=>{ toast('重载成功', 'ok'); setNeedsReload(false); void settings.refetch(); void subStatus.refetch() }, onError:e=>toast(e instanceof Error ? e.message:'重载失败','error') })
  const input = (label:string, value:string, onChange:(v:string)=>void, type='text') => <div className="field settings-form-item"><label>{label}</label><Input className="settings-input" type={type} autoComplete={type === 'password' ? 'current-password' : label.includes('用户名') ? 'username' : undefined} value={value} onChange={e=>onChange(e.target.value)} /></div>
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
  const subItems = splitLines(subs)
  const cfRows = (cfCache.data?.data || []) as CloudflareResult[]
  const repRows = (repCache.data?.data || []) as ReputationResult[]
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
  const reloadAfterSave = () => reload.mutate()
  const saveAllSettings = () => {
    if (freeSourceIssues.length > 0) {
      toast('请先补全免费代理源的 URL 或文件路径', 'error')
      return
    }
    save.mutate(draft)
  }
  const saveSubscriptions = () => saveSub.mutate({
    subscriptions: subItems,
    enabled: subRefresh.enabled !== false,
    interval: String(subRefresh.interval || '1h0m0s'),
  })

  return <div className="page settings-page">
    <div className="page-header settings-hero">
      <div><h1>系统设置</h1><p>集中管理订阅来源、代理入口、地区路由、质量检测和日志策略。</p></div>
      <div className="toolbar"><Button variant="primary" onClick={saveAllSettings} disabled={save.isPending}><Save size={16} />{save.isPending ? '保存中...' : '保存设置'}</Button></div>
    </div>
    {(isPublicAdmin || isPublicProxy) && <div className="settings-alert modern-settings-alert" role="alert">
      <AlertCircle size={18} />
      <div>
        <strong>{isPublicAdmin ? '管理端口当前对所有网卡开放且未设置密码' : '代理入口当前对所有网卡开放'}</strong>
        <span>如果只在本机使用，建议把监听地址改为 127.0.0.1；管理端口建议设置密码后再对局域网开放。</span>
      </div>
    </div>}
    {needsReload && <div className="settings-alert modern-settings-alert settings-reload-alert" role="status">
      <Clock3 size={18} />
      <div>
        <strong>设置已保存，重载核心后生效</strong>
        <span>新的免费源和自动筛选策略需要重载后才会抓取、预筛并进入节点总览。</span>
      </div>
      <Button onClick={reloadAfterSave} disabled={reload.isPending}>{reload.isPending ? '重载中...' : '立即重载'}</Button>
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
            <div className="status-card"><Database size={16} /><span>订阅条目</span><strong>{subItems.length}</strong></div>
            <div className="status-card"><Wifi size={16} /><span>刷新状态</span><strong>{boolValue(status.is_refreshing) ? '刷新中' : '空闲'}</strong></div>
            <div className="status-card"><Clock3 size={16} /><span>下次刷新</span><strong>{shortDate(status.next_refresh)}</strong></div>
            <div className="status-card"><Database size={16} /><span>最近节点数</span><strong>{Number(status.node_count || 0)}</strong></div>
          </div>
          <div className="subscription-controls">
            {toggle('启用订阅刷新', subRefresh.enabled !== false, v=>setDraft({...draft, subscription_refresh:{...subRefresh,enabled:v}}))}
            {input('刷新间隔', String(subRefresh.interval || '1h0m0s'), v=>setDraft({...draft, subscription_refresh:{...subRefresh,interval:v}}))}
          </div>
          <div className="subscription-list">
            {subItems.length ? subItems.map((url, idx) => <div className="subscription-item modern-subscription-item" key={`${idx}-${url.slice(0, 16)}`}><div className="subscription-index">#{idx + 1}</div><Input className="settings-input mono subscription-url-input" value={url} title={url} onChange={e=>updateSub(idx, e.target.value)} /><Button variant="danger" onClick={()=>removeSub(idx)}><Trash2 size={15} />删除</Button></div>) : <div className="empty-state compact-empty"><strong>暂无订阅 URL</strong><span>点击“新增订阅”添加一条订阅地址。</span></div>}
          </div>
          <details className="raw-editor"><summary>批量编辑原始文本</summary><Input.TextArea className="settings-input mono subscription-textarea" rows={4} value={subs} onChange={e=>setSubs(e.target.value)} /></details>
          <div className="settings-inline-note"><Badge tone={boolValue(status.enabled) ? 'good' : 'neutral'}>{boolValue(status.enabled) ? '已启用' : '未启用'}</Badge><span>上次刷新：{shortDate(status.last_refresh)}</span><span>刷新次数：{Number(status.refresh_count || 0)}</span>{String(status.last_error || '') && <span className="danger-text">错误：{String(status.last_error)}</span>}</div>
        </section>

        <section className="card settings-section" id="free-proxy">
          <div className="panel-header settings-section-header">
            <div><div className="panel-title">免费代理源</div><div className="panel-subtitle">把公开代理列表作为候选源。保存并重载后，系统会先抓取、去重和预筛，只让通过最低等级的代理进入运行节点。</div></div>
            <div className="toolbar"><Button onClick={addFreeSource}><Plus size={16} />新增源</Button></div>
          </div>
          <div className="settings-status-grid">
            <div className="status-card"><Database size={16} /><span>源数量</span><strong>{freeSources.length}</strong></div>
            <div className="status-card"><Wifi size={16} /><span>启用源</span><strong>{freeSources.filter(s => s.enabled !== false).length}</strong></div>
            <div className="status-card"><Database size={16} /><span>入池上限</span><strong>{Number(draft.free_proxy_max_nodes || 0) > 0 ? Number(draft.free_proxy_max_nodes) : '不限'}</strong></div>
            <div className="status-card"><Clock3 size={16} /><span>最低等级</span><strong>{String(freeFilter.min_tier || 'http_basic')}</strong></div>
          </div>
          <div className="free-proxy-filter-panel">
            <div className="quality-toggle-row">
              {toggle('启用自动筛选', freeFilter.enabled === true, v=>updateFreeFilter({enabled:v}))}
              <span className="settings-helper-text">开启后，免费源不会直接进入运行池；只有通过预筛的代理会展示。</span>
            </div>
          <div className="form-grid-3 compact-form-grid">
            <div className="field settings-form-item"><label>最低等级</label><Select className="settings-input" value={String(freeFilter.min_tier || 'http_basic')} onChange={v=>updateFreeFilter({min_tier:v})} options={[{value:'http_basic',label:'HTTP 基础可用'}, {value:'simple_web',label:'普通 Web 可用'}]} /></div>
            {input('入池上限（0=不限）', String(draft.free_proxy_max_nodes || 0), v=>setDraft({...draft, free_proxy_max_nodes:Number(v)||0}), 'number')}
            {input('候选上限（0=全量）', String(freeFilter.max_candidates || 0), v=>updateFreeFilter({max_candidates:Number(v)||0}), 'number')}
            {input('筛选并发', String(freeFilter.workers || 80), v=>updateFreeFilter({workers:Number(v)||80}), 'number')}
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
              {input('源下载并发', String(freeCache.workers || 4), v=>updateFreeCache({workers:Number(v)||4}), 'number')}
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
          <div className="settings-inline-note"><Badge tone={freeFilter.enabled ? 'good' : 'neutral'}>{freeFilter.enabled ? '自动筛选已启用' : '未启用自动筛选'}</Badge><span>建议最低等级使用 simple_web；默认每个免费源全量进入后台候选处理，候选上限/入池上限填 0 表示不截断。</span></div>
        </section>


        <section className="settings-card-grid">
          <div className="card settings-section" id="pool"><div className="panel-header"><div><div className="panel-title">默认代理池</div><div className="panel-subtitle">常规入口和基础认证。</div></div></div><form className="form-grid-2 compact-form-grid" onSubmit={e=>e.preventDefault()}>{input('监听地址', String(listener.address || ''), v=>setDraft({...draft, listener:{...listener,address:v}}))}{input('端口', String(listener.port || ''), v=>setDraft({...draft, listener:{...listener,port:Number(v)||0}}), 'number')}{input('用户名', String(listener.username||''), v=>setDraft({...draft, listener:{...listener,username:v}}))}{input('密码', String(listener.password||''), v=>setDraft({...draft, listener:{...listener,password:v}}), 'password')}</form></div>
          <div className="card settings-section" id="multi-port"><div className="panel-header"><div><div className="panel-title">多端口</div><div className="panel-subtitle">批量分配独立出口。</div></div></div><form className="form-grid-2 compact-form-grid" onSubmit={e=>e.preventDefault()}>{input('监听地址', String(mp.address || ''), v=>setDraft({...draft, multi_port:{...mp,address:v}}))}{input('起始端口', String(mp.base_port || ''), v=>setDraft({...draft, multi_port:{...mp,base_port:Number(v)||0}}), 'number')}{input('用户名', String(mp.username||''), v=>setDraft({...draft, multi_port:{...mp,username:v}}))}{input('密码', String(mp.password||''), v=>setDraft({...draft, multi_port:{...mp,password:v}}), 'password')}</form></div>
        </section>

        <section className="settings-card-grid">
          <div className="card settings-section" id="routing"><div className="panel-header"><div><div className="panel-title">地区路由 / Android</div><div className="panel-subtitle">GeoIP 路由和移动端代理端口。</div></div></div><div className="form-grid-2 compact-form-grid">{input('GeoIP listen', String(geo.listen || ''), v=>setDraft({...draft, geoip:{...geo,listen:v}}))}{input('GeoIP port', String(geo.port || ''), v=>setDraft({...draft, geoip:{...geo,port:Number(v)||0}}))}{input('Android listen', String(android.listen || ''), v=>setDraft({...draft, android_proxy:{...android,listen:v}}))}{input('Android base port', String(android.base_port || ''), v=>setDraft({...draft, android_proxy:{...android,base_port:Number(v)||0}}))}</div></div>
          <div className="card settings-section" id="quality-check"><div className="panel-header"><div><div className="panel-title">质量检测定时任务</div><div className="panel-subtitle">自动刷新 CF 评分和 IP 风险缓存。</div></div><div className="toolbar"><Button onClick={() => { void cfCache.refetch(); void repCache.refetch() }}>刷新缓存状态</Button></div></div><div className="settings-status-grid quality-cache-grid"><div className="status-card"><Database size={16} /><span>CF 缓存</span><strong>{cfRows.length}</strong></div><div className="status-card"><Database size={16} /><span>IP 风险缓存</span><strong>{repRows.length}</strong></div><div className="status-card"><Clock3 size={16} /><span>CF 最近检测</span><strong>{latestCheckedAt(cfRows)}</strong></div><div className="status-card"><Clock3 size={16} /><span>IP 最近检测</span><strong>{latestCheckedAt(repRows)}</strong></div></div><div className="quality-toggle-row">{toggle('启用定时检测', boolValue(quality.enabled), v=>setDraft({...draft, quality_check:{...quality,enabled:v}}))}{toggle('包含不可用节点', quality.include_unavailable !== false, v=>setDraft({...draft, quality_check:{...quality,include_unavailable:v}}))}{toggle('优先重试失败节点', boolValue(quality.retry_failed), v=>setDraft({...draft, quality_check:{...quality,retry_failed:v}}))}</div><div className="form-grid-3 compact-form-grid">{input('检测间隔', String(quality.interval || '24h'), v=>setDraft({...draft, quality_check:{...quality,interval:v}}))}{input('地区范围', String(quality.region || 'all'), v=>setDraft({...draft, quality_check:{...quality,region:v}}))}{input('检测数量', String(quality.count || 500), v=>setDraft({...draft, quality_check:{...quality,count:Number(v)||500}}), 'number')}</div><div className="settings-inline-note">建议间隔不要低于 1 小时；全量检测会访问外部查询服务。</div></div>
        </section>

        <section className="card settings-section" id="management"><div className="panel-header"><div><div className="panel-title">管理与日志</div><div className="panel-subtitle">控制面入口和日志滚动策略。</div></div></div><div className="form-grid-2 compact-form-grid">{input('管理 listen', String(mgmt.listen || ''), v=>setDraft({...draft, management:{...mgmt,listen:v}}))}{input('管理 password', String(mgmt.password||''), v=>setDraft({...draft, management:{...mgmt,password:v}}))}{input('日志 output', String(log.output || ''), v=>setDraft({...draft, log:{...log,output:v}}))}{input('日志 max size', String(log.max_size || ''), v=>setDraft({...draft, log:{...log,max_size:Number(v)||0}}))}</div></section>
      </div>
    </div>
  </div>
}
