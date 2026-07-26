import { useEffect, useMemo, useState } from 'react'
import { InputNumber, Pagination, Progress, Select, Space, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useMutation, useQuery } from '@tanstack/react-query'
import { getNodes, getNodesSummary } from '../api/nodes'
import { getCloudflareCache, checkCloudflare } from '../api/cloudflare'
import { getSettings } from '../api/settings'
import { checkReputation, getReputationCache } from '../api/reputation'
import { cancelQualityJob, createQualityJob, getQualityJob, getQualityJobResults } from '../api/qualityJobs'
import { Button } from '../components/ui/Button'
import { QueryErrorBanner } from '../components/ui/QueryErrorBanner'
import { Badge } from '../components/ui/Badge'
import { CfDistributionChart, ReputationRiskChart, CfScoreRankChart, rankChartIsLatency } from '../components/charts/QualityCharts'
import { ChevronDown } from 'lucide-react'
import { REGION_META, regionMeta } from '../components/charts/region'
import { useToast } from '../components/ui/Toast'
import { useAppStore } from '../store/appStore'
import { useExtractorStore } from '../store/extractorStore'
import { useReload } from '../hooks/useReload'
import { copyToClipboard } from '../lib/clipboard'
import type { CloudflareResult } from '../types/cloudflare'
import type { ReputationRegionUpdateSummary, ReputationResult } from '../types/reputation'
import type { QualityJobResult, QualityJobSnapshot } from '../types/qualityJob'
import { Page } from '../components/layout/Page'

function levelTone(level?: string) { return level === 'excellent' || level === 'low' ? 'good' : level === 'good' || level === 'medium' ? 'warn' : level ? 'bad' : 'neutral' }
function cfLabel(level?: string) { return ({excellent:'优秀',good:'良好',fair:'一般',poor:'较差',failed:'失败'} as Record<string,string>)[level || ''] || '-' }
function repLevel(row: ReputationResult) { const r = row.result || row; return r.risk_level || (row.error ? 'failed' : '-') }
function qualityLabel(score: number) { return score >= 80 ? '高质量' : score >= 60 ? '可用' : score >= 40 ? '一般' : '不推荐' }
function qualityTone(score: number) { return score >= 80 ? 'good' : score >= 60 ? 'info' : score >= 40 ? 'warn' : 'bad' }
function riskPenalty(level?: string) { return level === 'low' ? 0 : level === 'medium' ? 18 : level === 'high' ? 36 : level === 'failed' ? 50 : 12 }
function riskScore(row?: ReputationResult) { const r = row?.result || row; return Number(r?.risk_score) || 0 }
function failedCf(row: CloudflareResult) { return row.level === 'failed' || !!row.error }
function sourceLabel(source?: string) {
  return ({
    all: '全部来源',
    free_proxy: '免费源',
    subscription: '订阅源',
    inline: '内联',
    nodes_file: '节点文件',
    unknown: '未知',
  } as Record<string, string>)[source || ''] || source || '-'
}
function rowKey(row: { node_tag?: string; port?: number; target_index?: number; node_name?: string; name?: string; host?: string; exit_ip?: string }) {
  return row.node_tag
    || (typeof row.target_index === 'number' ? `target:${row.target_index}` : '')
    || row.node_name
    || row.name
    || (row.host && row.port ? `${row.host}:${row.port}` : '')
    || (row.exit_ip && row.port ? `${row.exit_ip}:${row.port}` : '')
    || (row.port ? `port:${row.port}` : '')
    || `row:${JSON.stringify(row)}`
}
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
function safeRows<T>(rows: unknown): T[] {
  return Array.isArray(rows) ? rows : []
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
function regionUpdateText(summary?: ReputationRegionUpdateSummary | null) {
  if (!summary) return ''
  return `检查 ${summary.checked || 0} 个，更新 ${summary.updated || 0} 个，未变 ${summary.unchanged || 0} 个，跳过 ${summary.skipped || 0} 个，持久化 ${summary.persisted || 0} 个`
}

type QualityRow = { key: string; row: CloudflareResult; rep?: ReputationResult; repRisk: string; score: number; tier?: string; pool?: string; source?: string }
const QUALITY_REGION_OPTIONS = Object.entries(REGION_META).map(([value, meta]) => ({ value, label: meta.label }))

function reputationExitIp(row: ReputationResult) {
  return String(row.exit_ip || row.ip || '').trim()
}

function regionLabel(region?: string) {
  return regionMeta(region).label
}

export function QualityPage() {
  const [region, setRegion] = useState('all')
  const [source, setSource] = useState('all')
  const [count, setCount] = useState(20)
  const [regionUpdateSummary, setRegionUpdateSummary] = useState<ReputationRegionUpdateSummary | null>(null)
  const [cfRows, setCfRows] = useState<CloudflareResult[]>([])
  const [repRows, setRepRows] = useState<ReputationResult[]>([])
  const [jobId, setJobId] = useState('')
  const [terminalSyncedJobId, setTerminalSyncedJobId] = useState('')
  const [resultPage, setResultPage] = useState(1)
  const [resultPageSize, setResultPageSize] = useState(20)
  const [filter, setFilter] = useState('all')
  const [chartsOpen, setChartsOpen] = useState(false)
  const [tierFilter, setTierFilter] = useState('all')
  const [poolFilter, setPoolFilter] = useState('all')
  const [mobileResultPage, setMobileResultPage] = useState(1)
  const [mobileResultPageSize, setMobileResultPageSize] = useState(20)
  const toast = useToast(s => s.show)
  const setActiveTab = useAppStore(s => s.setActiveTab)
  const setExtractorParams = useExtractorStore(s => s.setParams)
  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })
  const nodesQuery = useQuery({ queryKey: ['nodes'], queryFn: getNodes })
  const nodesSummary = useQuery({ queryKey: ['nodes-summary'], queryFn: getNodesSummary })
  const cfCache = useQuery({ queryKey: ['cf-cache'], queryFn: getCloudflareCache, enabled: false })
  const repCache = useQuery({ queryKey: ['rep-cache'], queryFn: getReputationCache, enabled: false })
  const {
    needReload: needRegionReload,
    setNeedReload: setNeedRegionReload,
    reloadState: regionReloadState,
    startReload: regionReload,
    status: regionReloadStatusData,
  } = useReload({
    scope: 'quality-region',
    successMessage: status => status?.duration_ms ? `地区校准已重载入池（${status.duration_ms}ms）` : '地区校准已重载入池',
    startMessage: res => res.started ? '地区校准重载已启动' : '已有重载任务在运行',
    onSucceeded: () => { void nodesSummary.refetch() },
  })
  const jobQuery = useQuery({ queryKey: ['quality-job', jobId], queryFn: () => getQualityJob(jobId), enabled: !!jobId })
  const jobResults = useQuery({ queryKey: ['quality-job-results', jobId, resultPage, resultPageSize], queryFn: () => getQualityJobResults(jobId, { page: resultPage, page_size: resultPageSize }), enabled: !!jobId })
  const sourceStats = (nodesSummary.data?.source_stats || {}) as Record<string, number>
  const hasSummaryError = nodesSummary.isError && !nodesSummary.data
  const hasCacheError = (cfCache.isError && !cfCache.data) || (repCache.isError && !repCache.data)
  const sourceTotalLabel = hasSummaryError ? '未知' : String(nodesSummary.data?.total_nodes || 0)
  const sourceCountLabel = (key: string) => hasSummaryError ? '未知' : String(sourceStats[key] || 0)
  const sourceCount = source === 'all' ? (nodesSummary.data?.total_nodes || nodesQuery.data?.length || 0) : Number(sourceStats[source] || 0)
  const scanCount = Math.min(50, Math.max(1, count))
  const pipelineCount = sourceCount
  const hasPipelineTargets = pipelineCount > 0
  const jobRunning = !!jobId && !isTerminalJob(jobQuery.data)
  const cacheLoading = cfCache.isFetching || repCache.isFetching
  const jobProgressLoading = jobQuery.isFetching || jobResults.isFetching
  const canCreatePipeline = !nodesSummary.isLoading && !hasSummaryError && !jobRunning && hasPipelineTargets
  const canRetryPipeline = !nodesSummary.isLoading && !hasSummaryError && hasPipelineTargets
  const canRunSampleCheck = !nodesSummary.isLoading && !hasSummaryError && !jobRunning
  const selectedSourceLabel = sourceLabel(source)
  const scanAllLabel = nodesSummary.isLoading ? 'Pipeline 扫描节点（统计加载中）' : hasSummaryError ? 'Pipeline 扫描节点（数量未知）' : !hasPipelineTargets ? 'Pipeline 无可扫描节点' : `Pipeline 扫描 ${pipelineCount} 个节点`
  const pipelineTargetText = nodesSummary.isLoading ? '统计加载中' : hasSummaryError ? '数量未知' : `${selectedSourceLabel} · ${pipelineCount} 个目标`
  const sampleTargetText = nodesSummary.isLoading ? '统计加载中' : hasSummaryError ? '数量未知' : `${selectedSourceLabel} · 抽样 ${scanCount} 个`
  const qualitySource = source === 'all' ? undefined : source
  const showCacheMode = () => {
    setJobId('')
    setTerminalSyncedJobId('')
    setResultPage(1)
  }
  const cfScan = useMutation({ mutationFn: () => checkCloudflare(region, scanCount, false, false, source), onSuccess: d => { showCacheMode(); setCfRows(safeRows<CloudflareResult>(d.data)); toast('CF 检测完成', 'ok') }, onError: e => toast(e instanceof Error ? e.message : 'CF 检测失败', 'error') })
  const regionCalibrate = useMutation({
    mutationFn: () => checkReputation(region, scanCount, false, false, source, true),
    onSuccess: d => {
      showCacheMode()
      setRepRows(prev => mergeRepRows(prev, safeRows<ReputationResult>(d.data)))
      setRegionUpdateSummary(d.region_updates || null)
      setNeedRegionReload(Boolean(d.region_updates?.need_reload))
      void nodesSummary.refetch()
      toast(d.region_updates?.need_reload ? '出口地区校准完成，需要重载入池' : '出口地区校准完成', 'ok')
    },
    onError: e => toast(e instanceof Error ? e.message : '出口地区校准失败', 'error'),
  })
  const fullScan = useMutation({ mutationFn: () => createQualityJob({ kind: 'pipeline', region, mode: 'multi-port', source: qualitySource, count: pipelineCount, include_unavailable: true }) })
  const retryScan = useMutation({ mutationFn: () => createQualityJob({ kind: 'pipeline', region, mode: 'multi-port', source: qualitySource, count: pipelineCount, include_unavailable: true, retry_failed: true, replace: true }) })
  const cancelScan = useMutation({ mutationFn: () => cancelQualityJob(jobId), onSuccess: () => { void jobQuery.refetch(); void jobResults.refetch(); toast('后台任务已取消', 'ok') }, onError: e => toast(e instanceof Error ? e.message : '取消任务失败', 'error') })
  const startQualityJob = async (retryFailed = false) => {
    const mutation = retryFailed ? retryScan : fullScan
    try {
      const job = await mutation.mutateAsync()
      if (!job?.job_id) throw new Error('后台任务响应缺少 job_id')
      setJobId(job.job_id)
      setTerminalSyncedJobId('')
      setResultPage(1)
      toast(retryFailed ? '失败节点 Pipeline 重试任务已创建' : 'Pipeline 后台扫描任务已创建', 'ok')
    } catch (e) {
      toast(e instanceof Error ? e.message : retryFailed ? '创建重试任务失败' : '创建后台扫描失败', 'error')
    }
  }
  const loadCache = async () => {
    try {
      const [cf, rep] = await Promise.all([cfCache.refetch(), repCache.refetch()])
      const failed = [cf.error, rep.error].find(Boolean)
      if (failed) throw failed
      showCacheMode()
      setCfRows(safeRows<CloudflareResult>(cf.data?.data))
      setRepRows(safeRows<ReputationResult>(rep.data?.data))
      toast('缓存结果已加载', 'ok')
    } catch (e) {
      toast(e instanceof Error ? `加载质量缓存失败：${e.message}` : '加载质量缓存失败', 'error')
    }
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
  useEffect(() => {
    if (!jobId || terminalSyncedJobId === jobId || !isTerminalJob(jobQuery.data)) return
    setTerminalSyncedJobId(jobId)
    void Promise.all([cfCache.refetch(), repCache.refetch()])
      .then(([cf, rep]) => {
        setCfRows(prev => mergeCfRows(prev, safeRows<CloudflareResult>(cf.data?.data)))
        setRepRows(prev => mergeRepRows(prev, safeRows<ReputationResult>(rep.data?.data)))
        if (jobQuery.data?.status === 'completed') toast('后台任务结果已同步到质量缓存', 'ok')
      })
      .catch(e => toast(e instanceof Error ? `质量缓存同步失败：${e.message}` : '质量缓存同步失败', 'error'))
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [jobId, terminalSyncedJobId, jobQuery.data?.status])

  const jobRows = useMemo(() => safeRows<QualityJobResult>(jobResults.data?.data), [jobResults.data?.data])
  const jobCfRows = useMemo(() => jobRows.map(cfFromJobRow), [jobRows])
  const jobRepRows = useMemo(() => jobRows.map(repFromJobRow), [jobRows])
  const jobMetaByKey = useMemo(() => new Map(jobRows.map(row => [rowKey(cfFromJobRow(row)), row])), [jobRows])
  const activeCfRows = safeRows<CloudflareResult>(jobId ? jobCfRows : cfRows)
  const activeRepRows = safeRows<ReputationResult>(jobId ? jobRepRows : repRows)
  const summary = jobQuery.data?.summary
  const failedCount = jobId ? (jobQuery.data?.failed || 0) : cfRows.filter(failedCf).length + repRows.filter(row => repLevel(row) === 'failed' || !!row.error).length
  const repByExitIp = useMemo(() => new Map<string, ReputationResult>(activeRepRows.flatMap(r => {
    const ip = reputationExitIp(r)
    return ip ? [[ip, r] as [string, ReputationResult]] : []
  })), [activeRepRows])
  const repByPort = useMemo(() => new Map(activeRepRows.filter(r => r.port != null).map(r => [String(r.port), r])), [activeRepRows])
  const rows = useMemo<QualityRow[]>(() => {
    const sourceRows = jobId ? activeCfRows : activeCfRows.filter(r => filter === 'all' || r.level === filter)
    const mapped = sourceRows
      .map((r, idx) => {
        const rep = repByPort.get(String(r.port || '')) || (r.exit_ip ? repByExitIp.get(String(r.exit_ip)) : undefined)
        const repRisk = rep ? repLevel(rep) : '-'
        const cfScore = Number(r.score) || 0
        const latencyPenalty = Number(r.latency_ms) > 3000 ? 12 : Number(r.latency_ms) > 1000 ? 6 : Number(r.latency_ms) > 500 ? 3 : 0
        const rawJob = jobMetaByKey.get(rowKey(r))
        const score = jobId && typeof rawJob?.final_score === 'number' ? Number(rawJob.final_score) : Math.max(0, Math.min(100, Math.round(cfScore - riskPenalty(repRisk) - latencyPenalty)))
        return { key: rowKey(r), row: r, rep, repRisk, score, tier: rawJob?.tier, pool: rawJob?.pool, source: rawJob?.source || (jobId ? undefined : source) }
      })
    const filtered = mapped.filter(item => (tierFilter === 'all' || item.tier === tierFilter) && (poolFilter === 'all' || item.pool === poolFilter))
    return jobId ? filtered : filtered.sort((a, b) => b.score - a.score || (Number(a.row.latency_ms) || 0) - (Number(b.row.latency_ms) || 0))
  }, [activeCfRows, filter, jobId, poolFilter, repByExitIp, repByPort, jobMetaByKey, source, tierFilter])
  useEffect(() => {
    setMobileResultPage(1)
  }, [filter, tierFilter, poolFilter, source, region, jobId])
  const mobilePage = jobId ? resultPage : mobileResultPage
  const mobilePageSize = jobId ? resultPageSize : mobileResultPageSize
  const mobileTotal = jobId ? (jobResults.data?.count || rows.length) : rows.length
  const mobileRows = jobId ? rows : rows.slice((mobileResultPage - 1) * mobileResultPageSize, mobileResultPage * mobileResultPageSize)
  const onMobilePageChange = (page: number, pageSize: number) => {
    if (jobId) {
      setResultPage(page)
      setResultPageSize(pageSize)
      return
    }
    setMobileResultPage(page)
    setMobileResultPageSize(pageSize)
  }
  const proxyUrl = (row: CloudflareResult) => {
    const mp = (settings.data?.multi_port || {}) as Record<string, unknown>
    const host = String(row.host || mp.address || '127.0.0.1')
    const user = String(mp.username || '')
    const pass = String(mp.password || '')
    const auth = user || pass ? `${encodeURIComponent(user)}:${encodeURIComponent(pass)}@` : ''
    return `http://${auth}${host}:${row.port || ''}`
  }
  const extract = (row: CloudflareResult) => {
    setExtractorParams({ mode:'multi-port', region: (row.region || 'all') as never, format:'http_url', count:1, reveal:true })
    setActiveTab('extractor')
    window.history.replaceState(null, '', '#extractor')
    toast('已带入代理提取页', 'ok')
  }
  const rankIsLatency = rankChartIsLatency(rows.slice(0, 10).map(item => item.row))
  const columns = useMemo<ColumnsType<QualityRow>>(() => [
    { title: '节点', dataIndex: 'node', width: 210, fixed: 'left', render: (_, item) => <div className="q-node"><strong title={String(item.row.node_name || '')}>{item.row.node_name || item.row.node_tag || '-'}</strong><span className="muted mono">{item.row.node_tag || ''}</span></div> },
    // Long region names ("中国香港特别行政区") wrap and double the row height.
    { title: '地区/端口', width: 132, render: (_, item) => <span className="q-region" title={`${regionLabel(item.row.region)}:${item.row.port || '-'}`}>{regionLabel(item.row.region)}<em>:{item.row.port || '-'}</em></span> },
    // IPv6 exit addresses wrap to three lines and blow up the row height.
    { title: '出口 IP', width: 140, render: (_, item) => <span className="mono uri-clip q-ip" title={String(item.row.exit_ip || '')}>{item.row.exit_ip || '-'}</span> },
    { title: 'CF 分', width: 110, sorter: jobId ? undefined : (a, b) => (Number(a.row.score) || 0) - (Number(b.row.score) || 0), render: (_, item) => <Badge tone={levelTone(item.row.level)}>{item.row.score ?? '-'} / {cfLabel(item.row.level)}</Badge> },
    // Unmeasured cells stay plain text — a badge with a status dot implies a
    // verdict that was never computed.
    { title: 'IP 风险', width: 110, render: (_, item) => item.rep ? <Badge tone={levelTone(item.repRisk)}>{item.repRisk} / {riskScore(item.rep)}</Badge> : <span className="muted">未检测</span> },
    { title: 'Tier/池', width: 150, render: (_, item) => item.tier || item.pool
      ? <div className="q-tier"><Badge tone={qualityTone(item.score)}>{item.tier || '-'}</Badge>{item.pool ? <span className="muted mono">{item.pool}</span> : null}</div>
      : <span className="muted">—</span> },
    { title: '综合质量', width: 130, sorter: jobId ? undefined : (a, b) => a.score - b.score, defaultSortOrder: jobId ? undefined : 'descend', render: (_, item) => <Badge tone={qualityTone(item.score)}>{item.score} / {qualityLabel(item.score)}</Badge> },
    { title: '延迟', width: 100, sorter: jobId ? undefined : (a, b) => (Number(a.row.latency_ms) || 0) - (Number(b.row.latency_ms) || 0), render: (_, item) => <span className="q-lat">{item.row.latency_ms || 0} ms</span> },
    { title: '操作', width: 206, fixed: 'right', render: (_, item) => <Space size={6}><Button variant="primary" onClick={() => { void copyToClipboard(proxyUrl(item.row), toast, '代理已复制') }}>复制</Button><Button onClick={() => { void copyToClipboard(`curl -x ${proxyUrl(item.row)} http://cp.cloudflare.com/generate_204`, toast, 'curl 已复制') }}>curl</Button><Button onClick={() => extract(item.row)}>提取</Button></Space> },
  ], [jobId, proxyUrl, toast, extract])
  return <Page
    className="quality-page"
    title="节点质量"
    description="按 CF 评分、IP 风险和综合质量筛选可用节点。"
    stats={[
      { label: '推荐', value: hasCacheError && !jobId ? '-' : summary?.final?.recommend ?? rows.filter(r => r.score >= 75).length },
      { label: 'CF 优秀', value: hasCacheError && !jobId ? '-' : summary?.cloudflare?.excellent ?? activeCfRows.filter(r => r.level === 'excellent').length },
      { label: '失败/高风险', value: hasCacheError && !jobId ? '-' : failedCount },
    ]}
    actions={
      <>
        <Button disabled={cacheLoading} onClick={loadCache}>{cacheLoading ? '加载中...' : '刷新缓存'}</Button>
        <Button variant="primary" disabled={!canCreatePipeline || fullScan.isPending} onClick={() => { void startQualityJob(false) }}>{fullScan.isPending ? '创建中...' : scanAllLabel}</Button>
      </>
    }
  >
    {settings.isError && <QueryErrorBanner title="设置加载失败" error={settings.error} onRetry={() => { void settings.refetch() }} />}
    {nodesQuery.isError && <QueryErrorBanner title="节点清单加载失败" error={nodesQuery.error} onRetry={() => { void nodesQuery.refetch() }} />}
    {nodesSummary.isError && <QueryErrorBanner title="节点统计加载失败" error={nodesSummary.error} onRetry={() => { void nodesSummary.refetch() }} />}
    {cfCache.isError && <QueryErrorBanner title="CF 缓存加载失败" error={cfCache.error} onRetry={() => { void cfCache.refetch() }} />}
    {repCache.isError && <QueryErrorBanner title="IP 风险缓存加载失败" error={repCache.error} onRetry={() => { void repCache.refetch() }} />}
    {jobId && jobQuery.isError && <QueryErrorBanner title="后台任务状态加载失败" error={jobQuery.error} onRetry={() => { void jobQuery.refetch() }} />}
    {jobId && jobResults.isError && <QueryErrorBanner title="后台任务结果加载失败" error={jobResults.error} onRetry={() => { void jobResults.refetch() }} />}
    <section className="sec sec-flush quality-scan-sec">
      <div className="sec-head">
        <h2>扫描</h2>
        <span className="sec-desc">{pipelineTargetText}；样本操作仅抽 {count} 个（{sampleTargetText}）。</span>
      </div>
      <div className="ftoolbar">
        <Select className="console-select f-select" aria-label="地区范围" value={region} onChange={setRegion} options={QUALITY_REGION_OPTIONS} />
        <Select className="console-select f-select" aria-label="节点来源" value={source} onChange={setSource} options={[{ value: 'all', label: `全部来源 (${sourceTotalLabel})` }, { value: 'free_proxy', label: `免费源 (${sourceCountLabel('free_proxy')})` }, { value: 'subscription', label: `订阅源 (${sourceCountLabel('subscription')})` }, { value: 'inline', label: `内联 (${sourceCountLabel('inline')})` }, { value: 'nodes_file', label: `节点文件 (${sourceCountLabel('nodes_file')})` }]} />
        <InputNumber className="console-number quality-count-input" aria-label="样本数" min={1} max={50} value={count} onChange={value=>setCount(Math.min(50, Math.max(1, Number(value)||10)))} />
        <span className="f-end">
          <Button title={`同步抽样 ${count} 个节点检测 CF 兼容性，不影响 Pipeline 全量结果`} disabled={!canRunSampleCheck || cfScan.isPending} onClick={() => cfScan.mutate()}>{cfScan.isPending ? '检测中...' : '抽样检测 CF'}</Button>
          <Button title={`按出口 IP 校准 ${count} 个抽样节点的地区并写入持久化`} disabled={!canRunSampleCheck || regionCalibrate.isPending} onClick={() => regionCalibrate.mutate()}>{regionCalibrate.isPending ? '校准中...' : '出口校准地区'}</Button>
          <Button title={needRegionReload ? '让校准后的地区进入对应地区池' : '暂无待生效的地区校准'} disabled={!needRegionReload || regionReload.isPending || regionReloadState === 'reloading'} onClick={() => regionReload.mutate()}>{regionReloadState === 'reloading' ? '重载中...' : '重载入池'}</Button>
          <Button title="只对上次 Pipeline 失败的节点重新扫描" disabled={!canRetryPipeline || retryScan.isPending} onClick={() => { void startQualityJob(true) }}>{retryScan.isPending ? '重试中...' : jobRunning ? '替换任务重试失败' : '重试失败节点'}</Button>
        </span>
      </div>
      {regionUpdateSummary && <div className="settings-alert modern-settings-alert settings-reload-alert" role="status" style={{ marginTop: 16 }}><div><strong>出口地区校准结果</strong><span>{regionUpdateText(regionUpdateSummary)}{needRegionReload ? '；需要重载入池后地区池才完全生效。' : '；当前无需重载。'}</span></div>{needRegionReload && <Button variant="primary" disabled={regionReload.isPending || regionReloadState === 'reloading'} onClick={() => regionReload.mutate()}>{regionReloadState === 'reloading' ? '重载中...' : '立即重载入池'}</Button>}</div>}
      {regionReloadState !== 'idle' && <div className="settings-alert modern-settings-alert settings-reload-alert" role="status" style={{ marginTop: 12 }}><div><strong>{regionReloadState === 'reloading' ? '代理核心正在后台重载' : '代理核心重载失败'}</strong><span>{regionReloadState === 'reloading' ? `已运行 ${Math.floor(Number(regionReloadStatusData?.elapsed_ms || 0) / 1000)} 秒，完成后会自动清除待重载状态。` : regionReloadStatusData?.error || '请检查日志后重试。'}</span></div></div>}
      {jobId && <div className="quality-job-strip">
        <div className="quality-job-meta">
          <strong>后台任务</strong>
          <span className="mono muted">{jobId}</span>
          <span className="muted">{jobQuery.data?.status || 'queued'} · {jobQuery.data?.completed || 0}/{jobQuery.data?.total || 0}</span>
        </div>
        <div className="quality-job-progress"><Progress percent={Math.round(jobQuery.data?.percent || 0)} status={jobQuery.data?.status === 'failed' ? 'exception' : jobQuery.data?.status === 'completed' ? 'success' : 'active'} /></div>
        <div className="toolbar">
          <Button disabled={isTerminalJob(jobQuery.data) || cancelScan.isPending} onClick={() => cancelScan.mutate()}>{cancelScan.isPending ? '取消中...' : '取消任务'}</Button>
          <Button disabled={jobProgressLoading} onClick={() => { void jobQuery.refetch(); void jobResults.refetch() }}>{jobProgressLoading ? '刷新中...' : '刷新进度'}</Button>
        </div>
      </div>}
    </section>
    <section className="sec quality-table-card">
      <div className="ftoolbar">
        <Select className="console-select f-select" aria-label="结果筛选" value={filter} onChange={setFilter} disabled={!!jobId} options={[{ value: 'all', label: jobId ? '后台任务分页结果' : '全部等级' }, { value: 'excellent', label: '优秀' }, { value: 'good', label: '良好' }, { value: 'fair', label: '一般' }, { value: 'poor', label: '较差' }, { value: 'failed', label: '失败' }]} />
        <Select className="console-select f-select" aria-label="Tier 筛选" value={tierFilter} onChange={setTierFilter} options={[{ value: 'all', label: '全部 Tier' }, { value: 'reject', label: 'T0 Reject' }, { value: 'rescue', label: 'T1 Rescue' }, { value: 'http_only', label: 'T2 HTTP-only' }, { value: 'simple_web', label: 'T3 Simple Web' }, { value: 'recommended', label: 'T4 Recommended' }, { value: 'premium', label: 'T5 Premium' }]} />
        <Select className="console-select f-select" aria-label="池筛选" value={poolFilter} onChange={setPoolFilter} options={[{ value: 'all', label: '全部池' }, { value: 'reject_pool', label: 'reject_pool' }, { value: 'rescue_pool', label: 'rescue_pool' }, { value: 'http_pool', label: 'http_pool' }, { value: 'web_pool', label: 'web_pool' }, { value: 'recommended_pool', label: 'recommended_pool' }, { value: 'strict_pool', label: 'strict_pool' }]} />
        <span className="f-end">{jobId ? `当前页 ${rows.length} / 共 ${jobResults.data?.count || 0} 条` : `${rows.length} 条结果`}</span>
      </div>
      <div className="quality-table-desktop">
        <Table className="quality-table" columns={columns} dataSource={rows} size="middle" scroll={{ x: 1300 }} pagination={jobId ? { current: resultPage, pageSize: resultPageSize, total: jobResults.data?.count || 0, size: 'small', showSizeChanger: true, pageSizeOptions: [10, 20, 50, 100], showTotal: (total, range) => `第 ${range[0]}-${range[1]} 条 / 共 ${total} 条`, onChange: (page, pageSize) => { setResultPage(page); setResultPageSize(pageSize) } } : { pageSize: 10, size: 'small', showSizeChanger: true, pageSizeOptions: [10, 20, 50], showTotal: (total, range) => `第 ${range[0]}-${range[1]} 条 / 共 ${total} 条` }} locale={{ emptyText: jobResults.isError ? '任务结果接口失败，请先重试。' : hasCacheError ? '质量缓存加载失败，请先重试。' : '暂无质量数据，请先检测或查看缓存。' }} />
      </div>
      <div className="quality-mobile-list" aria-label="移动端质量卡片列表">
        {mobileRows.length ? mobileRows.map(item => (
          <article className="node-card quality-node-card" key={`${item.key}-quality-card`}>
            <div className="node-card-head">
              <div>
                <strong>{item.row.node_name || item.row.node_tag || '-'}</strong>
                <span className="mono">{item.row.node_tag || ''}</span>
              </div>
              <Badge tone={qualityTone(item.score)}>{item.score} / {qualityLabel(item.score)}</Badge>
            </div>
            <div className="node-card-meta quality-card-meta">
              <div><span>地区 / 端口</span><strong>{regionLabel(item.row.region)}:{item.row.port || '-'}</strong></div>
              <div><span>来源</span><strong>{sourceLabel(item.source)}</strong></div>
              <div><span>出口 IP</span><strong>{item.row.exit_ip || '-'}</strong></div>
              <div><span>CF 分</span><strong>{item.row.score ?? '-'} / {cfLabel(item.row.level)}</strong></div>
              <div><span>IP 风险</span><strong>{item.repRisk}{item.rep ? ` / ${riskScore(item.rep)}` : ''}</strong></div>
              <div><span>Tier / 池</span><strong>{item.tier || '-'} / {item.pool || '-'}</strong></div>
              <div><span>延迟</span><strong>{item.row.latency_ms || 0} ms</strong></div>
              <div><span>出口地区</span><strong>{item.row.cf_loc || item.rep?.country || '-'}</strong></div>
            </div>
            <div className="node-card-foot quality-card-foot">
              <span>{item.row.error ? `错误：${item.row.error}` : `代理 ${proxyUrl(item.row)}`}</span>
              <div className="quality-card-actions">
                <Button variant="primary" onClick={() => { void copyToClipboard(proxyUrl(item.row), toast, '代理已复制') }}>复制</Button>
                <Button onClick={() => { void copyToClipboard(`curl -x ${proxyUrl(item.row)} http://cp.cloudflare.com/generate_204`, toast, 'curl 已复制') }}>curl</Button>
                <Button onClick={() => extract(item.row)}>提取</Button>
              </div>
            </div>
          </article>
        )) : <div className="empty-state compact-empty"><strong>{jobProgressLoading || cacheLoading ? '加载中...' : '暂无质量数据'}</strong><span>{jobResults.isError ? '任务结果接口失败，请先重试。' : hasCacheError ? '质量缓存加载失败，请先重试。' : '先刷新缓存、抽样检测或启动 Pipeline。'}</span></div>}
      </div>
      {mobileTotal > 0 && <div className="quality-mobile-pagination">
        <Pagination
          size="small"
          current={mobilePage}
          pageSize={mobilePageSize}
          total={mobileTotal}
          showSizeChanger
          pageSizeOptions={[10, 20, 50, 100]}
          showTotal={(total, range) => `第 ${range[0]}-${range[1]} 条 / 共 ${total} 条`}
          onChange={onMobilePageChange}
        />
      </div>}
    </section>
    {(activeCfRows.length > 0 || activeRepRows.length > 0) && <section className="sec quality-charts-sec">
      <button
        type="button"
        className="sec-toggle"
        onClick={() => setChartsOpen(v => !v)}
        aria-expanded={chartsOpen}
      >
        <ChevronDown size={15} className={chartsOpen ? 'is-open' : ''} />
        <span className="sec-title">分布概览</span>
        <span className="sec-desc">{chartsOpen ? '收起图表' : `CF 评分 · IP 风险 · ${rankIsLatency ? '延迟排行' : '高分排行'}`}</span>
      </button>
      {chartsOpen && <div className="charts-grid quality-charts">
        <div className="chart-panel"><div className="chart-title">CF 评分分布 <span>{jobId ? 'Current Page' : 'Compatibility'}</span></div><CfDistributionChart rows={activeCfRows} /></div>
        <div className="chart-panel"><div className="chart-title">IP 风险等级 <span>{jobId ? 'Current Page' : 'Reputation'}</span></div><ReputationRiskChart rows={activeRepRows} /></div>
        <div className="chart-panel wide compact-rank-chart"><div className="chart-title">{rankIsLatency ? '最快节点排行' : 'CF 高分节点排行'} <span>{rankIsLatency ? 'By Latency' : 'Top Scores'}</span></div><CfScoreRankChart rows={rows.slice(0, 10).map(item => item.row)} /></div>
      </div>}
    </section>}
  </Page>
}