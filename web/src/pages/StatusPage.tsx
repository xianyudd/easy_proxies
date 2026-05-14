import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getNodes } from '../api/nodes'
import { getDebug } from '../api/logs'
import { DataTable } from '../components/ui/DataTable'
import { Badge } from '../components/ui/Badge'
import { RegionAvailabilityChart, LatencyTopChart, TrafficTrendChart, FailureRankChart } from '../components/charts/NodeCharts'

interface DebugNode { failure_count?: number; success_count?: number }
interface DebugResponse { success_rate?: number; nodes?: DebugNode[]; total_calls?: number; total_success?: number }

export function StatusPage() {
  const { data = [] } = useQuery({ queryKey:['nodes'], queryFn:getNodes, refetchInterval:10000 })
  const debug = useQuery({ queryKey:['debug-summary'], queryFn:() => getDebug() as Promise<DebugResponse>, refetchInterval:10000 })
  const stats = useMemo(() => ({
    total:data.length,
    healthy:data.filter(n=>n.available&&!n.blacklisted).length,
    bad:data.filter(n=>n.blacklisted || (n.initial_check_done && !n.available)).length,
    conn:data.reduce((s,n)=>s+(Number(n.active_connections)||0),0),
    successRate: Number(debug.data?.success_rate || 0),
  }), [data, debug.data])
  const regions = Object.entries(data.reduce((m,n)=>{ const r=String(n.region||'other'); m[r]=(m[r]||0)+1; return m }, {} as Record<string,number>)).sort((a,b)=>b[1]-a[1])
  const recentBad = data.filter(n=>n.blacklisted || (Number(n.failure_count)||0)>0).slice(0,20)
  return <div className="page">
    <div className="page-header"><div><h1>运行状态</h1><p>恢复旧版 Dashboard 的图形监控：地域连通率、最优延迟、实时流量和稳定性排行。</p></div></div>
    <div className="summary-grid">
      <div className="metric"><div className="label">总节点</div><div className="value">{stats.total}</div></div>
      <div className="metric"><div className="label">可用节点</div><div className="value success">{stats.healthy}</div></div>
      <div className="metric"><div className="label">异常节点</div><div className="value error">{stats.bad}</div></div>
      <div className="metric"><div className="label">成功率</div><div className="value">{stats.successRate.toFixed(1)}%</div></div>
    </div>
    <div className="charts-grid dashboard-charts">
      <div className="chart-panel wide"><div className="chart-title">地域连通率 <span>Region Availability</span></div><RegionAvailabilityChart nodes={data} /></div>
      <div className="chart-panel"><div className="chart-title">最优节点延迟 <span>Top Fastest Nodes</span></div><LatencyTopChart nodes={data} /></div>
      <div className="chart-panel"><div className="chart-title">实时流量带宽 <span>Real-time Traffic</span></div><TrafficTrendChart /></div>
      <div className="chart-panel"><div className="chart-title">稳定性掉线排行 <span>Top Unstable Nodes</span></div><FailureRankChart nodes={data} /></div>
    </div>
    <div className="grid-2">
      <div className="card"><div className="section-title">地区分布</div>{regions.map(([r,c])=><div key={r} className="region-row"><span>{r}</span><strong>{c}</strong></div>)}</div>
      <div className="card"><div className="section-title">最近异常</div><DataTable headers={['节点','地区','延迟','状态']} empty="暂无异常节点">{recentBad.map(n=><tr key={String(n.tag||n.name)}><td>{String(n.name||n.tag||'-')}</td><td>{String(n.region||'-')}</td><td>{Number(n.last_latency_ms)||0} ms</td><td><Badge tone={n.blacklisted?'bad':'warn'}>{n.blacklisted?'拉黑':'失败 '+(n.failure_count||0)}</Badge></td></tr>)}</DataTable></div>
    </div>
  </div>
}
