import { useEffect, useMemo, useState } from 'react'
import * as echarts from 'echarts'
import type { EChartsOption } from 'echarts'
import type { NodeSnapshot } from '../../types/node'
import { Button } from '../ui/Button'
import { EChart, cssVar, formatBytes } from './EChart'
import { regionMeta } from './region'

function chartText() { return cssVar('--text', '#eef2ff') }
function chartMuted() { return cssVar('--muted', '#94a3b8') }
function chartPanel() { return cssVar('--panel', '#111827') }
function chartBorder() { return cssVar('--border', '#263143') }
function safeRows<T>(rows: unknown): T[] { return Array.isArray(rows) ? rows : [] }

type TrafficWindow = '1m' | '5m' | '15m' | '1h'
const TRAFFIC_WINDOWS: Record<TrafficWindow, { label: string; ms: number }> = {
  '1m': { label: '1 分钟', ms: 60_000 },
  '5m': { label: '5 分钟', ms: 5 * 60_000 },
  '15m': { label: '15 分钟', ms: 15 * 60_000 },
  '1h': { label: '1 小时', ms: 60 * 60_000 },
}

function safeTrafficNumber(input: unknown) {
  const value = Number(input)
  return Number.isFinite(value) && value >= 0 ? value : 0
}

export function RegionAvailabilityChart({ nodes }: { nodes: unknown }) {
  const option = useMemo<EChartsOption>(() => {
    const chartNodes = safeRows<NodeSnapshot>(nodes)
    const stats = new Map<string, { total: number; healthy: number }>()
    for (const node of chartNodes) {
      const code = String(node.region || 'other').toLowerCase()
      const item = stats.get(code) || { total: 0, healthy: 0 }
      item.total += 1
      if (node.available && !node.blacklisted) item.healthy += 1
      stats.set(code, item)
    }
    const entries = [...stats.entries()].sort((a, b) => b[1].total - a[1].total || a[0].localeCompare(b[0]))
    const total = entries.reduce((sum, [, stat]) => sum + stat.total, 0)
    const healthy = entries.reduce((sum, [, stat]) => sum + stat.healthy, 0)
    const data = entries.map(([code, stat]) => {
      const meta = regionMeta(code)
      return { name: `${meta.emoji} ${meta.label}`, value: stat.total, code, healthy: stat.healthy, itemStyle: { color: meta.color } }
    })
    return {
      backgroundColor: 'transparent',
      tooltip: {
        trigger: 'item',
        backgroundColor: chartPanel(),
        borderColor: chartBorder(),
        textStyle: { color: chartText() },
        formatter: (p: any) => {
          const rate = p.data.value ? Math.round((p.data.healthy || 0) * 100 / p.data.value) : 0
          return `${p.name}<br/>节点总数：${p.data.value}<br/>健康在线：${p.data.healthy || 0}<br/>连通率：${rate}%`
        },
      },
      legend: { type: 'scroll', orient: 'vertical', right: 4, top: 36, bottom: 8, textStyle: { color: chartMuted(), fontSize: 12 } },
      series: [{
        type: 'pie', radius: ['45%', '72%'], center: ['36%', '54%'], minAngle: 5, avoidLabelOverlap: true,
        itemStyle: { borderRadius: 8, borderColor: chartPanel(), borderWidth: 3 },
        label: {
          show: true,
          position: 'center',
          formatter: `{a|${total}}\n{b|健康 ${healthy}}`,
          rich: {
            a: { color: chartText(), fontSize: 28, fontWeight: 800 },
            b: { color: chartMuted(), fontSize: 12, lineHeight: 22 },
          },
        },
        labelLine: { show: false },
        data,
      }],
    }
  }, [nodes])

  return <EChart option={option} height={340} />
}

export function LatencyTopChart({ nodes }: { nodes: unknown }) {
  const option = useMemo<EChartsOption>(() => {
    const chartNodes = safeRows<NodeSnapshot>(nodes)
    const sorted = chartNodes
      .filter(n => Number(n.last_latency_ms) > 0 && !n.blacklisted)
      .sort((a, b) => Number(a.last_latency_ms) - Number(b.last_latency_ms))
      .slice(0, 10)
      .reverse()
    return {
      backgroundColor: 'transparent',
      tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' }, backgroundColor: chartPanel(), borderColor: chartBorder(), textStyle: { color: chartText() } },
      grid: { left: 10, right: 18, bottom: 12, top: 24, containLabel: true },
      xAxis: { type: 'value', splitLine: { lineStyle: { color: chartBorder(), type: 'dashed' } }, axisLabel: { color: chartMuted(), formatter: '{value} ms' } },
      yAxis: { type: 'category', data: sorted.map(n => String(n.name || n.tag || '-')), axisLabel: { color: chartMuted(), width: 120, overflow: 'truncate' } },
      series: [{ name: '延迟', type: 'bar', data: sorted.map(n => Number(n.last_latency_ms) || 0), itemStyle: { color: new echarts.graphic.LinearGradient(1, 0, 0, 0, [{ offset: 0, color: '#10b981' }, { offset: 1, color: '#3b82f6' }]), borderRadius: [0, 6, 6, 0] } }],
    }
  }, [nodes])

  return <EChart option={option} height={340} />
}

export function FailureRankChart({ nodes }: { nodes: unknown }) {
  const option = useMemo<EChartsOption>(() => {
    const chartNodes = safeRows<NodeSnapshot>(nodes)
    const sorted = chartNodes.filter(n => Number(n.failure_count) > 0).sort((a, b) => Number(b.failure_count) - Number(a.failure_count)).slice(0, 10)
    return {
      backgroundColor: 'transparent',
      tooltip: { trigger: 'axis', backgroundColor: chartPanel(), borderColor: chartBorder(), textStyle: { color: chartText() } },
      grid: { left: 10, right: 18, bottom: 70, top: 24, containLabel: true },
      xAxis: { type: 'category', data: sorted.map(n => String(n.name || n.tag || '-')), axisLabel: { color: chartMuted(), width: 80, overflow: 'truncate', rotate: 28 } },
      yAxis: { type: 'value', splitLine: { lineStyle: { color: chartBorder(), type: 'dashed' } }, axisLabel: { color: chartMuted() } },
      series: [{ name: '失败次数', type: 'bar', data: sorted.map(n => Number(n.failure_count) || 0), itemStyle: { color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [{ offset: 0, color: '#f43f5e' }, { offset: 1, color: '#9f1239' }]), borderRadius: [6, 6, 0, 0] } }],
    }
  }, [nodes])

  return <EChart option={option} height={300} />
}

export function TrafficTrendChart() {
  const [points, setPoints] = useState<Array<{ time: string; ts: number; up: number; down: number }>>([])
  const [connected, setConnected] = useState(false)
  const [malformedEvents, setMalformedEvents] = useState(0)
  const [windowKey, setWindowKey] = useState<TrafficWindow>('5m')

  useEffect(() => {
    const source = new EventSource('/api/traffic')
    source.onopen = () => setConnected(true)
    source.onmessage = event => {
      try {
        const data = JSON.parse(event.data)
        const now = Date.now()
        const time = new Date(now).toLocaleTimeString([], { hour12: false })
        setPoints(prev => [...prev.slice(-599), { time, ts: now, up: safeTrafficNumber(data.up), down: safeTrafficNumber(data.down) }])
      } catch {
        setMalformedEvents(count => count + 1)
      }
    }
    source.onerror = () => setConnected(false)
    return () => source.close()
  }, [])

  const visiblePoints = useMemo(() => {
    const windowMs = TRAFFIC_WINDOWS[windowKey].ms
    const cutoff = Date.now() - windowMs
    return points.filter(point => point.ts >= cutoff)
  }, [points, windowKey])

  const option = useMemo<EChartsOption>(() => ({
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'axis',
      backgroundColor: chartPanel(),
      borderColor: chartBorder(),
      textStyle: { color: chartText() },
      formatter: (params: any) => `${params[0]?.axisValue || ''}<br/>${params.map((p: any) => `${p.marker}${p.seriesName}: ${formatBytes(p.value)}/s`).join('<br/>')}`,
    },
    legend: { top: 0, right: 10, textStyle: { color: chartMuted() } },
    grid: { left: 10, right: 18, bottom: 18, top: 36, containLabel: true },
    xAxis: { type: 'category', boundaryGap: false, data: visiblePoints.map(p => p.time), axisLine: { lineStyle: { color: chartBorder() } }, axisLabel: { color: chartMuted() } },
    yAxis: { type: 'value', splitLine: { lineStyle: { color: chartBorder(), type: 'dashed' } }, axisLabel: { color: chartMuted(), formatter: (v: number) => `${formatBytes(v)}/s` } },
    series: [
      { name: 'Up', type: 'line', smooth: true, symbol: 'none', lineStyle: { width: 2, color: '#3b82f6' }, areaStyle: { color: 'rgba(59,130,246,.18)' }, data: visiblePoints.map(p => p.up) },
      { name: 'Down', type: 'line', smooth: true, symbol: 'none', lineStyle: { width: 2, color: '#10b981' }, areaStyle: { color: 'rgba(16,185,129,.18)' }, data: visiblePoints.map(p => p.down) },
    ],
  }), [visiblePoints])

  return <div>
    <div className="toolbar" style={{ justifyContent: 'space-between', marginBottom: 10 }}>
      <div className={`chart-status ${connected ? 'ok' : 'warn'}`}>
        {connected ? '实时流量已连接' : '等待流量数据 / Clash API'}
        {malformedEvents > 0 ? ` · 流量数据异常 ${malformedEvents} 次` : ''}
      </div>
      <div className="toolbar">
        {(Object.keys(TRAFFIC_WINDOWS) as TrafficWindow[]).map(key => (
          <Button key={key} variant={windowKey === key ? 'primary' : 'secondary'} onClick={() => setWindowKey(key)}>{TRAFFIC_WINDOWS[key].label}</Button>
        ))}
      </div>
    </div>
    <EChart option={option} height={300} />
  </div>
}
