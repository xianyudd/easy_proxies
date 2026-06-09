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

export function CfDistributionChart({ rows }: { rows: unknown }) {
  const option = useMemo<EChartsOption>(() => {
    const chartRows = safeRows<CloudflareResult>(rows)
    const labels: Record<string, string> = { excellent: '优秀', good: '良好', fair: '一般', poor: '较差', failed: '失败' }
    const colors: Record<string, string> = { excellent: '#10b981', good: '#3b82f6', fair: '#f59e0b', poor: '#f97316', failed: '#ef4444' }
    const data = Object.keys(labels).map(k => ({ name: labels[k], value: chartRows.filter(r => r.level === k).length, itemStyle: { color: colors[k] } })).filter(x => x.value > 0)
    return {
      backgroundColor: 'transparent', tooltip: { trigger: 'item', backgroundColor: panel(), borderColor: border(), textStyle: { color: text() } },
      legend: { bottom: 0, textStyle: { color: muted() } },
      series: [{ type: 'pie', radius: ['48%', '72%'], center: ['50%', '45%'], itemStyle: { borderRadius: 8, borderColor: panel(), borderWidth: 3 }, label: { color: text() }, data }]
    }
  }, [rows])
  return <EChart option={option} height={280} />
}

export function ReputationRiskChart({ rows }: { rows: unknown }) {
  const option = useMemo<EChartsOption>(() => {
    const chartRows = safeRows<ReputationResult>(rows)
    const labels: Record<string, string> = { low: '低风险', medium: '中风险', high: '高风险', failed: '失败' }
    const colors: Record<string, string> = { low: '#10b981', medium: '#f59e0b', high: '#ef4444', failed: '#64748b' }
    const keys = Object.keys(labels)
    return {
      backgroundColor: 'transparent', tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' }, backgroundColor: panel(), borderColor: border(), textStyle: { color: text() } },
      grid: { left: 10, right: 16, bottom: 18, top: 24, containLabel: true },
      xAxis: { type: 'category', data: keys.map(k => labels[k]), axisLabel: { color: muted() } },
      yAxis: { type: 'value', splitLine: { lineStyle: { color: border(), type: 'dashed' } }, axisLabel: { color: muted() } },
      series: [{ type: 'bar', name: 'IP 风险', data: keys.map(k => chartRows.filter(r => repLevel(r) === k).length), itemStyle: { color: (p: any) => colors[keys[p.dataIndex]], borderRadius: [6, 6, 0, 0] } }]
    }
  }, [rows])
  return <EChart option={option} height={280} />
}

export function CfScoreRankChart({ rows }: { rows: unknown }) {
  const option = useMemo<EChartsOption>(() => {
    const chartRows = safeRows<CloudflareResult>(rows)
    const sorted = [...chartRows].filter(r => typeof r.score === 'number').sort((a, b) => Number(b.score) - Number(a.score)).slice(0, 10).reverse()
    return {
      backgroundColor: 'transparent', tooltip: { trigger: 'axis', backgroundColor: panel(), borderColor: border(), textStyle: { color: text() } },
      grid: { left: 10, right: 18, bottom: 12, top: 24, containLabel: true },
      xAxis: { type: 'value', max: 100, splitLine: { lineStyle: { color: border(), type: 'dashed' } }, axisLabel: { color: muted() } },
      yAxis: { type: 'category', data: sorted.map(r => String(r.node_name || r.node_tag || '-')), axisLabel: { color: muted(), width: 130, overflow: 'truncate' } },
      series: [{ name: 'CF 分', type: 'bar', data: sorted.map(r => Number(r.score) || 0), itemStyle: { color: new echarts.graphic.LinearGradient(1, 0, 0, 0, [{ offset: 0, color: '#10b981' }, { offset: 1, color: '#2563eb' }]), borderRadius: [0, 6, 6, 0] } }]
    }
  }, [rows])
  return <EChart option={option} height={300} />
}
