import { useEffect, useMemo, useState } from 'react'
import { InputNumber, Progress, Select, Space, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useMutation, useQuery } from '@tanstack/react-query'
import { getNodes, getNodesSummary } from '../api/nodes'
import { getCloudflareCache, checkCloudflare } from '../api/cloudflare'
import { getSettings } from '../api/settings'
import { getReputationCache } from '../api/reputation'
import { cancelQualityJob, createQualityJob, getQualityJob, getQualityJobResults } from '../api/qualityJobs'
import { Button } from '../components/ui/Button'
import { Badge } from '../components/ui/Badge'
import { CfDistributionChart, ReputationRiskChart, CfScoreRankChart } from '../components/charts/QualityCharts'
import { useToast } from '../components/ui/Toast'
import { useAppStore } from '../store/appStore'
import { useExtractorStore } from '../store/extractorStore'
import type { CloudflareResult } from '../types/cloudflare'
import type { ReputationResult } from '../types/reputation'
import type { QualityJobResult, QualityJobSnapshot } from '../types/qualityJob'

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
function cfFromJobRow(row: QualityJobResult): CloudflareResult {
  const cf = (row.cf || {}) as Record<string, unknown>
  return {
    node_name: row.node_name,
    node_tag: row.node_tag,
    region: row.region,
    host: row.host,
    port: row.port,
    exit_ip: String(cf.exit_ip || ''),
    cf_loc: String(cf.cf_loc || ''),
    http_204_ok: Boolean(cf.http_204_ok),
    trace_ok: Boolean(cf.trace_ok),
    score: Number(cf.score ?? row.score ?? 0),
    level: String(cf.level || (row.error ? 'failed' : 'good')),
    latency_ms: Number(cf.latency_ms ?? row.latency_ms ?? 0),
    error: String(cf.error || row.error || ''),
  }
}
function repFromJobRow(row: QualityJobResult): ReputationResult {
  const rep = (row.reputation || {}) as Record<string, unknown>
  return {
    node_name: row.node_name,
    node_tag: row.node_tag,
    region: row.region,
    port: row.port,
    risk_level: String(rep.risk_level || (row.error ? 'failed' : '-')),
    risk_score: Number(rep.risk_score || 0),
    country: String(rep.country_code || ''),
    error: String(rep.error || row.error || ''),
  }
}
function isTerminalJob(job?: QualityJobSnapshot) { return !!job && ['completed', 'failed', 'cancelled'].includes(job.status) }

type QualityRow = { key: string; row: CloudflareResult; rep?: ReputationResult; repRisk: string; score: number; tier?: string; pool?: string }

export function QualityPage() {
  const [region, setRegion] = useState('all')
  const [source, setSource] = useState('all')
  const [count, setCount] = useState(20)
  const [cfRows, setCfRows] = useState<CloudflareResult[]>([])
  const [repRows, setRepRows] = useState<ReputationResult[]>([])
  const [jobId, setJobId] = useState('')
  const [resultPage, setResultPage] = useState(1)
  const [resultPageSize, setResultPageSize] = useState(20)
  const [filter, setFilter] = useState('all')
  const [tierFilter, setTierFilter] = useState('all')
  const [poolFilter, setPoolFilter] = useState('all')
  const toast = useToast(s => s.show)
  const setActiveTab = useAppStore(s => s.setActiveTab)
  const setExtractorParams = useExtractorStore(s => s.setParams)
  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })
  const nodesQuery = useQuery({ queryKey: ['nodes'], queryFn: getNodes })
  const nodesSummary = useQuery({ queryKey: ['nodes-summary'], queryFn: getNodesSummary })
  const cfCache = useQuery({ queryKey: ['cf-cache'], queryFn: getCloudflareCache, enabled: false })
  const repCache = useQuery({ queryKey: ['rep-cache'], queryFn: getReputationCache, enabled: false })
  const jobQuery = useQuery({ queryKey: ['quality-job', jobId], queryFn: () => getQualityJob(jobId), enabled: !!jobId })
  const jobResults = useQuery({ queryKey: ['quality-job-results', jobId, resultPage, resultPageSize], queryFn: () => getQualityJobResults(jobId, { page: resultPage, page_size: resultPageSize }), enabled: !!jobId })
  const sourceStats = (nodesSummary.data?.source_stats || {}) as Record<string, number>
  const sourceCount = source === 'all' ? (nodesSummary.data?.total_nodes || nodesQuery.data?.length || 0) : Number(sourceStats[source] || 0)
  const scanCount = Math.max(1, count)
  const allCount = Math.max(sourceCount || nodesSummary.data?.total_nodes || nodesQuery.data?.length || 0, source === 'all' ? 500 : 1)
  const qualitySource = source === 'all' ? undefined : source
  const cfScan = useMutation({ mutationFn: () => checkCloudflare(region, scanCount, false, false, source), onSuccess: d => { setCfRows(d.data || []); toast('CF 检测完成', 'ok') }, onError: e => toast(e instanceof Error ? e.message : 'CF 检测失败', 'error') })
  const fullScan = useMutation({ mutationFn: () => createQualityJob({ kind: 'pipeline', region, mode: 'multi-port', source: qualitySource, count: allCount, include_unavailable: true }), onSuccess: job => { setJobId(job.job_id); setResultPage(1); toast('Pipeline 后台扫描任务已创建', 'ok') }, onError: e => toast(e instanceof Error ? e.message : '创建后台扫描失败', 'error') })
  const retryScan = useMutation({ mutationFn: () => createQualityJob({ kind: 'pipeline', region, mode: 'multi-port', source: qualitySource, count: allCount, include_unavailable: true, retry_failed: true, replace: true }), onSuccess: job => { setJobId(job.job_id); setResultPage(1); toast('失败节点 Pipeline 重试任务已创建', 'ok') }, onError: e => toast(e instanceof Error ? e.message : '创建重试任务失败', 'error') })
  const cancelScan = useMutation({ mutationFn: () => cancelQualityJob(jobId), onSuccess: () => { void jobQuery.refetch(); void jobResults.refetch(); toast('后台任务已取消', 'ok') }, onError: e => toast(e instanceof Error ? e.message : '取消任务失败', 'error') })
  const loadCache = async () => {
    const [cf, rep] = await Promise.all([cfCache.refetch(), repCache.refetch()])
    setCfRows(cf.data?.data || [])
    setRepRows(rep.data?.data || [])
    toast('缓存结果已加载', 'ok')
  }
  useEffect(() => {
    void loadCache()
  }, [])
  useEffect(() => {
    if (!jobId || isTerminalJob(jobQuery.data)) return
    const timer = window.setInterval(() => {
      void jobQuery.refetch()
      void jobResults.refetch()
    }, 1000)
    return () => window.clearInterval(timer)
  }, [jobId, jobQuery.data?.status, resultPage, resultPageSize])

  const jobRows = useMemo(() => jobResults.data?.data || [], [jobResults.data?.data])
  const jobCfRows = useMemo(() => jobRows.map(cfFromJobRow), [jobRows])
  const jobRepRows = useMemo(() => jobRows.map(repFromJobRow), [jobRows])
  const activeCfRows = jobId ? jobCfRows : cfRows
  const activeRepRows = jobId ? jobRepRows : repRows
  const summary = jobQuery.data?.summary
  const failedCount = jobId ? (jobQuery.data?.failed || 0) : cfRows.filter(failedCf).length + repRows.filter(row => repLevel(row) === 'failed' || !!row.error).length
  const repByExitIp = useMemo(() => new Map(activeRepRows.filter(r => r.exit_ip).map(r => [String(r.exit_ip), r])), [activeRepRows])
  const repByPort = useMemo(() => new Map(activeRepRows.filter(r => r.port != null).map(r => [String(r.port), r])), [activeRepRows])
  const rows = useMemo<QualityRow[]>(() => {
    const sourceRows = jobId ? activeCfRows : activeCfRows.filter(r => filter === 'all' || r.level === filter)
    const mapped = sourceRows
      .map((r, idx) => {
        const rep = repByPort.get(String(r.port || '')) || (r.exit_ip ? repByExitIp.get(String(r.exit_ip)) : undefined)
        const repRisk = rep ? repLevel(rep) : '-'
        const cfScore = Number(r.score) || 0
        const latencyPenalty = Number(r.latency_ms) > 3000 ? 12 : Number(r.latency_ms) > 1000 ? 6 : Number(r.latency_ms) > 500 ? 3 : 0
        const rawJob = jobRows[idx]
        const score = jobId && typeof rawJob?.final_score === 'number' ? Number(rawJob.final_score) : Math.max(0, Math.min(100, Math.round(cfScore - riskPenalty(repRisk) - latencyPenalty)))
        return { key: `${r.node_tag || r.node_name || r.port || 'row'}-${idx}`, row: r, rep, repRisk, score, tier: rawJob?.tier, pool: rawJob?.pool }
      })
    const filtered = mapped.filter(item => (tierFilter === 'all' || item.tier === tierFilter) && (poolFilter === 'all' || item.pool === poolFilter))
    return jobId ? filtered : filtered.sort((a, b) => b.score - a.score || (Number(a.row.latency_ms) || 0) - (Number(b.row.latency_ms) || 0))
  }, [activeCfRows, filter, jobId, poolFilter, repByExitIp, repByPort, jobRows, tierFilter])
  const proxyUrl = (row: CloudflareResult) => {
    const mp = (settings.data?.multi_port || {}) as Record<string, unknown>
    const host = String(row.host || mp.address || '127.0.0.1')
    const user = String(mp.username || '')
    const pass = String(mp.password || '')
    const auth = user || pass ? `${encodeURIComponent(user)}:${encodeURIComponent(pass)}@` : ''
    return `http://${auth}${host}:${row.port || ''}`
  }
  const extract = (row: CloudflareResult) => { setExtractorParams({ mode:'multi-port', region: (row.region || 'all') as never, format:'http_url', count:1, reveal:true }); setActiveTab('extractor'); toast('已带入代理提取页', 'ok') }
  const columns = useMemo<ColumnsType<QualityRow>>(() => [
    { title: '节点', dataIndex: 'node', width: 220, fixed: 'left', render: (_, item) => <div><strong>{item.row.node_name || item.row.node_tag || '-'}</strong><br/><span className="muted mono">{item.row.node_tag || ''}</span></div> },
    { title: '地区/端口', width: 110, render: (_, item) => `${item.row.region || '-'}:${item.row.port || '-'}` },
    { title: '出口 IP', width: 150, render: (_, item) => item.row.exit_ip || '-' },
    { title: 'CF 分', width: 120, sorter: jobId ? undefined : (a, b) => (Number(a.row.score) || 0) - (Number(b.row.score) || 0), render: (_, item) => <Badge tone={levelTone(item.row.level)}>{item.row.score ?? '-'} / {cfLabel(item.row.level)}</Badge> },
    { title: 'IP 风险', width: 130, render: (_, item) => <Badge tone={levelTone(item.repRisk)}>{item.repRisk}{item.rep ? ` / ${riskScore(item.rep)}` : ''}</Badge> },
    { title: 'Tier/池', width: 180, render: (_, item) => <div><Badge tone={qualityTone(item.score)}>{item.tier || '-'}</Badge><br /><span className="muted mono">{item.pool || '-'}</span></div> },
    { title: '综合质量', width: 140, sorter: jobId ? undefined : (a, b) => a.score - b.score, defaultSortOrder: jobId ? undefined : 'descend', render: (_, item) => <Badge tone={qualityTone(item.score)}>{item.score} / {qualityLabel(item.score)}</Badge> },
    { title: '延迟', width: 100, sorter: jobId ? undefined : (a, b) => (Number(a.row.latency_ms) || 0) - (Number(b.row.latency_ms) || 0), render: (_, item) => `${item.row.latency_ms || 0} ms` },
    { title: '操作', width: 190, fixed: 'right', render: (_, item) => <Space size={6}><Button variant="primary" onClick={() => navigator.clipboard.writeText(proxyUrl(item.row)).then(()=>toast('代理已复制','ok'))}>复制</Button><Button onClick={() => navigator.clipboard.writeText(`curl -x ${proxyUrl(item.row)} http://cp.cloudflare.com/generate_204`).then(()=>toast('curl 已复制','ok'))}>curl</Button><Button onClick={() => extract(item.row)}>提取</Button></Space> },
  ], [jobId, proxyUrl, toast, extract])
  return <div className="page quality-page">
    <div className="page-header"><div><h1>节点质量</h1><p>自动加载缓存，一键全量扫描，并按 CF 评分、IP 风险和综合质量筛选可用节点。</p></div></div>
    <div className="card quality-control-card">
      <div className="quality-control-head"><div><div className="panel-title">检测流程</div><div className="panel-subtitle">Pipeline 会先快速预筛，再只对可连通节点执行 CF/IP 风险深度检测。</div></div><div className="quality-control-actions"><Button variant="primary" disabled={fullScan.isPending || (!!jobId && !isTerminalJob(jobQuery.data))} onClick={() => fullScan.mutate()}>{fullScan.isPending ? '创建中...' : 'Pipeline 扫描全部节点'}</Button><Button disabled={retryScan.isPending || (!!jobId && !isTerminalJob(jobQuery.data))} onClick={() => retryScan.mutate()}>{retryScan.isPending ? '重试中...' : 'Pipeline 重试失败节点'}</Button><Button onClick={loadCache}>刷新缓存</Button><Button onClick={() => cfScan.mutate()}>抽样检测 CF</Button></div></div>
      <div className="quality-filter-grid modern-filter-grid">
        <div className="field console-field">
          <label>地区范围</label>
          <Select className="console-select" value={region} onChange={setRegion} options={[{ value: 'all', label: '全部' }, { value: 'us', label: '美国' }, { value: 'jp', label: '日本' }, { value: 'hk', label: '香港' }, { value: 'sg', label: '新加坡' }, { value: 'de', label: '德国' }, { value: 'gb', label: '英国' }]} />
        </div>
        <div className="field console-field">
          <label>节点来源</label>
          <Select className="console-select" value={source} onChange={setSource} options={[{ value: 'all', label: `全部来源 (${nodesSummary.data?.total_nodes || 0})` }, { value: 'free_proxy', label: `免费源 (${sourceStats.free_proxy || 0})` }, { value: 'subscription', label: `订阅源 (${sourceStats.subscription || 0})` }, { value: 'inline', label: `内联 (${sourceStats.inline || 0})` }, { value: 'nodes_file', label: `节点文件 (${sourceStats.nodes_file || 0})` }]} />
        </div>
        <div className="field console-field">
          <label>样本数</label>
          <InputNumber className="console-number" min={1} value={count} onChange={value=>setCount(Number(value)||10)} />
        </div>
        <div className="field console-field">
          <label>结果筛选</label>
          <Select className="console-select" value={filter} onChange={setFilter} disabled={!!jobId} options={[{ value: 'all', label: jobId ? '后台任务分页结果' : '全部等级' }, { value: 'excellent', label: '优秀' }, { value: 'good', label: '良好' }, { value: 'fair', label: '一般' }, { value: 'poor', label: '较差' }, { value: 'failed', label: '失败' }]} />
        </div>
        <div className="field console-field">
          <label>Tier 筛选</label>
          <Select className="console-select" value={tierFilter} onChange={setTierFilter} options={[{ value: 'all', label: '全部 Tier' }, { value: 'reject', label: 'T0 Reject' }, { value: 'rescue', label: 'T1 Rescue' }, { value: 'http_only', label: 'T2 HTTP-only' }, { value: 'simple_web', label: 'T3 Simple Web' }, { value: 'recommended', label: 'T4 Recommended' }, { value: 'premium', label: 'T5 Premium' }]} />
        </div>
        <div className="field console-field">
          <label>池筛选</label>
          <Select className="console-select" value={poolFilter} onChange={setPoolFilter} options={[{ value: 'all', label: '全部池' }, { value: 'reject_pool', label: 'reject_pool' }, { value: 'rescue_pool', label: 'rescue_pool' }, { value: 'http_pool', label: 'http_pool' }, { value: 'web_pool', label: 'web_pool' }, { value: 'recommended_pool', label: 'recommended_pool' }, { value: 'strict_pool', label: 'strict_pool' }]} />
        </div>
      </div>
      {jobId && <div className="card" style={{ marginTop: 16 }}>
        <div className="panel-header"><div><div className="panel-title">后台质量检测任务</div><div className="panel-subtitle">{jobId} · {jobQuery.data?.status || 'queued'} · {jobQuery.data?.completed || 0}/{jobQuery.data?.total || 0}</div></div><div className="toolbar"><Button disabled={isTerminalJob(jobQuery.data) || cancelScan.isPending} onClick={() => cancelScan.mutate()}>取消任务</Button><Button onClick={() => { void jobQuery.refetch(); void jobResults.refetch() }}>刷新进度</Button></div></div>
        <Progress percent={Math.round(jobQuery.data?.percent || 0)} status={jobQuery.data?.status === 'failed' ? 'exception' : jobQuery.data?.status === 'completed' ? 'success' : 'active'} />
      </div>}
    </div>
    <div className="summary-grid quality-summary-grid"><div className="metric"><div className="label">预筛通过</div><div className="value success">{summary?.quick?.ok ?? '-'}</div></div><div className="metric"><div className="label">最终推荐</div><div className="value success">{summary?.final?.recommend ?? rows.filter(r => r.score >= 75).length}</div></div><div className="metric"><div className="label">CF 优秀</div><div className="value success">{summary?.cloudflare?.excellent ?? activeCfRows.filter(r=>r.level==='excellent').length}</div></div><div className="metric"><div className="label">失败/高风险</div><div className="value error">{failedCount}</div></div></div>
    <div className="charts-grid quality-charts"><div className="chart-panel"><div className="chart-title">CF 评分分布 <span>{jobId ? 'Current Page' : 'Compatibility'}</span></div><CfDistributionChart rows={activeCfRows} /></div><div className="chart-panel"><div className="chart-title">IP 风险等级 <span>{jobId ? 'Current Page' : 'Reputation'}</span></div><ReputationRiskChart rows={activeRepRows} /></div><div className="chart-panel wide compact-rank-chart"><div className="chart-title">CF 高分节点排行 <span>{jobId ? 'Current Page' : 'Top Scores'}</span></div><CfScoreRankChart rows={rows.slice(0, 10).map(item => item.row)} /></div></div>
    <div className="card quality-table-card"><div className="panel-header"><div><div className="panel-title">可用节点列表</div><div className="panel-subtitle">{jobId ? `当前页 ${rows.length} 条 / 任务共 ${jobResults.data?.count || 0} 条；后台任务结果由服务端分页返回，不在前端二次筛选排序。` : `共 ${rows.length} 条结果。`}</div></div></div><Table className="quality-table" columns={columns} dataSource={rows} size="middle" scroll={{ x: 1260 }} pagination={jobId ? { current: resultPage, pageSize: resultPageSize, total: jobResults.data?.count || 0, showSizeChanger: true, pageSizeOptions: [10, 20, 50, 100], showTotal: total => `共 ${total} 条`, onChange: (page, pageSize) => { setResultPage(page); setResultPageSize(pageSize) } } : { pageSize: 10, showSizeChanger: true, pageSizeOptions: [10, 20, 50], showTotal: total => `共 ${total} 条` }} locale={{ emptyText: '暂无质量数据，请先检测或查看缓存。' }} /></div>
  </div>
}
