import { useEffect, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { AlertTriangle, Clock3, Database, Plus, Save, Trash2, Wifi } from 'lucide-react'
import { getSettings, saveSettings, getSubscriptionStatus, saveSubscriptionConfig } from '../api/settings'
import { getCloudflareCache } from '../api/cloudflare'
import { getReputationCache } from '../api/reputation'
import { Button } from '../components/ui/Button'
import { Badge } from '../components/ui/Badge'
import { useToast } from '../components/ui/Toast'
import type { SettingsResponse } from '../types/settings'
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
  const toast = useToast(s=>s.show)
  useEffect(()=>{ if(settings.data){ setDraft(settings.data); setSubs(listValue(settings.data.subscriptions)) } }, [settings.data])
  useEffect(() => {
    const id = window.location.hash.slice(1)
    if (!id) return
    window.setTimeout(() => document.getElementById(id)?.scrollIntoView({ block: 'start' }), 0)
  }, [])
  const save = useMutation({ mutationFn: saveSettings, onSuccess:()=>{ toast('设置已保存', 'ok'); void settings.refetch() }, onError:e=>toast(e instanceof Error ? e.message:'保存失败','error') })
  const saveSub = useMutation({ mutationFn: saveSubscriptionConfig, onSuccess:()=>{ toast('订阅配置已保存','ok'); void settings.refetch(); void subStatus.refetch() }, onError:e=>toast(e instanceof Error ? e.message:'订阅保存失败','error') })
  const input = (label:string, value:string, onChange:(v:string)=>void, type='text') => <div className="field"><label>{label}</label><input className="input" type={type} value={value} onChange={e=>onChange(e.target.value)} /></div>
  const toggle = (label:string, checked:boolean, onChange:(v:boolean)=>void) => <label className="toggle-field"><input type="checkbox" checked={checked} onChange={e=>onChange(e.target.checked)} /><span>{label}</span></label>
  const listener = (draft.listener || {}) as Record<string, unknown>
  const mp = (draft.multi_port || {}) as Record<string, unknown>
  const geo = (draft.geoip || {}) as Record<string, unknown>
  const android = (draft.android_proxy || {}) as Record<string, unknown>
  const mgmt = (draft.management || {}) as Record<string, unknown>
  const log = (draft.log || {}) as Record<string, unknown>
  const quality = (draft.quality_check || {}) as Record<string, unknown>
  const subRefresh = (draft.subscription_refresh || {}) as Record<string, unknown>
  const status = subStatus.data || {}
  const subItems = splitLines(subs)
  const cfRows = (cfCache.data?.data || []) as CloudflareResult[]
  const repRows = (repCache.data?.data || []) as ReputationResult[]
  const isPublicAdmin = String(mgmt.listen || '').startsWith('0.0.0.0') && !String(mgmt.password || '').trim()
  const isPublicProxy = isWideOpen(listener.address) || isWideOpen(mp.address) || isWideOpen(android.listen)
  const updateSub = (idx: number, value: string) => setSubs(subItems.map((item, i) => i === idx ? value : item).join('\n'))
  const removeSub = (idx: number) => setSubs(subItems.filter((_, i) => i !== idx).join('\n'))
  const addSub = () => setSubs([...subItems, ''].join('\n'))
  const saveSubscriptions = () => saveSub.mutate({
    subscriptions: subItems,
    enabled: subRefresh.enabled !== false,
    interval: String(subRefresh.interval || '1h0m0s'),
  })

  return <div className="page settings-page">
    <div className="page-header settings-hero">
      <div><h1>系统设置</h1><p>集中管理订阅来源、代理入口、地区路由、质量检测和日志策略。</p></div>
      <div className="toolbar"><Button variant="primary" onClick={()=>save.mutate(draft)} disabled={save.isPending}><Save size={16} />{save.isPending ? '保存中...' : '保存设置'}</Button></div>
    </div>
    {(isPublicAdmin || isPublicProxy) && <div className="settings-alert">
      <AlertTriangle size={18} />
      <div>
        <strong>{isPublicAdmin ? '管理端口当前对所有网卡开放且未设置密码' : '代理入口当前对所有网卡开放'}</strong>
        <span>如果只在本机使用，建议把监听地址改为 127.0.0.1；管理端口建议设置密码后再对局域网开放。</span>
      </div>
    </div>}
    <div className="settings-layout refined-settings-layout">
      <div className="side-nav settings-anchor-nav">
        <a href="#subscriptions"><strong>订阅</strong><span>节点来源与刷新</span></a>
        <a href="#pool"><strong>默认代理池</strong><span>监听地址和认证</span></a>
        <a href="#multi-port"><strong>多端口</strong><span>批量端口出口</span></a>
        <a href="#routing"><strong>地区 / Android</strong><span>GeoIP 与移动端代理</span></a>
        <a href="#quality-check"><strong>质量检测</strong><span>定时刷新评分缓存</span></a>
        <a href="#management"><strong>管理与日志</strong><span>控制面与日志策略</span></a>
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
            {subItems.length ? subItems.map((url, idx) => <div className="subscription-item" key={`${idx}-${url.slice(0, 16)}`}><div className="subscription-index">#{idx + 1}</div><input className="input mono" value={url} title={url} onChange={e=>updateSub(idx, e.target.value)} /><Button variant="danger" onClick={()=>removeSub(idx)}><Trash2 size={15} />删除</Button></div>) : <div className="empty-state compact-empty"><strong>暂无订阅 URL</strong><span>点击“新增订阅”添加一条订阅地址。</span></div>}
          </div>
          <details className="raw-editor"><summary>批量编辑原始文本</summary><textarea className="input mono subscription-textarea" rows={4} value={subs} onChange={e=>setSubs(e.target.value)} /></details>
          <div className="settings-inline-note"><Badge tone={boolValue(status.enabled) ? 'good' : 'neutral'}>{boolValue(status.enabled) ? '已启用' : '未启用'}</Badge><span>上次刷新：{shortDate(status.last_refresh)}</span><span>刷新次数：{Number(status.refresh_count || 0)}</span>{String(status.last_error || '') && <span className="danger-text">错误：{String(status.last_error)}</span>}</div>
        </section>

        <section className="settings-card-grid">
          <div className="card settings-section" id="pool"><div className="panel-header"><div><div className="panel-title">默认代理池</div><div className="panel-subtitle">常规入口和基础认证。</div></div></div><div className="form-grid-2 compact-form-grid">{input('监听地址', String(listener.address || ''), v=>setDraft({...draft, listener:{...listener,address:v}}))}{input('端口', String(listener.port || ''), v=>setDraft({...draft, listener:{...listener,port:Number(v)||0}}), 'number')}{input('用户名', String(listener.username||''), v=>setDraft({...draft, listener:{...listener,username:v}}))}{input('密码', String(listener.password||''), v=>setDraft({...draft, listener:{...listener,password:v}}), 'password')}</div></div>
          <div className="card settings-section" id="multi-port"><div className="panel-header"><div><div className="panel-title">多端口</div><div className="panel-subtitle">批量分配独立出口。</div></div></div><div className="form-grid-2 compact-form-grid">{input('监听地址', String(mp.address || ''), v=>setDraft({...draft, multi_port:{...mp,address:v}}))}{input('起始端口', String(mp.base_port || ''), v=>setDraft({...draft, multi_port:{...mp,base_port:Number(v)||0}}), 'number')}{input('用户名', String(mp.username||''), v=>setDraft({...draft, multi_port:{...mp,username:v}}))}{input('密码', String(mp.password||''), v=>setDraft({...draft, multi_port:{...mp,password:v}}), 'password')}</div></div>
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
