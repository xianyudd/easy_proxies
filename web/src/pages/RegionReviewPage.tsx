import { Input, Pagination, Select } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, RefreshCw, ServerCog } from 'lucide-react'
import { confirmNodeRegion, getNodesPage } from '../api/nodes'
import { getReloadStatus, reloadCore } from '../api/configNodes'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { DataTable } from '../components/ui/DataTable'
import { QueryErrorBanner } from '../components/ui/QueryErrorBanner'
import { useToast } from '../components/ui/Toast'
import { MANUAL_REGION_OPTIONS } from '../components/charts/region'
import type { NodeSnapshot } from '../types/node'

const SOURCE_LABELS: Record<string, string> = {
  all: '全部来源',
  inline: '配置内置',
  nodes_file: '节点文件',
  subscription: '订阅源',
  free_proxy: '免费源',
  unknown: '未知来源',
}

function safeRows<T>(rows: unknown): T[] {
  return Array.isArray(rows) ? rows : []
}

function statusLabel(node: NodeSnapshot) {
  if (node.blacklisted) return '已拉黑'
  if (node.available && node.initial_check_done) return '可用'
  if (node.initial_check_done) return '不可用'
  return '未检测'
}

function statusTone(node: NodeSnapshot) {
  if (node.blacklisted || (node.initial_check_done && !node.available)) return 'bad'
  if (node.available && node.initial_check_done) return 'good'
  return 'warn'
}

function latencyLabel(value: unknown) {
  const ms = Number(value)
  return Number.isFinite(ms) && ms >= 0 ? `${ms} ms` : '未测速'
}

function tagOf(node: NodeSnapshot, idx: number) {
  return String(node.tag || node.name || node.uri || `node-${idx}`)
}

function reviewEvidence(node: NodeSnapshot) {
  const items = [
    node.exit_ip ? `出口 IP ${String(node.exit_ip)}` : '',
    node.cf_loc ? `CF ${String(node.cf_loc)}` : '',
    node.country ? `国家 ${String(node.country)}` : '',
    node.region && node.region !== 'other' ? `地区 ${String(node.region)}` : '',
    node.failure_count ? `失败 ${String(node.failure_count)} 次` : '',
    node.last_error ? `错误 ${String(node.last_error)}` : '',
  ].filter(Boolean)
  return items.length ? items.join(' · ') : '暂无识别线索'
}

export function RegionReviewPage() {
  const toast = useToast(s => s.show)
  const queryClient = useQueryClient()
  const [source, setSource] = useState('all')
  const [availability, setAvailability] = useState('available')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(100)
  const [selectedRegions, setSelectedRegions] = useState<Record<string, string>>({})
  const [needReload, setNeedReload] = useState(false)
  const [reloadState, setReloadState] = useState<'idle' | 'reloading' | 'failed'>('idle')

  const queryParams = { page, page_size: pageSize, region: 'other', source, availability, q: search, sort: 'latency' }
  const nodes = useQuery({
    queryKey: ['region-review-nodes', queryParams],
    queryFn: () => getNodesPage(queryParams),
    refetchInterval: 10000,
  })
  const reloadStatus = useQuery({
    queryKey: ['region-review-reload-status'],
    queryFn: getReloadStatus,
    enabled: reloadState === 'reloading',
    refetchInterval: reloadState === 'reloading' ? 800 : false,
  })

  const rows = safeRows<NodeSnapshot>(nodes.data?.nodes)
  const selectedConfirmations = rows
    .map((node, idx) => {
      const tag = tagOf(node, idx)
      return { tag: String(node.tag || ''), key: tag, region: selectedRegions[tag] || '' }
    })
    .filter(item => item.tag && item.region)
  const sourceOptions = useMemo(() => [
    { value: 'all', label: '全部来源' },
    { value: 'free_proxy', label: '免费源' },
    { value: 'subscription', label: '订阅源' },
    { value: 'inline', label: '配置内置' },
    { value: 'nodes_file', label: '节点文件' },
    { value: 'unknown', label: '未知来源' },
  ], [])

  useEffect(() => {
    if (nodes.data?.page && nodes.data.page !== page) setPage(nodes.data.page)
  }, [nodes.data?.page, page])

  useEffect(() => {
    const state = String(reloadStatus.data?.state || '')
    if (state === 'succeeded') {
      setReloadState('idle')
      setNeedReload(false)
      toast('地区确认已进入运行池', 'ok')
      void queryClient.invalidateQueries({ queryKey: ['nodes-page'] })
      void queryClient.invalidateQueries({ queryKey: ['nodes-summary'] })
      void nodes.refetch()
    } else if (state === 'failed') {
      setReloadState('failed')
      toast(reloadStatus.data?.error ? `重载失败：${reloadStatus.data.error}` : '重载失败', 'error')
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reloadStatus.data?.state, reloadStatus.data?.error])

  const confirmRegion = useMutation({
    mutationFn: ({ tag, region }: { tag: string; region: string }) => confirmNodeRegion(tag, region),
    onSuccess: res => {
      toast(res.message || '地区已确认', 'ok')
      if (res.need_reload) setNeedReload(true)
      void queryClient.invalidateQueries({ queryKey: ['nodes-page'] })
      void queryClient.invalidateQueries({ queryKey: ['nodes-summary'] })
      void nodes.refetch()
    },
    onError: error => toast(error instanceof Error ? error.message : '地区确认失败', 'error'),
  })

  const batchConfirmRegions = useMutation({
    mutationFn: async () => {
      let needReloadNext = false
      for (const item of selectedConfirmations) {
        const res = await confirmNodeRegion(item.tag, item.region)
        if (res.need_reload) needReloadNext = true
      }
      return { count: selectedConfirmations.length, need_reload: needReloadNext }
    },
    onSuccess: res => {
      toast(`已确认 ${res.count} 个节点地区`, 'ok')
      if (res.need_reload) setNeedReload(true)
      setSelectedRegions({})
      void queryClient.invalidateQueries({ queryKey: ['nodes-page'] })
      void queryClient.invalidateQueries({ queryKey: ['nodes-summary'] })
      void nodes.refetch()
    },
    onError: error => toast(error instanceof Error ? error.message : '批量确认地区失败', 'error'),
  })

  const startReload = useMutation({
    mutationFn: reloadCore,
    onSuccess: res => {
      toast(res.message || '重载已在后台启动', 'ok')
      setReloadState('reloading')
      void reloadStatus.refetch()
    },
    onError: error => {
      setReloadState('failed')
      toast(error instanceof Error ? error.message : '重载启动失败', 'error')
    },
  })

  const resetPage = <T,>(setter: (value: T) => void) => (value: T) => {
    setter(value)
    setPage(1)
  }

  const regionSelectFor = (tag: string, selected: string) => (
    <Select
      aria-label={`确认 ${tag} 的地区`}
      className="console-select"
      value={selected || undefined}
      placeholder="选择地区"
      options={MANUAL_REGION_OPTIONS}
      onChange={region => setSelectedRegions(prev => ({ ...prev, [tag]: region }))}
    />
  )

  return <div className="page region-review-page">
    <div className="page-header">
      <div>
        <h1>待确认节点</h1>
        <p>这里只展示自动识别不到、落到 other/空地区的节点。手动确认地区后会写入持久化覆盖；点击“重载入池”后进入对应地区池。</p>
      </div>
      <div className="toolbar">
        <Button variant="primary" onClick={() => batchConfirmRegions.mutate()} disabled={!selectedConfirmations.length || batchConfirmRegions.isPending || confirmRegion.isPending}>{batchConfirmRegions.isPending ? '确认中...' : `确认本页已选择${selectedConfirmations.length ? `（${selectedConfirmations.length}）` : ''}`}</Button>
        <Button onClick={() => { void nodes.refetch() }} disabled={nodes.isFetching}><RefreshCw size={16} />{nodes.isFetching ? '刷新中...' : '刷新'}</Button>
        <Button variant="primary" onClick={() => startReload.mutate()} disabled={!needReload || startReload.isPending || reloadState === 'reloading'}><ServerCog size={16} />{reloadState === 'reloading' ? '重载中...' : '重载入池'}</Button>
      </div>
    </div>

    {nodes.isError && <QueryErrorBanner title="待确认节点加载失败" error={nodes.error} onRetry={() => { void nodes.refetch() }} />}
    {reloadStatus.isError && <QueryErrorBanner title="重载状态加载失败" error={reloadStatus.error} onRetry={() => { void reloadStatus.refetch() }} />}

    {needReload && <div className="settings-alert modern-settings-alert settings-reload-alert" role="status">
      <CheckCircle2 size={18} />
      <div>
        <strong>地区确认已保存，等待重载入池</strong>
        <span>当前列表会立即移除已确认节点；点击“重载入池”后 GeoIP 地区池和导出结果会使用人工确认地区。</span>
      </div>
    </div>}

    <div className="summary-grid overview-summary">
      <div className="metric"><div className="label">待确认节点</div><div className="value">{nodes.isError ? '-' : Number(nodes.data?.total_filtered || 0)}</div></div>
      <div className="metric"><div className="label">当前页</div><div className="value">{nodes.isError ? '-' : rows.length}</div></div>
      <div className="metric"><div className="label">筛选来源</div><div className="value">{SOURCE_LABELS[source] || source}</div></div>
      <div className="metric"><div className="label">待重载</div><div className="value">{needReload ? '是' : '否'}</div></div>
    </div>

    <section className="card overview-filters">
      <div className="panel-header">
        <div>
          <div className="panel-title">筛选待确认节点</div>
          <div className="panel-subtitle">节点必须由人工确认到具体国家/地区；这里不提供 other 作为可选结果。确认后再统一重载入池。</div>
        </div>
      </div>
      <div className="form-grid-3 overview-filter-grid modern-filter-grid">
        <div className="field console-field"><label>搜索</label><Input className="console-input" aria-label="搜索待确认节点" value={search} onChange={event => { setSearch(event.target.value); setPage(1) }} placeholder="名称 / tag / URI / 来源" /></div>
        <div className="field console-field"><label>来源</label><Select aria-label="筛选待确认节点来源" className="console-select" value={source} onChange={resetPage(setSource)} options={sourceOptions} /></div>
        <div className="field console-field"><label>状态</label><Select aria-label="筛选待确认节点状态" className="console-select" value={availability} onChange={resetPage(setAvailability)} options={[{ value: 'available', label: '可用待确认' }, { value: 'all', label: '全部状态' }, { value: 'unavailable', label: '不可用 / 无法确认' }, { value: 'unchecked', label: '未检测' }, { value: 'blacklisted', label: '已拉黑' }]} /></div>
        <div className="field console-field"><label>每页</label><Select aria-label="待确认节点每页数量" className="console-select" value={pageSize} onChange={(value) => { setPageSize(value); setPage(1) }} options={[50, 100, 200, 500].map(value => ({ value, label: `${value} 条` }))} /></div>
      </div>
    </section>

    <section className="card">
      <div className="panel-header">
        <div>
          <div className="panel-title">人工确认队列</div>
          <div className="panel-subtitle">默认只展示可用待确认节点，避免无效免费源或 CDN 地址污染人工确认；需要排查时再切换到不可用 / 全部状态。</div>
        </div>
      </div>
      <div className="region-review-table-view">
        <DataTable headers={['节点', '来源', 'URI', '识别线索', '状态', '延迟', '确认地区', '操作']} empty={nodes.isLoading ? '加载中...' : nodes.isError ? '接口失败，请先重试。' : '暂无待确认节点'}>
          {rows.map((node, idx) => {
            const tag = tagOf(node, idx)
            const selected = selectedRegions[tag] || ''
            return <tr key={`${tag}-${idx}`}>
              <td><strong>{node.name || node.tag || '-'}</strong><br /><span className="muted mono">{node.tag || ''}</span></td>
              <td>{SOURCE_LABELS[String(node.source || 'unknown')] || String(node.source || '-')}</td>
              <td><span className="mono muted node-config-uri">{String(node.uri || '-')}</span></td>
              <td><span className="muted region-review-evidence">{reviewEvidence(node)}</span></td>
              <td><Badge tone={statusTone(node)}>{statusLabel(node)}</Badge></td>
              <td>{latencyLabel(node.last_latency_ms)}</td>
              <td>{regionSelectFor(tag, selected)}</td>
              <td><Button variant="primary" title={!node.tag ? '该节点缺少 tag，无法持久化确认' : !selected ? '请先选择具体国家/地区' : '确认该节点地区'} disabled={!node.tag || !selected || confirmRegion.isPending || batchConfirmRegions.isPending} onClick={() => confirmRegion.mutate({ tag: String(node.tag), region: selected })}>确认地区</Button></td>
            </tr>
          })}
        </DataTable>
      </div>
      <div className="region-review-mobile-list" aria-label="移动端待确认节点卡片列表">
        {rows.length ? rows.map((node, idx) => {
          const tag = tagOf(node, idx)
          const selected = selectedRegions[tag] || ''
          return <article className="node-card region-review-card" key={`${tag}-mobile-${idx}`}>
            <div className="node-card-head">
              <div>
                <strong>{node.name || node.tag || '-'}</strong>
                <span className="mono">{node.tag || ''}</span>
              </div>
              <Badge tone={statusTone(node)}>{statusLabel(node)}</Badge>
            </div>
            <div className="node-card-meta">
              <div><span>来源</span><strong>{SOURCE_LABELS[String(node.source || 'unknown')] || String(node.source || '-')}</strong></div>
              <div><span>延迟</span><strong>{latencyLabel(node.last_latency_ms)}</strong></div>
              <div><span>识别线索</span><strong>{reviewEvidence(node)}</strong></div>
            </div>
            <div className="node-config-card-uri">
              <span>URI</span>
              <code>{String(node.uri || '-')}</code>
            </div>
            <div className="region-review-card-controls">
              <div className="field console-field">
                <label>确认地区</label>
                {regionSelectFor(tag, selected)}
              </div>
              <Button variant="primary" title={!node.tag ? '该节点缺少 tag，无法持久化确认' : !selected ? '请先选择具体国家/地区' : '确认该节点地区'} disabled={!node.tag || !selected || confirmRegion.isPending || batchConfirmRegions.isPending} onClick={() => confirmRegion.mutate({ tag: String(node.tag), region: selected })}>确认地区</Button>
            </div>
          </article>
        }) : <div className="empty-state compact-empty"><strong>{nodes.isLoading ? '加载中...' : '暂无待确认节点'}</strong><span>{nodes.isError ? '接口失败，请先重试。' : '当前没有需要人工确认的可用节点。'}</span></div>}
      </div>
      <div className="toolbar list-pagination-toolbar" style={{ justifyContent: 'flex-end', marginTop: 16 }}>
        <Pagination
          current={page}
          pageSize={pageSize}
          total={nodes.data?.total_filtered || 0}
          showSizeChanger
          pageSizeOptions={[50, 100, 200, 500]}
          showTotal={(total, range) => `第 ${range[0]}-${range[1]} 条 / 共 ${total} 条`}
          onChange={(nextPage, nextPageSize) => {
            setPage(nextPage)
            if (nextPageSize !== pageSize) setPageSize(nextPageSize)
          }}
        />
      </div>
    </section>
  </div>
}
