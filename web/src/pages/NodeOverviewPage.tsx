import { Select } from 'antd'
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getNodes } from '../api/nodes'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { DataTable } from '../components/ui/DataTable'
import type { NodeSnapshot } from '../types/node'

const REGION_LABELS: Record<string, string> = {
  all: '全部',
  us: '美国',
  jp: '日本',
  hk: '香港',
  sg: '新加坡',
  de: '德国',
  gb: '英国',
}

function levelTone(node: NodeSnapshot) {
  if (node.blacklisted) return 'bad'
  if (node.available && node.initial_check_done) return 'good'
  if (node.available) return 'info'
  return 'warn'
}

function statusLabel(node: NodeSnapshot) {
  if (node.blacklisted) return '已拉黑'
  if (node.available && node.initial_check_done) return '可用'
  if (node.available) return '待确认'
  if (node.initial_check_done) return '不可用'
  return '未检测'
}

function regionLabel(region?: string) {
  const code = String(region || 'other').toLowerCase()
  return REGION_LABELS[code] || code.toUpperCase() || '-'
}

export function NodeOverviewPage() {
  const { data = [], isLoading, refetch } = useQuery({ queryKey: ['nodes'], queryFn: getNodes, refetchInterval: 10000 })
  const [region, setRegion] = useState('all')
  const [availability, setAvailability] = useState('all')
  const [latency, setLatency] = useState('all')
  const [sortKey, setSortKey] = useState<'name'|'latency'|'connections'|'failure'>('latency')

  const regions = useMemo(() => {
    const counts = data.reduce((map, node) => {
      const key = String(node.region || 'other').toLowerCase()
      map.set(key, (map.get(key) || 0) + 1)
      return map
    }, new Map<string, number>())
    return [...counts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  }, [data])

  const rows = useMemo(() => {
    return data
      .filter(node => region === 'all' || String(node.region || 'other').toLowerCase() === region)
      .filter(node => {
        if (availability === 'all') return true
        if (availability === 'available') return !!node.available && !node.blacklisted
        if (availability === 'unavailable') return !node.available || !!node.blacklisted
        if (availability === 'blacklisted') return !!node.blacklisted
        if (availability === 'unchecked') return !node.initial_check_done
        return true
      })
      .filter(node => {
        const latencyMs = Number(node.last_latency_ms) || 0
        if (latency === 'all') return true
        if (latency === '<500') return latencyMs > 0 && latencyMs < 500
        if (latency === '500-1000') return latencyMs >= 500 && latencyMs < 1000
        if (latency === '1000+') return latencyMs >= 1000
        return true
      })
      .sort((a, b) => {
        if (sortKey === 'name') return String(a.name || a.tag || '').localeCompare(String(b.name || b.tag || ''))
        if (sortKey === 'connections') return (Number(b.active_connections) || 0) - (Number(a.active_connections) || 0)
        if (sortKey === 'failure') return (Number(b.failure_count) || 0) - (Number(a.failure_count) || 0)
        return (Number(a.last_latency_ms) || 0) - (Number(b.last_latency_ms) || 0)
      })
  }, [availability, data, latency, region, sortKey])

  const summary = useMemo(() => ({
    total: data.length,
    available: data.filter(n => n.available && !n.blacklisted).length,
    blacklisted: data.filter(n => n.blacklisted).length,
    unchecked: data.filter(n => !n.initial_check_done).length,
  }), [data])

  const copyNode = (node: NodeSnapshot) => {
    const text = [
      node.name || node.tag || '-',
      `region=${node.region || '-'}`,
      `port=${node.port || '-'}`,
      `latency=${Number(node.last_latency_ms) || 0}ms`,
    ].join(' | ')
    navigator.clipboard.writeText(text)
  }

  return <div className="page overview-page">
    <div className="page-header">
      <div>
        <h1>节点总览</h1>
        <p>用一张表查看全部节点，支持地区、可用状态、延迟和排序筛选，方便快速定位可用代理。</p>
      </div>
      <div className="toolbar">
        <Button onClick={() => refetch()}>刷新</Button>
      </div>
    </div>

    <div className="summary-grid overview-summary">
      <div className="metric"><div className="label">总节点</div><div className="value">{summary.total}</div></div>
      <div className="metric"><div className="label">可用节点</div><div className="value success">{summary.available}</div></div>
      <div className="metric"><div className="label">拉黑节点</div><div className="value error">{summary.blacklisted}</div></div>
      <div className="metric"><div className="label">未检测</div><div className="value">{summary.unchecked}</div></div>
    </div>

    <div className="card overview-filters">
      <div className="panel-header">
        <div>
          <div className="panel-title">筛选条件</div>
          <div className="panel-subtitle">先过滤，再按需要排序查看节点清单。</div>
        </div>
      </div>
      <div className="form-grid-3 overview-filter-grid modern-filter-grid">
        <div className="field console-field">
          <label>地区</label>
          <Select className="console-select" value={region} onChange={setRegion} options={[{ value: 'all', label: '全部' }, ...regions.map(([code]) => ({ value: code, label: regionLabel(code) }))]} />
        </div>
        <div className="field console-field">
          <label>状态</label>
          <Select className="console-select" value={availability} onChange={setAvailability} options={[{ value: 'all', label: '全部' }, { value: 'available', label: '可用' }, { value: 'unavailable', label: '不可用' }, { value: 'blacklisted', label: '已拉黑' }, { value: 'unchecked', label: '未检测' }]} />
        </div>
        <div className="field console-field">
          <label>延迟</label>
          <Select className="console-select" value={latency} onChange={setLatency} options={[{ value: 'all', label: '全部' }, { value: '<500', label: '500ms 以下' }, { value: '500-1000', label: '500-1000ms' }, { value: '1000+', label: '1000ms 以上' }]} />
        </div>
        <div className="field console-field">
          <label>排序</label>
          <Select className="console-select" value={sortKey} onChange={value => setSortKey(value)} options={[{ value: 'latency', label: '延迟升序' }, { value: 'connections', label: '连接数降序' }, { value: 'failure', label: '失败次数降序' }, { value: 'name', label: '名称字母序' }]} />
        </div>
      </div>
    </div>

    <div className="card">
      <div className="panel-header">
        <div>
          <div className="panel-title">节点列表</div>
          <div className="panel-subtitle">共 {rows.length} 条结果。</div>
        </div>
      </div>
      <DataTable headers={['节点', '地区', '端口', '状态', '延迟', '连接', '失败', '操作']} empty={isLoading ? '加载中...' : '暂无节点'}>
        {rows.map((node, idx) => (
          <tr key={`${node.tag || node.name || 'node'}-${idx}`}>
            <td>
              <strong>{node.name || node.tag || '-'}</strong><br />
              <span className="muted mono">{node.tag || ''}</span>
            </td>
            <td>{regionLabel(node.region)}</td>
            <td>{node.port || '-'}</td>
            <td><Badge tone={levelTone(node)}>{statusLabel(node)}</Badge></td>
            <td>{Number(node.last_latency_ms) || 0} ms</td>
            <td>{Number(node.active_connections) || 0}</td>
            <td>{Number(node.failure_count) || 0}</td>
            <td>
              <div className="toolbar">
                <Button onClick={() => copyNode(node)}>复制</Button>
              </div>
            </td>
          </tr>
        ))}
      </DataTable>
    </div>
  </div>
}
