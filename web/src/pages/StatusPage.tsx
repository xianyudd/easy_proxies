import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getNodesPage, getNodesSummary } from '../api/nodes'
import { getDebugSummary } from '../api/logs'
import { Badge } from '../components/ui/Badge'
import { QueryErrorBanner } from '../components/ui/QueryErrorBanner'
import { RegionAvailabilityChart, LatencyTopChart, TrafficTrendChart, FailureRankChart } from '../components/charts/NodeCharts'
import { regionMeta } from '../components/charts/region'
import type { NodeSnapshot } from '../types/node'

interface DebugNode { failure_count?: number; success_count?: number }
interface DebugResponse { success_rate?: number; nodes?: DebugNode[]; total_calls?: number; total_success?: number }

function latencyLabel(value: unknown) {
  const ms = Number(value)
  return Number.isFinite(ms) && ms >= 0 ? `${ms} ms` : '未测速'
}

function safeCount(input: unknown) {
  const value = Number(input)
  return Number.isFinite(value) && value >= 0 ? Math.trunc(value) : 0
}

function safeRate(input: unknown) {
  const value = Number(input)
  if (!Number.isFinite(value) || value < 0) return 0
  return Math.min(100, value)
}

function regionLabel(region?: string) {
  return regionMeta(region).label
}

const STATUS_PAGE_SIZE = 500

async function getAllStatusNodes() {
  let page = 1
  let firstPage: Awaited<ReturnType<typeof getNodesPage>> | null = null
  const nodes: NodeSnapshot[] = []
  while (page <= 1000) {
    const current = await getNodesPage({ availability: 'all', page, page_size: STATUS_PAGE_SIZE })
    if (!firstPage) firstPage = current
    nodes.push(...current.nodes)
    if (!current.has_next) {
      return { ...firstPage, page: current.page, has_next: current.has_next, nodes }
    }
    page += 1
  }
  return { ...(firstPage || await getNodesPage({ availability: 'all', page_size: STATUS_PAGE_SIZE })), nodes }
}

export function StatusPage() {
  const nodes = useQuery({ queryKey:['status-nodes-all'], queryFn:getAllStatusNodes, refetchInterval:10000 })
  const summary = useQuery({ queryKey:['nodes-summary'], queryFn:getNodesSummary, refetchInterval:10000 })
  const debug = useQuery({ queryKey:['debug-summary'], queryFn:() => getDebugSummary() as Promise<DebugResponse>, refetchInterval:10000 })
  const data = nodes.data?.nodes || []
  const summaryData = summary.data
  const dataUnavailable = (nodes.isError && !nodes.data) || (summary.isError && !summary.data)
  const healthyTotal = Object.values(summaryData?.region_healthy || {}).reduce((sum, count) => sum + safeCount(count), 0)
  const totalNodes = safeCount(summaryData?.total_nodes || nodes.data?.total_nodes || data.length)
  const stats = useMemo(() => ({
    total: dataUnavailable ? null : totalNodes,
    healthy: dataUnavailable ? null : healthyTotal || data.filter(n=>n.available&&!n.blacklisted).length,
    bad: dataUnavailable ? null : Math.max(0, totalNodes - (healthyTotal || data.filter(n=>n.available&&!n.blacklisted).length)),
    conn:data.reduce((s,n)=>s+safeCount(n.active_connections),0),
    successRate: debug.isError && !debug.data ? null : safeRate(debug.data?.success_rate),
  }), [data, dataUnavailable, debug.data, debug.isError, healthyTotal, totalNodes])
  const healthRate = stats.total && stats.healthy !== null ? Math.round((stats.healthy / stats.total) * 100) : null
  const regions = Object.entries(summaryData?.region_stats && Object.keys(summaryData.region_stats).length ? summaryData.region_stats : data.reduce((m,n)=>{ const r=String(n.region||'other'); m[r]=(m[r]||0)+1; return m }, {} as Record<string,number>)).sort((a,b)=>b[1]-a[1])
  const recentBad = data.filter(n=>n.blacklisted || (Number(n.failure_count)||0)>0).slice(0,8)
  return <div className="page">
    <div className="page-header"><div><h1>运行状态</h1><p>把整体健康度、关键趋势和异常节点放在同一监控视图里，优先定位需要处理的问题。</p></div></div>
    {nodes.isError && <QueryErrorBanner title="节点状态加载失败" error={nodes.error} onRetry={() => { void nodes.refetch() }} />}
    {summary.isError && <QueryErrorBanner title="节点统计加载失败" error={summary.error} onRetry={() => { void summary.refetch() }} />}
    {debug.isError && <QueryErrorBanner title="调试摘要加载失败" error={debug.error} onRetry={() => { void debug.refetch() }} />}
    <div className="status-hero">
      <div className="insight-panel">
        <div className="panel-title">整体健康状态</div>
        <div className="panel-subtitle">节点可用率 / Success telemetry</div>
        <div className="health-score">{healthRate === null ? '--' : `${healthRate}%`}</div>
        <div className="mini-grid"><div className="mini-stat"><span>总节点</span><strong>{stats.total ?? '-'}</strong></div><div className="mini-stat"><span>可用</span><strong>{stats.healthy ?? '-'}</strong></div><div className="mini-stat"><span>异常</span><strong>{stats.bad ?? '-'}</strong></div></div>
      </div>
      <div className="summary-grid"><div className="metric"><div className="label">成功率</div><div className="value">{stats.successRate === null ? '-' : `${stats.successRate.toFixed(1)}%`}</div></div><div className="metric"><div className="label">活跃连接</div><div className="value">{dataUnavailable ? '-' : stats.conn}</div></div><div className="metric"><div className="label">地区数</div><div className="value">{dataUnavailable ? '-' : regions.length}</div></div><div className="metric"><div className="label">最近异常</div><div className="value error">{dataUnavailable ? '-' : recentBad.length}</div></div></div>
    </div>
    <div className="dashboard-grid">
      <div className="dashboard-stack">
        <div className="chart-panel wide"><div className="chart-title">地域连通率 <span>Region Availability</span></div><RegionAvailabilityChart nodes={data} /></div>
        <div className="charts-grid">
          <div className="chart-panel"><div className="chart-title">最优节点延迟 <span>Top Fastest Nodes</span></div><LatencyTopChart nodes={data} /></div>
          <div className="chart-panel"><div className="chart-title">稳定性掉线排行 <span>Top Unstable Nodes</span></div><FailureRankChart nodes={data} /></div>
        </div>
        <div className="chart-panel wide"><div className="chart-title">实时流量带宽 <span>Real-time Traffic</span></div><TrafficTrendChart /></div>
      </div>
      <div className="dashboard-stack">
        <div className="card"><div className="section-title">地区分布</div>{regions.map(([r,c])=><div key={r} className="region-row"><span>{regionLabel(r)}</span><strong>{c}</strong></div>)}</div>
        <div className="card"><div className="section-title">最近异常</div>{recentBad.length ? recentBad.map(n=><div key={String(n.tag||n.name)} className="issue-row"><div><strong>{String(n.name||n.tag||'-')}</strong><span>{regionLabel(n.region)} · {latencyLabel(n.last_latency_ms)}</span></div><Badge tone={n.blacklisted?'bad':'warn'}>{n.blacklisted?'拉黑':'失败 '+(n.failure_count||0)}</Badge></div>) : <div className="hint">暂无异常节点</div>}</div>
      </div>
    </div>
  </div>
}
