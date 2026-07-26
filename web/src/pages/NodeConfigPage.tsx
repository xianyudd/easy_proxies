import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Input, InputNumber, Modal, Pagination, Select, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { Clock3, Plus, RefreshCw, Save, ServerCog, Trash2, X } from 'lucide-react'
import { createConfigNode, deleteConfigNode, getConfigNodes, updateConfigNode } from '../api/configNodes'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { QueryErrorBanner } from '../components/ui/QueryErrorBanner'
import { useToast } from '../components/ui/Toast'
import { useReload } from '../hooks/useReload'
import type { ConfigNode } from '../types/configNode'
import { Page } from '../components/layout/Page'

const emptyDraft: ConfigNode = { name: '', uri: '', port: 0, username: '', password: '' }

function nodeKey(node: ConfigNode, idx: number) {
  return String(node.name || node.uri || `node-${idx}`)
}

function sourceLabel(source?: string) {
  switch (String(source || '').trim()) {
    case 'inline': return '配置内置'
    case 'nodes_file': return '节点文件'
    case 'subscription': return '订阅/节点文件'
    case 'free_proxy': return '免费源缓存'
    default: return source || '未知'
  }
}

function reloadText(state?: string) {
  switch (state) {
    case 'running': return '重载中'
    case 'succeeded': return '已生效'
    case 'failed': return '失败'
    default: return '空闲'
  }
}

function safeRows<T>(rows: unknown): T[] {
  return Array.isArray(rows) ? rows : []
}

export function NodeConfigPage() {
  const queryClient = useQueryClient()
  const toast = useToast(s => s.show)
  const [editingName, setEditingName] = useState<string | null>(null)
  const [editorOpen, setEditorOpen] = useState(false)
  const [draft, setDraft] = useState<ConfigNode>(emptyDraft)
  const [searchTerm, setSearchTerm] = useState('')
  const [sourceFilter, setSourceFilter] = useState('all')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const editorRef = useRef<HTMLElement | null>(null)

  const nodesQuery = useQuery({ queryKey: ['config-nodes'], queryFn: getConfigNodes })
  const { needReload, setNeedReload, reloadState, isReloading, startReload, status: reloadStatusData, reloadStatusError, refetchReloadStatus } = useReload({
    scope: 'config',
    successMessage: status => status?.duration_ms ? `节点配置已生效（${status.duration_ms}ms）` : '节点配置已生效',
    onSucceeded: () => {
      void nodesQuery.refetch()
      void queryClient.invalidateQueries({ queryKey: ['nodes-page'] })
      void queryClient.invalidateQueries({ queryKey: ['nodes-summary'] })
    },
  })

  const rows = useMemo(() => safeRows<ConfigNode>(nodesQuery.data?.nodes), [nodesQuery.data?.nodes])
  const nodesLoadingWithoutData = nodesQuery.isLoading && !nodesQuery.data
  const countText = (count: number) => nodesLoadingWithoutData ? '-' : count
  const sourceOptions = useMemo(() => {
    const sources = Array.from(new Set(rows.map(node => String(node.source || 'unknown')).filter(Boolean))).sort()
    return [{ value: 'all', label: '全部来源' }, ...sources.map(source => ({ value: source, label: sourceLabel(source) }))]
  }, [rows])
  const filteredRows = useMemo(() => {
    const term = searchTerm.trim().toLowerCase()
    return rows.filter(node => {
      const source = String(node.source || 'unknown')
      if (sourceFilter !== 'all' && source !== sourceFilter) return false
      if (!term) return true
      const haystack = `${node.name || ''} ${node.uri || ''} ${node.source || ''} ${node.port || ''}`.toLowerCase()
      return haystack.includes(term)
    })
  }, [rows, searchTerm, sourceFilter])
  const editableRows = rows.filter(node => node.source !== 'free_proxy')
  const pagedRows = useMemo(() => {
    const totalPages = Math.max(1, Math.ceil(filteredRows.length / pageSize))
    const safePage = Math.min(page, totalPages)
    const start = (safePage - 1) * pageSize
    return filteredRows.slice(start, start + pageSize)
  }, [filteredRows, page, pageSize])

  useEffect(() => {
    const totalPages = Math.max(1, Math.ceil(filteredRows.length / pageSize))
    if (page > totalPages) setPage(totalPages)
  }, [filteredRows.length, page, pageSize])

  const resetDraft = () => {
    setEditingName(null)
    setEditorOpen(false)
    setDraft(emptyDraft)
  }
  const editNode = (node: ConfigNode) => {
    setEditingName(String(node.name || ''))
    setEditorOpen(true)
    setDraft({ ...emptyDraft, ...node })
    window.setTimeout(() => editorRef.current?.scrollIntoView({ block: 'start', behavior: 'smooth' }), 0)
  }
  const updateDraft = (patch: Partial<ConfigNode>) => setDraft(prev => ({ ...prev, ...patch }))
  const validateDraft = () => {
    if (!String(draft.name || '').trim()) return '请填写节点名称'
    if (!String(draft.uri || '').trim()) return '请填写节点 URI'
    return ''
  }

  const saveNode = useMutation({
    mutationFn: async () => {
      const error = validateDraft()
      if (error) throw new Error(error)
      const payload = { ...draft, name: String(draft.name || '').trim(), uri: String(draft.uri || '').trim(), port: Number(draft.port || 0) }
      return editingName ? updateConfigNode(editingName, payload) : createConfigNode(payload)
    },
    onSuccess: res => {
      toast(res.message || '节点已保存', 'ok')
      if (res.need_reload) setNeedReload(true)
      resetDraft()
      void nodesQuery.refetch()
    },
    onError: e => toast(e instanceof Error ? e.message : '节点保存失败', 'error'),
  })

  const removeNode = useMutation({
    mutationFn: deleteConfigNode,
    onSuccess: res => {
      toast(res.message || '节点已删除', 'ok')
      if (res.need_reload) setNeedReload(true)
      if (editingName) resetDraft()
      void nodesQuery.refetch()
    },
    onError: e => toast(e instanceof Error ? e.message : '节点删除失败', 'error'),
  })

  const confirmDelete = (name: string) => {
    Modal.confirm({
      title: `删除节点「${name}」？`,
      content: '该节点会从配置文件移除；确认后需手动重载才会让端口映射生效。此操作不可撤销。',
      okText: '删除',
      okType: 'danger',
      cancelText: '取消',
      centered: true,
      onOk: () => removeNode.mutateAsync(name),
    })
  }

  const columns: ColumnsType<ConfigNode> = [
    {
      title: '名称',
      width: 240,
      fixed: 'left',
      render: (_, node) => <strong>{String(node.name || '') || '-'}</strong>,
    },
    {
      title: '来源',
      width: 130,
      render: (_, node) => <Badge tone={node.source === 'free_proxy' ? 'neutral' : 'info'}>{sourceLabel(node.source)}</Badge>,
    },
    {
      title: 'URI',
      width: 300,
      render: (_, node) => <span className="mono muted uri-clip" title={String(node.uri || '')}>{String(node.uri || '-')}</span>,
    },
    {
      title: '端口',
      width: 90,
      render: (_, node) => Number(node.port || 0) || '自动',
    },
    {
      title: '认证',
      width: 90,
      render: (_, node) => (node.username || node.password ? '已配置' : '无'),
    },
    {
      title: '操作',
      width: 170,
      fixed: 'right',
      render: (_, node) => {
        const name = String(node.name || '')
        const canEdit = node.source !== 'free_proxy' && !!name
        return <div className="toolbar">
          <Button aria-label={`编辑节点 ${name}`} disabled={!canEdit} onClick={() => editNode(node)}>编辑</Button>
          <Button
            aria-label={`删除节点 ${name}`}
            variant="danger"
            disabled={!canEdit || removeNode.isPending}
            onClick={() => confirmDelete(name)}
          ><Trash2 size={15} />删除</Button>
        </div>
      },
    },
  ]

  return <Page
    className="node-config-page"
    title="节点配置"
    description="维护写入配置文件的节点，修改后手动重载生效。"
    stats={[
      { label: '配置', value: nodesQuery.isError ? '-' : countText(rows.length) },
      { label: '可编辑', value: nodesQuery.isError ? '-' : countText(editableRows.length) },
      { label: '重载', value: reloadText(reloadState === 'reloading' ? reloadStatusData?.state : reloadState) },
    ]}
    actions={
      <>
        <Button onClick={() => { void nodesQuery.refetch() }} disabled={nodesQuery.isFetching}><RefreshCw size={16} className={nodesQuery.isFetching ? 'spin' : undefined} />{nodesQuery.isFetching ? '刷新中...' : '刷新'}</Button>
        <Button onClick={() => startReload.mutate()} disabled={!needReload || startReload.isPending || isReloading}><ServerCog size={16} />{isReloading ? '重载中...' : '手动重载'}</Button>
        <Button variant="primary" onClick={() => { if (editorOpen && !editingName) { resetDraft() } else { setEditingName(null); setDraft(emptyDraft); setEditorOpen(true) } }}><Plus size={16} />{editorOpen && !editingName ? '收起表单' : '新增节点'}</Button>
      </>
    }
  >

    {nodesQuery.isError && <QueryErrorBanner title="节点配置加载失败" error={nodesQuery.error} onRetry={() => { void nodesQuery.refetch() }} />}
    {reloadStatusError && <QueryErrorBanner title="重载状态加载失败" error={reloadStatusError} onRetry={refetchReloadStatus} />}

    {needReload && <div className="settings-alert modern-settings-alert settings-reload-alert" role="status">
      <Clock3 size={18} />
      <div>
        <strong>节点配置已保存，等待手动重载</strong>
        <span>当前变更已经写入配置文件；点击“手动重载”后才会进入运行节点和节点总览。</span>
      </div>
    </div>}
    {reloadState !== 'idle' && <div className="settings-alert modern-settings-alert settings-reload-alert" role="status">
      <Clock3 size={18} />
      <div>
        <strong>{reloadState === 'reloading' ? '代理核心正在后台重载' : '代理核心重载失败'}</strong>
        <span>{reloadState === 'reloading' ? `已运行 ${Math.floor(Number(reloadStatusData?.elapsed_ms || 0) / 1000)} 秒，完成后会自动清除待重载状态。` : reloadStatusData?.error || '请检查日志后重试。'}</span>
      </div>
    </div>}

    {editorOpen ? <section ref={editorRef} className="sec sec-flush node-config-editor">
      <div className="sec-head">
        <h2>{editingName ? `编辑节点：${editingName}` : '新增节点'}</h2>
        <span className="sec-desc">URI 支持 http/socks5 等格式；端口为 0 时自动分配。</span>
        <div className="sec-actions">
          <Button variant="ghost" onClick={resetDraft}><X size={15} />取消</Button>
          <Button variant="primary" onClick={() => saveNode.mutate()} disabled={saveNode.isPending}><Save size={16} />{saveNode.isPending ? '保存中...' : editingName ? '保存修改' : '新增节点'}</Button>
        </div>
      </div>
      <form className="form-grid-3 node-config-editor-grid" onSubmit={e => { e.preventDefault(); saveNode.mutate() }}>
        <div className="field settings-form-item"><label>名称</label><Input aria-label="节点名称" className="settings-input" value={String(draft.name || '')} onChange={e => updateDraft({ name: e.target.value })} placeholder="manual-us-1" /></div>
        <div className="field settings-form-item"><label>URI</label><Input aria-label="节点 URI" className="settings-input mono" value={String(draft.uri || '')} onChange={e => updateDraft({ uri: e.target.value })} placeholder="socks5://127.0.0.1:1080" /></div>
        <div className="field settings-form-item"><label>固定端口</label><InputNumber aria-label="固定端口" className="settings-input" min={0} max={65535} value={Number(draft.port || 0)} onChange={value => updateDraft({ port: Number(value || 0) })} /></div>
        <div className="field settings-form-item"><label>用户名</label><Input aria-label="节点用户名" className="settings-input" value={String(draft.username || '')} onChange={e => updateDraft({ username: e.target.value })} autoComplete="username" /></div>
        <div className="field settings-form-item"><label>密码</label><Input.Password aria-label="节点密码" className="settings-input" value={String(draft.password || '')} onChange={e => updateDraft({ password: e.target.value })} autoComplete="current-password" /></div>
      </form>
    </section> : null}

    <div className="ftoolbar">
      <Input allowClear aria-label="搜索节点配置" className="console-input f-grow" value={searchTerm} onChange={event => { setSearchTerm(event.target.value); setPage(1) }} placeholder="搜索名称 / URI / 来源 / 端口" />
      <Select aria-label="筛选节点来源" className="console-select f-select" value={sourceFilter} options={sourceOptions} onChange={value => { setSourceFilter(value); setPage(1) }} />
      <span className="f-end">{nodesLoadingWithoutData ? '加载中...' : `${filteredRows.length} / ${rows.length} 条`}</span>
    </div>

    <section className="sec">
      <div className="node-config-table-view">
        <Table
          className="quality-table node-config-table"
          size="middle"
          columns={columns}
          rowKey={record => String(record.name || record.uri)}
          scroll={{ x: 1020 }}
          dataSource={pagedRows}
          pagination={false}
          loading={nodesQuery.isLoading && !rows.length}
          locale={{ emptyText: nodesQuery.isError ? '接口失败，请先重试。' : '暂无配置节点' }}
        />
      </div>
      <div className="node-config-mobile-list" aria-label="移动端节点配置卡片列表">
        {pagedRows.length ? pagedRows.map((node, idx) => {
          const name = String(node.name || '')
          const canEdit = node.source !== 'free_proxy' && !!name
          return <article className="node-card node-config-card" key={`${nodeKey(node, idx)}-config-card`}>
            <div className="node-card-head">
              <div>
                <strong>{name || '-'}</strong>
                <span className="mono">{sourceLabel(node.source)}</span>
              </div>
              <Badge tone={node.source === 'free_proxy' ? 'neutral' : 'info'}>{Number(node.port || 0) || '自动端口'}</Badge>
            </div>
            <div className="node-card-meta">
              <div><span>来源</span><strong>{sourceLabel(node.source)}</strong></div>
              <div><span>端口</span><strong>{Number(node.port || 0) || '自动'}</strong></div>
              <div><span>认证</span><strong>{node.username || node.password ? '已配置' : '无'}</strong></div>
              <div><span>编辑状态</span><strong>{canEdit ? '可编辑' : '只读'}</strong></div>
            </div>
            <div className="node-config-card-uri">
              <span>URI</span>
              <code>{String(node.uri || '-')}</code>
            </div>
            <div className="node-card-foot">
              <span>{canEdit ? '修改后需手动重载生效' : '免费源缓存节点请在免费源设置中管理'}</span>
              <div className="quality-card-actions">
                <Button aria-label={`编辑节点 ${name}`} disabled={!canEdit} onClick={() => editNode(node)}>编辑</Button>
                <Button
                  aria-label={`删除节点 ${name}`}
                  variant="danger"
                  disabled={!canEdit || removeNode.isPending}
                  onClick={() => confirmDelete(name)}
                ><Trash2 size={15} />删除</Button>
              </div>
            </div>
          </article>
        }) : <div className="empty-state compact-empty"><strong>{nodesQuery.isLoading ? '加载中...' : '暂无配置节点'}</strong><span>{nodesQuery.isError ? '接口失败，请先重试。' : '调整搜索或来源筛选后再查看。'}</span></div>}
      </div>
      <div className="toolbar list-pagination-toolbar" style={{ justifyContent: 'flex-end', marginTop: 16 }}>
        <Pagination
          size="small"
          current={page}
          pageSize={pageSize}
          total={filteredRows.length}
          showSizeChanger
          pageSizeOptions={[25, 50, 100, 200]}
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