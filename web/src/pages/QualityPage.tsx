import { useEffect, useMemo, useState } from 'react'
import { InputNumber, Select, Space, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useMutation, useQuery } from '@tanstack/react-query'
import { getNodes } from '../api/nodes'
import { getCloudflareCache, checkCloudflare } from '../api/cloudflare'
import { getSettings } from '../api/settings'
import { getReputationCache, checkReputation } from '../api/reputation'
import { Button } from '../components/ui/Button'
import { Badge } from '../components/ui/Badge'
import { CfDistributionChart, ReputationRiskChart, CfScoreRankChart } from '../components/charts/QualityCharts'
import { useToast } from '../components/ui/Toast'
import { useAppStore } from '../store/appStore'
import { useExtractorStore } from '../store/extractorStore'
import type { CloudflareResult } from '../types/cloudflare'
import type { ReputationResult } from '../types/reputation'

function levelTone(level?: string) { return level === 'excellent' || level === 'low' ? 'good' : level === 'good' || level === 'medium' ? 'warn' : level ? 'bad' : 'neutral' }
function cfLabel(level?: string) { return ({excellent:'优秀',good:'良好',fair:'一般',poor:'较差',failed:'失败'} as Record<string,string>)[level || ''] || '-' }
function repLevel(row: ReputationResult) { const r = row.result || row; return r.risk_level || (row.error ? 'failed' : '-') }
function qualityLabel(score: number) { return score >= 80 ? '高质量' : score >= 60 ? '可用' : score >= 40 ? '一般' : '不推荐' }
function qualityTone(score: number) { return score >= 80 ? 'good' : score >= 60 ? 'info' : score >= 40 ? 'warn' : 'bad' }
function riskPenalty(level?: string) { return level === 'low' ? 0 : level === 'medium' ? 18 : level === 'high' ? 36 : level === 'failed' ? 50 : 12 }
function riskScore(row?: ReputationResult) { const r = row?.result || row; return Number(r?.risk_score) || 0 }
function failedCf(row: CloudflareResult) { return row.level === 'failed' || !!row.error }
function rowKey(row: { node_tag?: string; port?: number }) { return row.node_tag || String(row.port || '') }
function mergeCfRows(current: CloudflareResult[], incoming: CloudflareResult[]) {
  const map = new Map(current.map(row => [rowKey(row), row]))
  incoming.forEach(row => map.set(rowKey(row), row))
  return [...map.values()]
}
function mergeRepRows(current: ReputationResult[], incoming: ReputationResult[]) {
  const map = new Map(current.map(row => [rowKey(row), row]))
  incoming.forEach(row => map.set(rowKey(row), row))
  return [...map.values()]
}

type QualityRow = { key: string; row: CloudflareResult; rep?: ReputationResult; repRisk: string; score: number }

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
  const nodesQuery = useQuery({ queryKey: ['nodes'], queryFn: getNodes })
  const cfCache = useQuery({ queryKey: ['cf-cache'], queryFn: getCloudflareCache, enabled: false })
  const repCache = useQuery({ queryKey: ['rep-cache'], queryFn: getReputationCache, enabled: false })
  const scanCount = Math.max(1, count)
  const allCount = Math.max(nodesQuery.data?.length || 0, 500)
  const cfScan = useMutation({ mutationFn: () => checkCloudflare(region, scanCount, false), onSuccess: d => { setCfRows(d.data || []); toast('CF 检测完成', 'ok') }, onError: e => toast(e instanceof Error ? e.message : 'CF 检测失败', 'error') })
  const fullScan = useMutation({ mutationFn: () => Promise.all([checkCloudflare(region, allCount, true), checkReputation(region, allCount, false, true)]), onSuccess: ([cf, rep]) => { setCfRows(cf.data || []); setRepRows(rep.data || []); toast('全量扫描已完成并写入缓存', 'ok') }, onError: e => toast(e instanceof Error ? e.message : '全量扫描失败', 'error') })
  const retryScan = useMutation({ mutationFn: () => Promise.all([checkCloudflare(region, allCount, true, true), checkReputation(region, allCount, true, true)]), onSuccess: ([cf, rep]) => { setCfRows(prev => mergeCfRows(prev, cf.data || [])); setRepRows(prev => mergeRepRows(prev, rep.data || [])); toast('失败节点已重试', 'ok') }, onError: e => toast(e instanceof Error ? e.message : '重试失败节点失败', 'error') })
  const loadCache = async () => {
    const [cf, rep] = await Promise.all([cfCache.refetch(), repCache.refetch()])
    setCfRows(cf.data?.data || [])
    setRepRows(rep.data?.data || [])
    toast('缓存结果已加载', 'ok')
  }
  useEffect(() => {
    void loadCache()
  }, [])

  const repByExitIp = useMemo(() => new Map(repRows.filter(r => r.exit_ip).map(r => [String(r.exit_ip), r])), [repRows])
  const repByPort = useMemo(() => new Map(repRows.filter(r => r.port != null).map(r => [String(r.port), r])), [repRows])
  const rows = useMemo<QualityRow[]>(() => {
    return cfRows
      .filter(r => filter === 'all' || r.level === filter)
      .map((r, idx) => {
        const rep = repByPort.get(String(r.port || '')) || (r.exit_ip ? repByExitIp.get(String(r.exit_ip)) : undefined)
        const repRisk = rep ? repLevel(rep) : '-'
        const cfScore = Number(r.score) || 0
        const latencyPenalty = Number(r.latency_ms) > 3000 ? 12 : Number(r.latency_ms) > 1000 ? 6 : Number(r.latency_ms) > 500 ? 3 : 0
        const score = Math.max(0, Math.min(100, Math.round(cfScore - riskPenalty(repRisk) - latencyPenalty)))
        return { key: `${r.node_tag || r.node_name || r.port || 'row'}-${idx}`, row: r, rep, repRisk, score }
      })
      .sort((a, b) => b.score - a.score || (Number(a.row.latency_ms) || 0) - (Number(b.row.latency_ms) || 0))
  }, [cfRows, filter, repByExitIp, repByPort])
  const proxyUrl = (row: CloudflareResult) => {
    const mp = (settings.data?.multi_port || {}) as Record<string, unknown>
    const host = String(row.host || mp.address || '127.0.0.1')
    const user = String(mp.username || '')
    const pass = String(mp.password || '')
    const auth = user || pass ? `${encodeURIComponent(user)}:${encodeURIComponent(pass)}@` : ''
    return `http://${auth}${host}:${row.port || ''}`
  }
  const extract = (row: CloudflareResult) => { setExtractorParams({ mode:'multi-port', region: (row.region || 'all') as never, format:'http_url', count:1, reveal:true }); setActiveTab('extractor'); toast('已带入代理提取页', 'ok') }
  const failedCount = cfRows.filter(failedCf).length + repRows.filter(row => repLevel(row) === 'failed' || !!row.error).length
  const columns: ColumnsType<QualityRow> = [
    { title: '节点', dataIndex: 'node', width: 220, fixed: 'left', render: (_, item) => <div><strong>{item.row.node_name || item.row.node_tag || '-'}</strong><br/><span className="muted mono">{item.row.node_tag || ''}</span></div> },
    { title: '地区/端口', width: 110, render: (_, item) => `${item.row.region || '-'}:${item.row.port || '-'}` },
    { title: '出口 IP', width: 150, render: (_, item) => item.row.exit_ip || '-' },
    { title: 'CF 分', width: 120, sorter: (a, b) => (Number(a.row.score) || 0) - (Number(b.row.score) || 0), render: (_, item) => <Badge tone={levelTone(item.row.level)}>{item.row.score ?? '-'} / {cfLabel(item.row.level)}</Badge> },
    { title: 'IP 风险', width: 130, render: (_, item) => <Badge tone={levelTone(item.repRisk)}>{item.repRisk}{item.rep ? ` / ${riskScore(item.rep)}` : ''}</Badge> },
    { title: '综合质量', width: 140, sorter: (a, b) => a.score - b.score, defaultSortOrder: 'descend', render: (_, item) => <Badge tone={qualityTone(item.score)}>{item.score} / {qualityLabel(item.score)}</Badge> },
    { title: '延迟', width: 100, sorter: (a, b) => (Number(a.row.latency_ms) || 0) - (Number(b.row.latency_ms) || 0), render: (_, item) => `${item.row.latency_ms || 0} ms` },
    { title: '操作', width: 190, fixed: 'right', render: (_, item) => <Space size={6}><Button variant="primary" onClick={() => navigator.clipboard.writeText(proxyUrl(item.row)).then(()=>toast('代理已复制','ok'))}>复制</Button><Button onClick={() => navigator.clipboard.writeText(`curl -x ${proxyUrl(item.row)} http://cp.cloudflare.com/generate_204`).then(()=>toast('curl 已复制','ok'))}>curl</Button><Button onClick={() => extract(item.row)}>提取</Button></Space> },
  ]
  return <div className="page quality-page">
    <div className="page-header"><div><h1>节点质量</h1><p>自动加载缓存，一键全量扫描，并按 CF 评分、IP 风险和综合质量筛选可用节点。</p></div></div>
    <div className="card quality-control-card">
      <div className="quality-control-head"><div><div className="panel-title">检测流程</div><div className="panel-subtitle">先选范围，再运行检测，最后筛选可用节点。</div></div><div className="quality-control-actions"><Button variant="primary" disabled={fullScan.isPending} onClick={() => fullScan.mutate()}>{fullScan.isPending ? '扫描中...' : '一键扫描全部节点'}</Button><Button disabled={retryScan.isPending} onClick={() => retryScan.mutate()}>{retryScan.isPending ? '重试中...' : '重试失败节点'}</Button><Button onClick={loadCache}>刷新缓存</Button><Button onClick={() => cfScan.mutate()}>抽样检测 CF</Button></div></div>
      <div className="quality-filter-grid modern-filter-grid">
        <div className="field console-field">
          <label>地区范围</label>
          <Select className="console-select" value={region} onChange={setRegion} options={[{ value: 'all', label: '全部' }, { value: 'us', label: '美国' }, { value: 'jp', label: '日本' }, { value: 'hk', label: '香港' }, { value: 'sg', label: '新加坡' }, { value: 'de', label: '德国' }, { value: 'gb', label: '英国' }]} />
        </div>
        <div className="field console-field">
          <label>样本数</label>
          <InputNumber className="console-number" min={1} value={count} onChange={value=>setCount(Number(value)||10)} />
        </div>
        <div className="field console-field">
          <label>结果筛选</label>
          <Select className="console-select" value={filter} onChange={setFilter} options={[{ value: 'all', label: '全部等级' }, { value: 'excellent', label: '优秀' }, { value: 'good', label: '良好' }, { value: 'fair', label: '一般' }, { value: 'poor', label: '较差' }, { value: 'failed', label: '失败' }]} />
        </div>
      </div>
    </div>
    <div className="summary-grid quality-summary-grid"><div className="metric"><div className="label">CF 优秀</div><div className="value success">{cfRows.filter(r=>r.level==='excellent').length}</div></div><div className="metric"><div className="label">高质量节点</div><div className="value success">{rows.filter(r => r.score >= 80).length}</div></div><div className="metric"><div className="label">低风险</div><div className="value success">{repRows.filter(r=>repLevel(r)==='low').length}</div></div><div className="metric"><div className="label">失败/高风险</div><div className="value error">{failedCount}</div></div></div>
    <div className="charts-grid quality-charts"><div className="chart-panel"><div className="chart-title">CF 评分分布 <span>Compatibility</span></div><CfDistributionChart rows={cfRows} /></div><div className="chart-panel"><div className="chart-title">IP 风险等级 <span>Reputation</span></div><ReputationRiskChart rows={repRows} /></div><div className="chart-panel wide compact-rank-chart"><div className="chart-title">CF 高分节点排行 <span>Top Scores</span></div><CfScoreRankChart rows={rows.slice(0, 10).map(item => item.row)} /></div></div>
    <div className="card quality-table-card"><div className="panel-header"><div><div className="panel-title">可用节点列表</div><div className="panel-subtitle">共 {rows.length} 条结果，默认每页 10 条；可按综合质量、CF 分和延迟排序。</div></div></div><Table className="quality-table" columns={columns} dataSource={rows} size="middle" scroll={{ x: 1260 }} pagination={{ pageSize: 10, showSizeChanger: true, pageSizeOptions: [10, 20, 50], showTotal: total => `共 ${total} 条` }} locale={{ emptyText: '暂无质量数据，请先检测或查看缓存。' }} /></div>
  </div>
}
