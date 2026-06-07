import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getNodes, getNodesSummary } from '../api/nodes'
import { getDebug } from '../api/logs'
import { Badge } from '../components/ui/Badge'
import { RegionAvailabilityChart, LatencyTopChart, TrafficTrendChart, FailureRankChart } from '../components/charts/NodeCharts'

interface DebugNode { failure_count?: number; success_count?: number }
interface DebugResponse { success_rate?: number; nodes?: DebugNode[]; total_calls?: number; total_success?: number }

export function StatusPage() {
  const { data = [] } = useQuery({ queryKey:['nodes'], queryFn:getNodes, refetchInterval:10000 })
  const summary = useQuery({ queryKey:['nodes-summary'], queryFn:getNodesSummary, refetchInterval:10000 })
  const debug = useQuery({ queryKey:['debug-summary'], queryFn:() => getDebug() as Promise<DebugResponse>, refetchInterval:10000 })
  const summaryData = summary.data
  const healthyTotal = Object.values(summaryData?.region_healthy || {}).reduce((sum, count) => sum + Number(count || 0), 0)
  const totalNodes = Number(summaryData?.total_nodes || data.length || 0)
  const stats = useMemo(() => ({
    total: totalNodes,
    healthy: healthyTotal || data.filter(n=>n.available&&!n.blacklisted).length,
    bad: Math.max(0, totalNodes - (healthyTotal || data.filter(n=>n.available&&!n.blacklisted).length)),
    conn:data.reduce((s,n)=>s+(Number(n.active_connections)||0),0),
    successRate: Number(debug.data?.success_rate || 0),
  }), [data, debug.data, healthyTotal, totalNodes])
  const healthRate = stats.total ? Math.round((stats.healthy / stats.total) * 100) : 0
  const regions = Object.entries(summaryData?.region_stats && Object.keys(summaryData.region_stats).length ? summaryData.region_stats : data.reduce((m,n)=>{ const r=String(n.region||'other'); m[r]=(m[r]||0)+1; return m }, {} as Record<string,number>)).sort((a,b)=>b[1]-a[1])
  const recentBad = data.filter(n=>n.blacklisted || (Number(n.failure_count)||0)>0).slice(0,8)
  return <div className="page">
    <div className="page-header"><div><h1>运行状态</h1><p>把整体健康度、关键趋势和异常节点放在同一监控视图里，优先定位需要处理的问题。</p></div></div>
    <div className="status-hero">
      <div className="insight-panel">
        <div className="panel-title">整体健康状态</div>
        <div className="panel-subtitle">节点可用率 / Success telemetry</div>
        <div className="health-score">{healthRate}%</div>
        <div className="mini-grid"><div className="mini-stat"><span>总节点</span><strong>{stats.total}</strong></div><div className="mini-stat"><span>可用</span><strong>{stats.healthy}</strong></div><div className="mini-stat"><span>异常</span><strong>{stats.bad}</strong></div></div>
      </div>
      <div className="summary-grid"><div className="metric"><div className="label">成功率</div><div className="value">{stats.successRate.toFixed(1)}%</div></div><div className="metric"><div className="label">活跃连接</div><div className="value">{stats.conn}</div></div><div className="metric"><div className="label">地区数</div><div className="value">{regions.length}</div></div><div className="metric"><div className="label">最近异常</div><div className="value error">{recentBad.length}</div></div></div>
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
        <div className="card"><div className="section-title">地区分布</div>{regions.map(([r,c])=><div key={r} className="region-row"><span>{r}</span><strong>{c}</strong></div>)}</div>
        <div className="card"><div className="section-title">最近异常</div>{recentBad.length ? recentBad.map(n=><div key={String(n.tag||n.name)} className="issue-row"><div><strong>{String(n.name||n.tag||'-')}</strong><span>{String(n.region||'-')} · {Number(n.last_latency_ms)||0} ms</span></div><Badge tone={n.blacklisted?'bad':'warn'}>{n.blacklisted?'拉黑':'失败 '+(n.failure_count||0)}</Badge></div>) : <div className="hint">暂无异常节点</div>}</div>
      </div>
    </div>
  </div>
}
