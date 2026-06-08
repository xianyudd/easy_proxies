import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Input, InputNumber, Pagination, Select } from 'antd'
import { Clock3, Plus, RefreshCw, Save, ServerCog, Trash2, X } from 'lucide-react'
import { createConfigNode, deleteConfigNode, getConfigNodes, getReloadStatus, reloadCore, updateConfigNode } from '../api/configNodes'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { DataTable } from '../components/ui/DataTable'
import { QueryErrorBanner } from '../components/ui/QueryErrorBanner'
import { useToast } from '../components/ui/Toast'
import type { ConfigNode } from '../types/configNode'

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

export function NodeConfigPage() {
  const queryClient = useQueryClient()
  const toast = useToast(s => s.show)
  const [editingName, setEditingName] = useState<string | null>(null)
  const [draft, setDraft] = useState<ConfigNode>(emptyDraft)
  const [needReload, setNeedReload] = useState(false)
  const [confirmDeleteName, setConfirmDeleteName] = useState<string | null>(null)
  const [searchTerm, setSearchTerm] = useState('')
  const [sourceFilter, setSourceFilter] = useState('all')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const [reloadState, setReloadState] = useState<'idle' | 'reloading' | 'failed'>('idle')

  const nodesQuery = useQuery({ queryKey: ['config-nodes'], queryFn: getConfigNodes })
  const reloadStatus = useQuery({
    queryKey: ['config-reload-status'],
    queryFn: getReloadStatus,
    enabled: reloadState === 'reloading',
    refetchInterval: reloadState === 'reloading' ? 800 : false,
  })

  const rows = useMemo(() => nodesQuery.data?.nodes || [], [nodesQuery.data?.nodes])
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

  useEffect(() => {
    const state = String(reloadStatus.data?.state || '')
    if (state === 'succeeded') {
      setReloadState('idle')
      setNeedReload(false)
      toast(reloadStatus.data?.duration_ms ? `节点配置已生效（${reloadStatus.data.duration_ms}ms）` : '节点配置已生效', 'ok')
      void nodesQuery.refetch()
      void queryClient.invalidateQueries({ queryKey: ['nodes-page'] })
    } else if (state === 'failed') {
      setReloadState('failed')
      toast(reloadStatus.data?.error ? `重载失败：${reloadStatus.data.error}` : '重载失败', 'error')
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reloadStatus.data?.state, reloadStatus.data?.duration_ms, reloadStatus.data?.error])

  const resetDraft = () => {
    setEditingName(null)
    setDraft(emptyDraft)
    setConfirmDeleteName(null)
  }
  const editNode = (node: ConfigNode) => {
    setEditingName(String(node.name || ''))
    setDraft({ ...emptyDraft, ...node })
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
      setConfirmDeleteName(null)
      void nodesQuery.refetch()
    },
    onError: e => toast(e instanceof Error ? e.message : '节点删除失败', 'error'),
  })

  const startReload = useMutation({
    mutationFn: reloadCore,
    onSuccess: res => {
      toast(res.message || '重载已在后台启动', 'ok')
      setReloadState('reloading')
      if (res.reload_status?.state === 'succeeded') setNeedReload(false)
      void reloadStatus.refetch()
    },
    onError: e => {
      setReloadState('failed')
      toast(e instanceof Error ? e.message : '重载启动失败', 'error')
    },
  })

  return <div className="page node-config-page settings-page">
    <div className="page-header settings-hero">
      <div>
        <h1>节点配置</h1>
        <p>维护写入配置文件的节点。新增、编辑、删除后不会立刻打断代理核心；确认后手动重载，让端口映射稳定生效。</p>
      </div>
      <div className="toolbar">
        <Button onClick={() => { void nodesQuery.refetch() }} disabled={nodesQuery.isFetching}><RefreshCw size={16} />{nodesQuery.isFetching ? '刷新中...' : '刷新'}</Button>
        <Button variant="primary" onClick={() => startReload.mutate()} disabled={!needReload || startReload.isPending || reloadState === 'reloading'}><ServerCog size={16} />{reloadState === 'reloading' ? '重载中...' : '手动重载'}</Button>
      </div>
    </div>

    {nodesQuery.isError && <QueryErrorBanner title="节点配置加载失败" error={nodesQuery.error} onRetry={() => { void nodesQuery.refetch() }} />}
    {reloadStatus.isError && <QueryErrorBanner title="重载状态加载失败" error={reloadStatus.error} onRetry={() => { void reloadStatus.refetch() }} />}

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
        <span>{reloadState === 'reloading' ? `已运行 ${Math.floor(Number(reloadStatus.data?.elapsed_ms || 0) / 1000)} 秒，完成后会自动清除待重载状态。` : reloadStatus.data?.error || '请检查日志后重试。'}</span>
      </div>
    </div>}

    <div className="summary-grid overview-summary">
      <div className="metric"><div className="label">配置节点</div><div className="value">{nodesQuery.isError ? '-' : rows.length}</div></div>
      <div className="metric"><div className="label">可编辑节点</div><div className="value success">{nodesQuery.isError ? '-' : editableRows.length}</div></div>
      <div className="metric"><div className="label">筛选结果</div><div className="value">{nodesQuery.isError ? '-' : filteredRows.length}</div></div>
      <div className="metric"><div className="label">待重载</div><div className="value">{needReload ? '是' : '否'}</div></div>
      <div className="metric"><div className="label">重载状态</div><div className="value">{reloadText(reloadState === 'reloading' ? reloadStatus.data?.state : reloadState)}</div></div>
    </div>

    <section className="card settings-section settings-section-featured">
      <div className="panel-header settings-section-header">
        <div><div className="panel-title">{editingName ? `编辑节点：${editingName}` : '新增节点'}</div><div className="panel-subtitle">URI 支持 http/socks5 等代理格式；端口为 0 时由系统按当前模式自动分配。</div></div>
        <div className="toolbar"><Button variant="ghost" onClick={resetDraft}><X size={15} />清空</Button><Button variant="primary" onClick={() => saveNode.mutate()} disabled={saveNode.isPending}><Save size={16} />{saveNode.isPending ? '保存中...' : editingName ? '保存修改' : '新增节点'}</Button></div>
      </div>
      <form className="form-grid-3 compact-form-grid" onSubmit={e => { e.preventDefault(); saveNode.mutate() }}>
        <div className="field settings-form-item"><label>名称</label><Input aria-label="节点名称" className="settings-input" value={String(draft.name || '')} onChange={e => updateDraft({ name: e.target.value })} placeholder="manual-us-1" /></div>
        <div className="field settings-form-item"><label>URI</label><Input aria-label="节点 URI" className="settings-input mono" value={String(draft.uri || '')} onChange={e => updateDraft({ uri: e.target.value })} placeholder="socks5://127.0.0.1:1080" /></div>
        <div className="field settings-form-item"><label>固定端口</label><InputNumber aria-label="固定端口" className="settings-input" min={0} max={65535} value={Number(draft.port || 0)} onChange={value => updateDraft({ port: Number(value || 0) })} /></div>
        <div className="field settings-form-item"><label>用户名</label><Input aria-label="节点用户名" className="settings-input" value={String(draft.username || '')} onChange={e => updateDraft({ username: e.target.value })} autoComplete="username" /></div>
        <div className="field settings-form-item"><label>密码</label><Input.Password aria-label="节点密码" className="settings-input" value={String(draft.password || '')} onChange={e => updateDraft({ password: e.target.value })} autoComplete="current-password" /></div>
      </form>
    </section>

    <section className="card">
      <div className="panel-header">
        <div><div className="panel-title">配置节点列表</div><div className="panel-subtitle">共 {rows.length} 条，当前筛选 {filteredRows.length} 条。订阅模式下手动节点会写入节点文件；免费源缓存节点不会在这里编辑。</div></div>
      </div>
      <div className="form-grid-3 compact-form-grid node-config-filters">
        <div className="field settings-form-item"><label>搜索</label><Input aria-label="搜索节点配置" className="settings-input" value={searchTerm} onChange={event => { setSearchTerm(event.target.value); setPage(1) }} placeholder="名称 / URI / 来源 / 端口" /></div>
        <div className="field settings-form-item"><label>来源</label><Select aria-label="筛选节点来源" className="settings-input" value={sourceFilter} options={sourceOptions} onChange={value => { setSourceFilter(value); setPage(1) }} /></div>
        <div className="field settings-form-item"><label>每页</label><Select className="settings-input" value={pageSize} options={[25, 50, 100, 200].map(value => ({ value, label: `${value} 条` }))} onChange={value => { setPageSize(value); setPage(1) }} /></div>
      </div>
      <DataTable headers={['名称', '来源', 'URI', '端口', '认证', '操作']} empty={nodesQuery.isLoading ? '加载中...' : nodesQuery.isError ? '接口失败，请先重试。' : '暂无配置节点'}>
        {pagedRows.map((node, idx) => {
          const name = String(node.name || '')
          const canEdit = node.source !== 'free_proxy' && !!name
          return <tr key={nodeKey(node, idx)}>
            <td><strong>{name || '-'}</strong></td>
            <td><Badge tone={node.source === 'free_proxy' ? 'neutral' : 'info'}>{sourceLabel(node.source)}</Badge></td>
            <td><span className="mono muted node-config-uri">{String(node.uri || '-')}</span></td>
            <td>{Number(node.port || 0) || '自动'}</td>
            <td>{node.username || node.password ? '已配置' : '无'}</td>
            <td><div className="toolbar">
              <Button disabled={!canEdit} onClick={() => editNode(node)}>编辑</Button>
              <Button
                variant="danger"
                disabled={!canEdit || removeNode.isPending}
                onClick={() => {
                  if (confirmDeleteName === name) removeNode.mutate(name)
                  else setConfirmDeleteName(name)
                }}
              ><Trash2 size={15} />{confirmDeleteName === name ? '确认删除' : '删除'}</Button>
            </div></td>
          </tr>
        })}
      </DataTable>
      <div className="toolbar" style={{ justifyContent: 'flex-end', marginTop: 16 }}>
        <Pagination
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
  </div>
}
