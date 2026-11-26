import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Input, Modal, Switch, Table } from 'antd'
import { api } from '../../api/client'
import { applyLocalSources, deleteLocalSource, getLocalSource, getLocalSources, previewLocalSource, saveLocalSource } from '../../api/localSources'
import { probeNode } from '../../api/nodes'
import { localMemberRows, localSaveIntent, localReplacementDiff, localPendingMessage } from '../../lib/localSourceDraft'
import type { LocalSourcePreview, LocalSourceSummary } from '../../types/localSource'
import { Button } from '../ui/Button'
import { QueryErrorBanner } from '../ui/QueryErrorBanner'
import { useToast } from '../ui/Toast'

export function LocalSourcesSection() {
  const client = useQueryClient()
  const toast = useToast(s => s.show)
  const query = useQuery({ queryKey: ['local-sources'], queryFn: getLocalSources, refetchInterval: 2000 })
  const status = useQuery({ queryKey: ['local-source-apply-status'], queryFn: () => api.get<{ state: string; local_revision?: number; error?: string }>('/api/reload/status'), refetchInterval: 2000 })
  const [editor, setEditor] = useState<{ id: string | null; revision: number; name: string; enabled: boolean; content: string; original: string | null } | null>(null)
  const [preview, setPreview] = useState<LocalSourcePreview | null>(null)
  const [previewedText, setPreviewedText] = useState<string | null>(null)
  const [membersID, setMembersID] = useState<string | null>(null)
  const membersQuery = useQuery({ queryKey: ['local-source-members', membersID], queryFn: () => getLocalSource(membersID!), enabled: !!membersID, refetchInterval: 2000 })
  const members = membersQuery.data?.source
  const [busy, setBusy] = useState(false)
  const [requestedRevision, setRequestedRevision] = useState<number | null>(null)
  const applying = query.data?.applying || status.data?.state === 'running'
  const blocked = busy || applying
  const run = async (fn: () => Promise<void>) => {
    setBusy(true)
    try { await fn() } catch (e) { toast(e instanceof Error ? e.message : '本地源操作失败', 'error'); await query.refetch() }
    finally { setBusy(false) }
  }
  const refresh = async () => {
    await query.refetch()
    await client.invalidateQueries({ queryKey: ['config-nodes'] })
    await client.invalidateQueries({ queryKey: ['nodes-page'] })
  }
  useEffect(() => {
    if (requestedRevision !== null && query.data && !query.data.pending && query.data.active_revision >= requestedRevision) {
      setRequestedRevision(null)
      void client.invalidateQueries({ queryKey: ['config-nodes'] })
      void client.invalidateQueries({ queryKey: ['nodes-page'] })
    }
  }, [query.data, requestedRevision, client])
  const open = async (row?: LocalSourceSummary) => {
    setPreview(null); setPreviewedText(null)
    if (!row) { setEditor({ id: null, revision: query.data!.revision, name: '', enabled: true, content: '', original: null }); return }
    const detail = await getLocalSource(row.id)
    setEditor({ id: row.id, revision: detail.revision, name: detail.source.name, enabled: detail.source.enabled, content: detail.source.nodes.map(n => n.uri).join('\n'), original: detail.source.nodes.map(n => n.uri).join('\n') })
  }
  const patchContent = (content: string) => { setEditor(prev => prev ? { ...prev, content } : null); setPreview(null); setPreviewedText(null) }
  const intent = editor ? localSaveIntent(editor.original, editor.content, previewedText, !!preview) : null
  const difference = editor?.original !== null && editor ? localReplacementDiff(editor.original, editor.content) : null
  const saveEditor = () => {
    if (!editor || !intent?.allowed || blocked) return
    const captured = editor
    const save = () => run(async () => { await saveLocalSource(captured.id, { expected_revision: captured.revision, name: captured.name.trim(), enabled: captured.enabled, content: intent.content }); setEditor(null); await refresh(); toast('草稿已保存，尚未应用', 'ok') })
    if (intent.confirmReplacement) {
      Modal.confirm({ title: '确认替换本地源成员？', content: `新增 ${difference!.added}、移除 ${difference!.removed}、保留 ${difference!.retained}。只保存草稿，不立即改变代理。`, okText: '确认替换', cancelText: '取消', onOk: save })
    } else void save()
  }
  return <section className="sec" aria-label="命名本地节点源">
    <div className="sec-head"><h2>本地节点源</h2><span className="sec-desc">文本快照分组管理；保存草稿不会改变正在使用的代理。</span>
      <div className="sec-actions">
        <Button disabled={!query.data || blocked} onClick={() => void run(() => open())}>新增本地源</Button>
        <Button variant="primary" disabled={!query.data?.pending || blocked} onClick={() => Modal.confirm({ title: '应用本地源草稿？', content: '重建代理核心可能短暂中断连接。失败时保留草稿并尝试恢复原配置。', okText: '应用', cancelText: '取消', onOk: () => run(async () => { const revision = query.data!.revision; await applyLocalSources(revision); setRequestedRevision(revision); await refresh() }) })}>{applying ? '应用中…' : '应用草稿'}</Button>
      </div>
    </div>
    {query.isError && <QueryErrorBanner title="本地源加载失败" error={query.error} onRetry={() => { void query.refetch() }} />}
    {query.data?.pending && <p role="status">{localPendingMessage(query.data)}</p>}
    {status.data?.local_revision && status.data.state === 'failed' ? <p role="alert">应用版本 {status.data.local_revision} 失败：{status.data.error}</p> : null}
    <p>格式检查、应用结果、代理健康是三个不同状态。离线或未检测的合法成员会一直保留。</p>
    <Table<LocalSourceSummary> rowKey="id" dataSource={query.data?.sources || []} loading={query.isLoading} pagination={false} scroll={{ x: 650 }} columns={[
      { title: '来源名称', dataIndex: 'name' },
      { title: '成员', dataIndex: 'member_count' },
      { title: '启用（草稿）', render: (_, row) => <Switch aria-label={`启用 ${row.name}`} checked={row.enabled} disabled={blocked} onChange={enabled => void run(async () => { await saveLocalSource(row.id, { expected_revision: query.data!.revision, name: row.name, enabled }); await refresh() })} /> },
      { title: '操作', render: (_, row) => <div className="toolbar">
        <Button disabled={blocked} onClick={() => setMembersID(row.id)}>全部成员</Button>
        <Button disabled={blocked} onClick={() => void run(() => open(row))}>编辑 / 替换</Button>
        <Button variant="danger" disabled={blocked} onClick={() => Modal.confirm({ title: `删除本地源「${row.name}」？`, content: '删除先保存为草稿；应用后才移除该来源引用。其他来源仍引用的节点不会被移除。', okText: '删除', cancelText: '取消', onOk: () => run(async () => { await deleteLocalSource(row.id, query.data!.revision); await refresh() }) })}>删除</Button>
      </div> },
    ]} />
    <Modal title={editor?.id ? '编辑本地源' : '新增本地源'} open={!!editor} onCancel={() => { if (!busy) setEditor(null) }} footer={null} destroyOnClose>
      {editor && <form onSubmit={e => { e.preventDefault(); saveEditor() }}>
        <label>来源名称<Input aria-label="本地源名称" maxLength={80} value={editor.name} onChange={e => setEditor({ ...editor, name: e.target.value })} /></label>
        <label>从电脑导入文本（最多 1 MiB）<input aria-label="导入节点文本" type="file" accept=".txt,.list,text/plain" onChange={e => { const file = e.target.files?.[0]; if (!file) return; void run(async () => { if (file.size > 1048576) throw new Error('文本文件不能超过 1 MiB'); patchContent(await file.text()) }) }} /></label>
        <label>逐行协议 URI<Input.TextArea aria-label="本地源节点文本" autoComplete="off" rows={9} value={editor.content} onChange={e => patchContent(e.target.value)} /></label>
        <p>每行一个 URI，支持 # 注释；不读取服务器路径，也不抓取远程地址。</p>
        <Button disabled={blocked || !editor.content.trim()} onClick={() => void run(async () => { const text = editor.content; const result = await previewLocalSource(text, editor.id, editor.revision); setPreview(result); setPreviewedText(text) })}>检查并预览</Button>
        {preview && previewedText === editor.content && <p role="status">格式通过：{preview.member_count} 个成员，{preview.duplicates?.length || 0} 条重复。共享 {preview.shared_count} 个（按当前草稿；非本地来源 {preview.shared_non_local}，其他草稿来源 {preview.shared_draft}，其他已应用来源 {preview.shared_active}；各项可能重叠）。其他来源仍提供时关闭本源不停止节点。{Object.entries(preview.protocols).map(([name, count]) => `${name}: ${count}`).join(' · ')}。尚未测活。</p>}
        {difference && preview && previewedText === editor.content && <p role="status">替换预览：新增 {difference.added}、移除 {difference.removed}、保留 {difference.retained}。</p>}
        <Button htmlType="submit" variant="primary" disabled={blocked || !editor.name.trim() || !intent?.allowed}>保存草稿</Button>
      </form>}
    </Modal>
    <Modal title={`${members?.name || ''} · 全部成员`} open={!!membersID} onCancel={() => setMembersID(null)} footer={null} destroyOnClose>
      <p>保留全部成员。健康状态来自当前运行快照；没有运行出口的草稿/停用成员显示未应用或未运行，不计作检测失败。其他来源仍提供时关闭本源不停止节点。</p>
      {membersQuery.isError && <QueryErrorBanner title="成员状态读取失败" error={membersQuery.error} onRetry={() => { void membersQuery.refetch() }} />}
      <Table rowKey="index" size="small" loading={membersQuery.isLoading} pagination={{ pageSize: 20 }} dataSource={members ? localMemberRows(members, membersQuery.data?.member_health || []) : []} columns={[
        { title: '成员', dataIndex: 'name' }, { title: '端口', dataIndex: 'port' }, { title: '当前健康', dataIndex: 'health' },
        { title: '操作', render: (_, row) => <Button disabled={!row.tag || blocked} onClick={() => void run(async () => { try { await probeNode(row.tag!) } finally { await membersQuery.refetch() } })}>检测</Button> },
      ]} />
    </Modal>
  </section>
}
