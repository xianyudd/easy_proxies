import { useEffect, useRef } from 'react'
import * as echarts from 'echarts'
import type { EChartsOption } from 'echarts'

export function cssVar(name: string, fallback = '') {
  if (typeof window === 'undefined') return fallback
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback
}

export function formatBytes(bytes?: number, decimals = 1) {
  const value = Number(bytes || 0)
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  if (value < 1024) return `${parseFloat(value.toFixed(decimals))} B`
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.max(Math.floor(Math.log(value) / Math.log(1024)), 0), units.length - 1)
  return `${parseFloat((value / Math.pow(1024, index)).toFixed(decimals))} ${units[index]}`
}

export function EChart({ option, height = 320, className = '' }: { option: EChartsOption; height?: number | string; className?: string }) {
  const ref = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!ref.current) return
    const chart = echarts.init(ref.current, null, { renderer: 'canvas' })
    const resize = () => chart.resize()
    const observer = new ResizeObserver(resize)
    observer.observe(ref.current)
    window.addEventListener('resize', resize)
    return () => {
      observer.disconnect()
      window.removeEventListener('resize', resize)
      chart.dispose()
    }
  }, [])

  useEffect(() => {
    if (!ref.current) return
    const chart = echarts.getInstanceByDom(ref.current)
    chart?.setOption(option, true)
  }, [option])

  return <div ref={ref} className={`chart ${className}`} style={{ height }} />
}
