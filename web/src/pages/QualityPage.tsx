import { useMemo, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { getCloudflareCache, checkCloudflare } from '../api/cloudflare'
import { getSettings } from '../api/settings'
import { getReputationCache, checkReputation } from '../api/reputation'
import { Button } from '../components/ui/Button'
import { Badge } from '../components/ui/Badge'
import { DataTable } from '../components/ui/DataTable'
import { useToast } from '../components/ui/Toast'
import { useAppStore } from '../store/appStore'
import { useExtractorStore } from '../store/extractorStore'
import type { CloudflareResult } from '../types/cloudflare'
import type { ReputationResult } from '../types/reputation'

function levelTone(level?: string) { return level === 'excellent' || level === 'low' ? 'good' : level === 'good' || level === 'medium' ? 'warn' : level ? 'bad' : 'neutral' }
function cfLabel(level?: string) { return ({excellent:'优秀',good:'良好',fair:'一般',poor:'较差',failed:'失败'} as Record<string,string>)[level || ''] || '-' }
function repLevel(row: ReputationResult) { const r = row.result || row; return r.risk_level || (row.error ? 'failed' : '-') }

export function QualityPage() {
  const [region, setRegion] = useState('all')
  const [count, setCount] = useState(20)
  const [cfRows, setCfRows] = useState<CloudflareResult[]>([])
  const [repRows, setRepRows] = useState<ReputationResult[]>([])
  const [filter, setFilter] = useState('all')
  const toast = useToast(s => s.show)
  const setActiveTab = useAppStore(s => s.setActiveTab)
  const setExtractorParams = useExtractorStore(s => s.setParams)
  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })
  const cfCache = useQuery({ queryKey: ['cf-cache'], queryFn: getCloudflareCache, enabled: false })
  const repCache = useQuery({ queryKey: ['rep-cache'], queryFn: getReputationCache, enabled: false })
  const cfScan = useMutation({ mutationFn: (full: boolean) => checkCloudflare(region, full ? 500 : count, full), onSuccess: d => { setCfRows(d.data || []); toast('CF 检测完成', 'ok') }, onError: e => toast(e instanceof Error ? e.message : 'CF 检测失败', 'error') })
  const repScan = useMutation({ mutationFn: () => checkReputation(region, count), onSuccess: d => { setRepRows(d.data || []); toast('IP 信誉检测完成', 'ok') }, onError: e => toast(e instanceof Error ? e.message : 'IP 信誉检测失败', 'error') })
  const loadCache = async () => { const [cf, rep] = await Promise.all([cfCache.refetch(), repCache.refetch()]); setCfRows(cf.data?.data || []); setRepRows(rep.data?.data || []); toast('缓存已加载', 'ok') }
  const repByPort = useMemo(() => new Map(repRows.map(r => [String(r.port || ''), r])), [repRows])
  const rows = cfRows.filter(r => filter === 'all' || r.level === filter).sort((a,b) => (b.score || 0) - (a.score || 0))
  const proxyUrl = (row: CloudflareResult) => {
    const mp = (settings.data?.multi_port || {}) as Record<string, unknown>
    const host = String(row.host || mp.address || '127.0.0.1')
    const user = String(mp.username || '')
    const pass = String(mp.password || '')
    const auth = user || pass ? `${encodeURIComponent(user)}:${encodeURIComponent(pass)}@` : ''
    return `http://${auth}${host}:${row.port || ''}`
  }
  const extract = (row: CloudflareResult) => { setExtractorParams({ mode:'multi-port', region: (row.region || 'all') as never, format:'http_url', count:1, reveal:true }); setActiveTab('extractor'); toast('已带入代理提取页', 'ok') }
  return <div className="page">
    <div className="page-header"><div><h1>节点质量</h1><p>筛选稳定、低风险、CF 兼容性好的节点。CF 评分是本地兼容性评分，不是官方 Bot Score。</p></div></div>
    <div className="card"><div className="toolbar"><select className="input" style={{width:150}} value={region} onChange={e=>setRegion(e.target.value)}><option value="all">全部</option><option value="us">美国</option><option value="jp">日本</option><option value="hk">香港</option><option value="sg">新加坡</option><option value="de">德国</option><option value="gb">英国</option></select><input className="input" style={{width:110}} type="number" value={count} onChange={e=>setCount(Number(e.target.value)||10)} /><Button variant="primary" onClick={() => cfScan.mutate(false)}>快速检测</Button><Button onClick={() => cfScan.mutate(true)}>完整扫描</Button><Button onClick={() => repScan.mutate()}>IP 信誉</Button><Button onClick={loadCache}>查看缓存</Button><select className="input" style={{width:130}} value={filter} onChange={e=>setFilter(e.target.value)}><option value="all">全部等级</option><option value="excellent">优秀</option><option value="good">良好</option><option value="fair">一般</option><option value="poor">较差</option><option value="failed">失败</option></select></div></div>
    <div className="summary-grid"><div className="metric"><div className="label">CF 优秀</div><div className="value success">{cfRows.filter(r=>r.level==='excellent').length}</div></div><div className="metric"><div className="label">CF 良好</div><div className="value">{cfRows.filter(r=>r.level==='good').length}</div></div><div className="metric"><div className="label">低风险</div><div className="value success">{repRows.filter(r=>repLevel(r)==='low').length}</div></div><div className="metric"><div className="label">失败/高风险</div><div className="value error">{cfRows.filter(r=>r.level==='failed').length + repRows.filter(r=>['high','failed'].includes(repLevel(r))).length}</div></div></div>
    <DataTable headers={['节点','地区/端口','出口 IP','CF 分','IP 风险','延迟','操作']} empty="暂无质量数据，请先检测或查看缓存。">{rows.map((r, idx) => { const rep = repByPort.get(String(r.port || '')); const risk = rep ? repLevel(rep) : '-'; return <tr key={`${r.node_tag || r.node_name}-${idx}`}><td>{r.node_name || r.node_tag || '-'}<br/><span className="muted mono">{r.node_tag || ''}</span></td><td>{r.region || '-'}:{r.port || '-'}</td><td>{r.exit_ip || '-'}</td><td><Badge tone={levelTone(r.level)}>{r.score ?? '-'} / {cfLabel(r.level)}</Badge></td><td><Badge tone={levelTone(risk)}>{risk}</Badge></td><td>{r.latency_ms || 0} ms</td><td><div className="toolbar"><Button onClick={() => navigator.clipboard.writeText(proxyUrl(r)).then(()=>toast('代理已复制','ok'))}>复制代理</Button><Button onClick={() => navigator.clipboard.writeText(`curl -x ${proxyUrl(r)} http://cp.cloudflare.com/generate_204`).then(()=>toast('curl 已复制','ok'))}>复制 curl</Button><Button onClick={() => extract(r)}>提取到代理页</Button></div></td></tr> })}</DataTable>
  </div>
}
