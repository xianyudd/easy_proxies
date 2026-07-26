import { Input, Modal, Pagination, Select, Table } from 'antd'
import type { TableColumnsType } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, RefreshCw, ServerCog } from 'lucide-react'
import { bulkConfirmRegions, confirmNodeRegion, getNodesPage } from '../api/nodes'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { QueryErrorBanner } from '../components/ui/QueryErrorBanner'
import { useToast } from '../components/ui/Toast'
import { useReload } from '../hooks/useReload'
import { MANUAL_REGION_OPTIONS } from '../components/charts/region'
import type { NodeSnapshot } from '../types/node'
import { Page } from '../components/layout/Page'

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

  const queryParams = { page, page_size: pageSize, region: 'other', source, availability, q: search, sort: 'latency' }
  const nodes = useQuery({
    queryKey: ['region-review-nodes', queryParams],
    queryFn: () => getNodesPage(queryParams),
    refetchInterval: 10000,
  })
  const { needReload, setNeedReload, isReloading, startReload, reloadStatusError, refetchReloadStatus } = useReload({
    scope: 'region-review',
    successMessage: '地区确认已进入运行池',
    onSucceeded: () => {
      void queryClient.invalidateQueries({ queryKey: ['nodes-page'] })
      void queryClient.invalidateQueries({ queryKey: ['nodes-summary'] })
      void nodes.refetch()
    },
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
    mutationFn: () => bulkConfirmRegions(selectedConfirmations),
    onSuccess: res => {
      if (res.fail === 0) {
        toast(`已确认 ${res.ok} 个节点地区`, 'ok')
      } else if (res.ok === 0) {
        toast(`批量确认失败 ${res.fail} 个${res.errors[0] ? `：${res.errors[0]}` : ''}`, 'error')
      } else {
        toast(`已确认 ${res.ok} 个，失败 ${res.fail} 个${res.errors[0] ? `：${res.errors[0]}` : ''}`, 'info')
      }
      if (res.need_reload) setNeedReload(true)
      setSelectedRegions({})
      void queryClient.invalidateQueries({ queryKey: ['nodes-page'] })
      void queryClient.invalidateQueries({ queryKey: ['nodes-summary'] })
      void nodes.refetch()
    },
    onError: error => toast(error instanceof Error ? error.message : '批量确认地区失败', 'error'),
  })

  const confirmBatch = () => {
    const count = selectedConfirmations.length
    if (!count) return
    Modal.confirm({
      title: `确认 ${count} 个节点的地区？`,
      content: '确认后这些节点会写入持久化地区并进入运行池，随后触发代理核心后台重载。',
      okText: '确认入池',
      okType: 'primary',
      cancelText: '取消',
      centered: true,
      onOk: () => batchConfirmRegions.mutateAsync(),
    })
  }

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

  const columns: TableColumnsType<NodeSnapshot> = [
    {
      title: '节点',
      key: 'node',
      width: 200,
      fixed: 'left',
      render: (_: unknown, node: NodeSnapshot) => (
        <><strong>{node.name || node.tag || '-'}</strong><br /><span className="muted mono">{node.tag || ''}</span></>
      ),
    },
    {
      title: '来源',
      key: 'source',
      width: 110,
      render: (_: unknown, node: NodeSnapshot) => SOURCE_LABELS[String(node.source || 'unknown')] || String(node.source || '-'),
    },
    {
      title: 'URI',
      key: 'uri',
      width: 260,
      render: (_: unknown, node: NodeSnapshot) => (
        <span className="mono muted uri-clip" title={String(node.uri || '')}>{String(node.uri || '-')}</span>
      ),
    },
    {
      title: '识别线索',
      key: 'evidence',
      width: 220,
      render: (_: unknown, node: NodeSnapshot) => (
        <span className="muted region-review-evidence">{reviewEvidence(node)}</span>
      ),
    },
    {
      title: '状态',
      key: 'status',
      width: 90,
      render: (_: unknown, node: NodeSnapshot) => <Badge tone={statusTone(node)}>{statusLabel(node)}</Badge>,
    },
    {
      title: '延迟',
      key: 'latency',
      width: 90,
      render: (_: unknown, node: NodeSnapshot) => latencyLabel(node.last_latency_ms),
    },
    {
      title: '确认地区',
      key: 'confirm',
      width: 220,
      fixed: 'right',
      render: (_: unknown, node: NodeSnapshot, idx: number) => {
        const tag = tagOf(node, idx)
        const selected = selectedRegions[tag] || ''
        return (
          <div className="confirm-inline">
            {regionSelectFor(tag, selected)}
            <Button variant="primary" title={!node.tag ? '该节点缺少 tag，无法持久化确认' : !selected ? '请先选择具体国家/地区' : '确认该节点地区'} disabled={!node.tag || !selected || confirmRegion.isPending || batchConfirmRegions.isPending} onClick={() => confirmRegion.mutate({ tag: String(node.tag), region: selected })}>确认</Button>
          </div>
        )
      },
    },
  ]

  return <Page
    className="region-review-page"
    title="待确认节点"
    description="自动识别不到地区的节点，人工确认后重载入池。"
    stats={[
      { label: '待确认', value: nodes.isError ? '-' : Number(nodes.data?.total_filtered || 0) },
      { label: '已选', value: selectedConfirmations.length },
    ]}
    actions={
      <>
        <Button onClick={() => { void nodes.refetch() }} disabled={nodes.isFetching}><RefreshCw size={16} />{nodes.isFetching ? '刷新中...' : '刷新'}</Button>
        <Button onClick={() => startReload.mutate()} disabled={!needReload || startReload.isPending || isReloading}><ServerCog size={16} />{isReloading ? '重载中...' : '重载入池'}</Button>
        <Button variant="primary" title={selectedConfirmations.length ? `批量确认 ${selectedConfirmations.length} 个节点的地区` : '先在列表中为节点选择地区'} onClick={confirmBatch} disabled={!selectedConfirmations.length || batchConfirmRegions.isPending || confirmRegion.isPending}>{batchConfirmRegions.isPending ? '确认中...' : `确认已选${selectedConfirmations.length ? `（${selectedConfirmations.length}）` : ''}`}</Button>
      </>
    }
  >

    {nodes.isError && <QueryErrorBanner title="待确认节点加载失败" error={nodes.error} onRetry={() => { void nodes.refetch() }} />}
    {reloadStatusError && <QueryErrorBanner title="重载状态加载失败" error={reloadStatusError} onRetry={refetchReloadStatus} />}

    {needReload && <div className="settings-alert modern-settings-alert settings-reload-alert" role="status">
      <CheckCircle2 size={18} />
      <div>
        <strong>地区确认已保存，等待重载入池</strong>
        <span>点击“重载入池”后，GeoIP 地区池和导出结果会使用人工确认的地区。</span>
      </div>
    </div>}

    <div className="ftoolbar">
      <Input className="console-input f-grow" allowClear aria-label="搜索待确认节点" value={search} onChange={event => { setSearch(event.target.value); setPage(1) }} placeholder="搜索名称 / tag / URI" />
      <Select aria-label="筛选待确认节点来源" className="console-select f-select" value={source} onChange={resetPage(setSource)} options={sourceOptions} />
      <Select aria-label="筛选待确认节点状态" className="console-select f-select" value={availability} onChange={resetPage(setAvailability)} options={[{ value: 'available', label: '可用待确认' }, { value: 'all', label: '全部状态' }, { value: 'unavailable', label: '不可用 / 无法确认' }, { value: 'unchecked', label: '未检测' }, { value: 'blacklisted', label: '已拉黑' }]} />
      <span className="f-end">{nodes.isError ? '' : `${rows.length} / ${Number(nodes.data?.total_filtered || 0)} 条`}</span>
    </div>

    <section className="sec">
      <div className="region-review-table-view">
        <Table<NodeSnapshot>
          className="quality-table region-review-table"
          size="middle"
          columns={columns}
          dataSource={rows}
          rowKey={record => String(record.tag || record.name || record.uri)}
          scroll={{ x: 1180 }}
          pagination={false}
          loading={nodes.isLoading && !rows.length}
          locale={{ emptyText: nodes.isError ? '接口失败，请先重试。' : '暂无待确认节点' }}
        />
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
          size="small"
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
  </Page>
}