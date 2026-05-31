import { Pagination, Select } from 'antd'
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getNodesPage } from '../api/nodes'
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

const SOURCE_LABELS: Record<string, string> = {
  all: '全部',
  inline: '配置内置',
  nodes_file: '节点文件',
  subscription: '订阅源',
  free_proxy: '免费源',
  unknown: '未知',
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
  const [region, setRegion] = useState('all')
  const [source, setSource] = useState('all')
  const [availability, setAvailability] = useState('all')
  const [latency, setLatency] = useState('all')
  const [sortKey, setSortKey] = useState<'name'|'latency'|'latency_desc'|'region'|'source'>('latency')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(100)

  const queryParams = { page, page_size: pageSize, region, source, availability, latency, sort: sortKey }
  const { data, isLoading, refetch } = useQuery({
    queryKey: ['nodes-page', queryParams],
    queryFn: () => getNodesPage(queryParams),
    refetchInterval: 10000,
  })

  const rows = data?.nodes || []

  const regions = useMemo(() => {
    const stats = data?.region_stats || {}
    return Object.entries(stats).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  }, [data?.region_stats])

  const sources = useMemo(() => {
    const stats = data?.source_stats || {}
    return Object.entries(stats).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  }, [data?.source_stats])

  const summary = useMemo(() => ({
    total: data?.total_nodes || 0,
    filtered: data?.total_filtered || 0,
    available: Object.values(data?.region_healthy || {}).reduce((sum, n) => sum + n, 0),
    free: data?.source_stats?.free_proxy || 0,
  }), [data])

  const resetPage = <T,>(setter: (value: T) => void) => (value: T) => {
    setter(value)
    setPage(1)
  }

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
      <div className="metric"><div className="label">筛选结果</div><div className="value">{summary.filtered}</div></div>
      <div className="metric"><div className="label">免费源节点</div><div className="value">{summary.free}</div></div>
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
          <Select className="console-select" value={region} onChange={resetPage(setRegion)} options={[{ value: 'all', label: '全部' }, ...regions.map(([code, count]) => ({ value: code, label: `${regionLabel(code)} (${count})` }))]} />
        </div>
        <div className="field console-field">
          <label>来源</label>
          <Select className="console-select" value={source} onChange={resetPage(setSource)} options={[{ value: 'all', label: '全部' }, ...sources.map(([code, count]) => ({ value: code, label: `${SOURCE_LABELS[code] || code} (${count})` }))]} />
        </div>
        <div className="field console-field">
          <label>状态</label>
          <Select className="console-select" value={availability} onChange={resetPage(setAvailability)} options={[{ value: 'all', label: '全部' }, { value: 'available', label: '可用' }, { value: 'unavailable', label: '不可用' }, { value: 'blacklisted', label: '已拉黑' }, { value: 'unchecked', label: '未检测' }]} />
        </div>
        <div className="field console-field">
          <label>延迟</label>
          <Select className="console-select" value={latency} onChange={resetPage(setLatency)} options={[{ value: 'all', label: '全部' }, { value: 'fast', label: '800ms 以下' }, { value: 'slow', label: '800ms 以上' }, { value: 'tested', label: '已测速' }, { value: 'untested', label: '未测速' }]} />
        </div>
        <div className="field console-field">
          <label>排序</label>
          <Select className="console-select" value={sortKey} onChange={resetPage(setSortKey)} options={[{ value: 'latency', label: '延迟升序' }, { value: 'latency_desc', label: '延迟降序' }, { value: 'region', label: '地区' }, { value: 'source', label: '来源' }, { value: 'name', label: '名称字母序' }]} />
        </div>
        <div className="field console-field">
          <label>每页</label>
          <Select className="console-select" value={pageSize} onChange={(value) => { setPageSize(value); setPage(1) }} options={[50, 100, 200, 500].map(value => ({ value, label: `${value} 条` }))} />
        </div>
      </div>
    </div>

    <div className="card">
      <div className="panel-header">
        <div>
          <div className="panel-title">节点列表</div>
          <div className="panel-subtitle">第 {data?.page || page} 页，当前 {rows.length} 条，筛选后共 {data?.total_filtered || 0} 条。</div>
        </div>

      </div>
      <DataTable headers={['节点', '来源', '地区', '端口', '状态', '延迟', '连接', '失败', '操作']} empty={isLoading ? '加载中...' : '暂无节点'}>
        {rows.map((node, idx) => (
          <tr key={`${node.tag || node.name || 'node'}-${idx}`}>
            <td>
              <strong>{node.name || node.tag || '-'}</strong><br />
              <span className="muted mono">{node.tag || ''}</span>
            </td>
            <td>{SOURCE_LABELS[String(node.source || 'unknown')] || String(node.source || '-')}</td>
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
      <div className="toolbar" style={{ justifyContent: 'flex-end', marginTop: 16 }}>
        <Pagination
          current={page}
          pageSize={pageSize}
          total={data?.total_filtered || 0}
          showSizeChanger
          pageSizeOptions={[50, 100, 200, 500]}
          showTotal={(total, range) => `第 ${range[0]}-${range[1]} 条 / 共 ${total} 条`}
          onChange={(nextPage, nextPageSize) => {
            setPage(nextPage)
            if (nextPageSize !== pageSize) setPageSize(nextPageSize)
          }}
        />
      </div>
    </div>
  </div>
}
