import { useMemo } from 'react'
import * as echarts from 'echarts'
import type { EChartsOption } from 'echarts'
import { EChart, cssVar } from './EChart'
import type { CloudflareResult } from '../../types/cloudflare'
import type { ReputationResult } from '../../types/reputation'

function panel() { return cssVar('--panel', '#111827') }
function border() { return cssVar('--border', '#263143') }
function text() { return cssVar('--text', '#eef2ff') }
function muted() { return cssVar('--muted', '#94a3b8') }
function repLevel(row: ReputationResult) { const r = row.result || row; return r.risk_level || (row.error ? 'failed' : '-') }
function safeRows<T>(rows: unknown): T[] { return Array.isArray(rows) ? rows : [] }

/** Charts with no data render an explanation, not an empty axis. */
function ChartEmpty({ hint, height = 200 }: { hint: string; height?: number }) {
  return <div className="chart-empty" style={{ height }}>{hint}</div>
}

export function CfDistributionChart({ rows }: { rows: unknown }) {
  const chartRows = safeRows<CloudflareResult>(rows)
  const option = useMemo<EChartsOption>(() => {
    const labels: Record<string, string> = { excellent: '优秀', good: '良好', fair: '一般', poor: '较差', failed: '失败' }
    const colors: Record<string, string> = { excellent: '#10b981', good: '#3b82f6', fair: '#f59e0b', poor: '#f97316', failed: '#ef4444' }
    const data = Object.keys(labels).map(k => ({ name: labels[k], value: chartRows.filter(r => r.level === k).length, itemStyle: { color: colors[k] } })).filter(x => x.value > 0)
    return {
      backgroundColor: 'transparent', tooltip: { trigger: 'item', backgroundColor: panel(), borderColor: border(), textStyle: { color: text() } },
      legend: { bottom: 0, textStyle: { color: muted() } },
      series: [{ type: 'pie', radius: ['50%', '74%'], center: ['50%', '44%'], itemStyle: { borderRadius: 8, borderColor: panel(), borderWidth: 3 }, label: { color: text() }, data }]
    }
  }, [chartRows])
  if (!chartRows.length) return <ChartEmpty hint="尚无 CF 检测结果 · 先「刷新缓存」或启动 Pipeline 扫描" />
  return <EChart option={option} height={220} />
}

export function ReputationRiskChart({ rows }: { rows: unknown }) {
  const chartRows = safeRows<ReputationResult>(rows)
  const scored = chartRows.filter(r => repLevel(r) !== '-')
  const option = useMemo<EChartsOption>(() => {
    const labels: Record<string, string> = { low: '低风险', medium: '中风险', high: '高风险', failed: '失败' }
    const colors: Record<string, string> = { low: '#10b981', medium: '#f59e0b', high: '#ef4444', failed: '#64748b' }
    const keys = Object.keys(labels)
    return {
      backgroundColor: 'transparent', tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' }, backgroundColor: panel(), borderColor: border(), textStyle: { color: text() } },
      grid: { left: 10, right: 16, bottom: 18, top: 20, containLabel: true },
      xAxis: { type: 'category', data: keys.map(k => labels[k]), axisLabel: { color: muted() } },
      yAxis: { type: 'value', minInterval: 1, splitLine: { lineStyle: { color: border(), type: 'dashed' } }, axisLabel: { color: muted() } },
      series: [{ type: 'bar', name: 'IP 风险', data: keys.map(k => scored.filter(r => repLevel(r) === k).length), itemStyle: { color: (p: { dataIndex: number }) => colors[keys[p.dataIndex]], borderRadius: [6, 6, 0, 0] } }]
    }
  }, [scored])
  if (!scored.length) return <ChartEmpty hint="尚无 IP 风险数据 · 点「出口校准地区」或运行 Pipeline 后显示" />
  return <EChart option={option} height={220} />
}

export function CfScoreRankChart({ rows }: { rows: unknown }) {
  const chartRows = safeRows<CloudflareResult>(rows)
  const sorted = useMemo(
    () => [...chartRows].filter(r => typeof r.score === 'number').sort((a, b) => Number(b.score) - Number(a.score)).slice(0, 10).reverse(),
    [chartRows],
  )
  // All-equal scores make a bar chart of identical bars — latency is the only
  // remaining differentiator worth plotting.
  const scores = sorted.map(r => Number(r.score) || 0)
  const flat = scores.length > 1 && new Set(scores).size === 1
  const byLatency = useMemo(
    () => [...chartRows].filter(r => Number(r.latency_ms) > 0).sort((a, b) => Number(a.latency_ms) - Number(b.latency_ms)).slice(0, 10).reverse(),
    [chartRows],
  )
  const useLatency = flat && byLatency.length > 1
  const plotted = useLatency ? byLatency : sorted

  const option = useMemo<EChartsOption>(() => ({
    backgroundColor: 'transparent',
    tooltip: { trigger: 'axis', backgroundColor: panel(), borderColor: border(), textStyle: { color: text() } },
    grid: { left: 10, right: 22, bottom: 8, top: 16, containLabel: true },
    xAxis: useLatency
      ? { type: 'value', splitLine: { lineStyle: { color: border(), type: 'dashed' } }, axisLabel: { color: muted(), formatter: '{value} ms' } }
      : { type: 'value', max: 100, splitLine: { lineStyle: { color: border(), type: 'dashed' } }, axisLabel: { color: muted() } },
    yAxis: { type: 'category', data: plotted.map(r => String(r.node_name || r.node_tag || '-')), axisLabel: { color: muted(), width: 150, overflow: 'truncate' } },
    series: [{
      name: useLatency ? '延迟' : 'CF 分',
      type: 'bar',
      data: plotted.map(r => (useLatency ? Number(r.latency_ms) || 0 : Number(r.score) || 0)),
      itemStyle: {
        color: new echarts.graphic.LinearGradient(1, 0, 0, 0, [{ offset: 0, color: '#10b981' }, { offset: 1, color: '#2563eb' }]),
        borderRadius: [0, 6, 6, 0],
      },
    }],
  }), [plotted, useLatency])

  if (!plotted.length) return <ChartEmpty hint="尚无评分数据" height={160} />
  return <EChart option={option} height={Math.max(160, plotted.length * 26 + 40)} />
}

/** Whether the rank chart is ranking by latency instead of an all-100 score. */
export function rankChartIsLatency(rows: unknown) {
  const chartRows = safeRows<CloudflareResult>(rows)
  const scores = chartRows.filter(r => typeof r.score === 'number').sort((a, b) => Number(b.score) - Number(a.score)).slice(0, 10).map(r => Number(r.score) || 0)
  return scores.length > 1 && new Set(scores).size === 1 && chartRows.filter(r => Number(r.latency_ms) > 0).length > 1
}
