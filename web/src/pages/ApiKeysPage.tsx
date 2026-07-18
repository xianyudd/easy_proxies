import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Dropdown, Input, Modal, Select, Switch } from 'antd'
import type { MenuProps } from 'antd'
import {
  Copy,
  Eye,
  EyeOff,
  KeyRound,
  MoreHorizontal,
  Pencil,
  Plus,
  RefreshCw,
  RotateCcw,
  Shield,
  Trash2,
  X,
} from 'lucide-react'
import { createApiKey, deleteApiKey, getSettings, listApiKeys, updateApiKey, type ApiKeyMeta } from '../api/settings'
import { Button } from '../components/ui/Button'
import { Badge } from '../components/ui/Badge'
import { QueryErrorBanner } from '../components/ui/QueryErrorBanner'
import { useToast } from '../components/ui/Toast'
import { copyToClipboard } from '../lib/clipboard'

function maskKey(key?: string, hint?: string) {
  if (hint) return hint
  const value = String(key || '')
  if (!value) return '••••••••'
  if (value.length <= 12) return '•'.repeat(value.length)
  return `${value.slice(0, 8)}${'•'.repeat(8)}${value.slice(-4)}`
}

export function ApiKeysPage() {
  const queryClient = useQueryClient()
  const toast = useToast(s => s.show)
  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })
  const keysQuery = useQuery({ queryKey: ['api-keys'], queryFn: listApiKeys })
  const [nameDraft, setNameDraft] = useState('')
  const [role, setRole] = useState<'read' | 'admin'>('read')
  const [revealed, setRevealed] = useState<Record<string, boolean>>({})
  const [pendingSecret, setPendingSecret] = useState<ApiKeyMeta | null>(null)
  const [busy, setBusy] = useState(false)
  const [renameTarget, setRenameTarget] = useState<string | null>(null)
  const [renameDraft, setRenameDraft] = useState('')
  const [highlightName, setHighlightName] = useState<string | null>(null)

  const passwordSet = Boolean((settings.data?.management as Record<string, unknown> | undefined)?.password_set)
  const keys = useMemo(() => {
    const rows = keysQuery.data?.api_keys
    return Array.isArray(rows) ? rows : []
  }, [keysQuery.data])

  const stats = useMemo(() => ({
    total: keys.length,
    admin: keys.filter(k => k.role === 'admin').length,
    enabled: keys.filter(k => k.enabled !== false).length,
  }), [keys])

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['api-keys'] })
    void queryClient.invalidateQueries({ queryKey: ['settings'] })
  }

  const flashRow = (name?: string) => {
    if (!name) return
    setHighlightName(name)
    window.setTimeout(() => setHighlightName(cur => (cur === name ? null : cur)), 2400)
  }

  const createMut = useMutation({
    mutationFn: async () => {
      if (!passwordSet) {
        throw new Error('请先在「系统设置 → 管理与日志」设置管理密码（可查看/复制），再生成 API Key')
      }
      return createApiKey({
        name: nameDraft.trim() || undefined,
        role,
        enabled: true,
      })
    },
    onMutate: () => setBusy(true),
    onSettled: () => setBusy(false),
    onSuccess: (res) => {
      const ak = res.api_key || null
      setPendingSecret(ak)
      setNameDraft('')
      if (ak?.name) {
        setRevealed(prev => ({ ...prev, [String(ak.name)]: true }))
        flashRow(String(ak.name))
      }
      toast(res.message || 'API Key 已生成', 'ok')
      refresh()
    },
    onError: (e) => toast(e instanceof Error ? e.message : '生成失败', 'error'),
  })

  const updateMut = useMutation({
    mutationFn: (args: { name: string; body: { name?: string; role?: 'read' | 'admin'; enabled?: boolean; rotate?: boolean } }) =>
      updateApiKey(args.name, args.body),
    onSuccess: (res, vars) => {
      const ak = res.api_key
      if (ak?.rotated && ak.key) {
        setPendingSecret(ak)
        if (ak.name) {
          setRevealed(prev => ({ ...prev, [String(ak.name)]: true }))
          flashRow(String(ak.name))
        }
        toast('密钥已轮换，请立即复制新 Key（旧 Key 立即失效）', 'ok')
      } else {
        toast(res.message || '已更新', 'ok')
        flashRow(String(ak?.name || vars.body.name || vars.name))
      }
      if (vars.body.name && vars.body.name !== vars.name) {
        setRevealed(prev => {
          const next = { ...prev }
          const was = next[vars.name]
          delete next[vars.name]
          if (was) next[vars.body.name as string] = true
          return next
        })
      }
      setRenameTarget(null)
      refresh()
    },
    onError: (e) => toast(e instanceof Error ? e.message : '更新失败', 'error'),
  })

  const deleteMut = useMutation({
    mutationFn: (name: string) => deleteApiKey(name),
    onSuccess: (_res, name) => {
      if (pendingSecret?.name === name) setPendingSecret(null)
      setRevealed(prev => {
        const next = { ...prev }
        delete next[name]
        return next
      })
      toast(`已删除 ${name}`, 'ok')
      refresh()
    },
    onError: (e) => toast(e instanceof Error ? e.message : '删除失败', 'error'),
  })

  const copyValue = async (value: string, label: string) => {
    if (!value) return toast('没有可复制的内容', 'error')
    await copyToClipboard(value, toast, `${label}已复制`)
  }

  const acting = busy || createMut.isPending || updateMut.isPending || deleteMut.isPending

  const confirmRotate = (name: string) => {
    Modal.confirm({
      title: `轮换「${name}」的密钥？`,
      content: '旧 Key 将立即失效。新 Key 只会显示一次，请立刻复制保存。',
      okText: '确认轮换',
      okType: 'danger',
      cancelText: '取消',
      onOk: () => updateMut.mutateAsync({ name, body: { rotate: true } }),
    })
  }

  const confirmDelete = (name: string) => {
    Modal.confirm({
      title: `删除 API Key「${name}」？`,
      content: '调用方将立即失效，此操作不可恢复。',
      okText: '删除',
      okType: 'danger',
      cancelText: '取消',
      onOk: () => deleteMut.mutateAsync(name),
    })
  }

  const submitRename = () => {
    if (!renameTarget) return
    const next = renameDraft.trim()
    if (!next) {
      toast('名称不能为空', 'error')
      return
    }
    if (next === renameTarget) {
      setRenameTarget(null)
      return
    }
    updateMut.mutate({ name: renameTarget, body: { name: next } })
  }

  const rowMenu = (row: ApiKeyMeta): MenuProps['items'] => {
    const name = String(row.name || '')
    const full = String(row.key || '')
    const show = !!revealed[name]
    return [
      {
        key: 'toggle-reveal',
        icon: show ? <EyeOff size={14} /> : <Eye size={14} />,
        label: show ? '遮挡密钥' : '显示密钥',
        disabled: !full,
        onClick: () => setRevealed(prev => ({ ...prev, [name]: !prev[name] })),
      },
      {
        key: 'rename',
        icon: <Pencil size={14} />,
        label: '重命名',
        onClick: () => {
          setRenameTarget(name)
          setRenameDraft(name)
        },
      },
      {
        key: 'rotate',
        icon: <RotateCcw size={14} />,
        label: '轮换密钥',
        disabled: acting,
        onClick: () => confirmRotate(name),
      },
      { type: 'divider' },
      {
        key: 'delete',
        icon: <Trash2 size={14} />,
        label: '删除',
        danger: true,
        disabled: acting,
        onClick: () => confirmDelete(name),
      },
    ]
  }

  return (
    <div className="page api-keys-page">
      <div className="page-header">
        <div>
          <div className="eyebrow">Access Control</div>
          <h1>API Key 管理</h1>
          <p>商业化凭证中心：一键签发、遮挡复制、权限与启停、轮换吊销。默认 read，可升为 admin。</p>
        </div>
        <div className="toolbar">
          <Button onClick={() => { void keysQuery.refetch(); void settings.refetch() }} disabled={keysQuery.isFetching}>
            <RefreshCw size={15} />刷新
          </Button>
        </div>
      </div>

      <div className="extractor-flow" aria-label="API Key 使用流程">
        <div className="extractor-flow-step">
          <span>1</span>
          <div>
            <strong>签发</strong>
            <p>生成 read/admin Key</p>
          </div>
        </div>
        <div className="extractor-flow-step">
          <span>2</span>
          <div>
            <strong>复制使用</strong>
            <p>请求头 X-API-Key</p>
          </div>
        </div>
        <div className="extractor-flow-step">
          <span>3</span>
          <div>
            <strong>轮换 / 吊销</strong>
            <p>泄露时立即失效旧 Key</p>
          </div>
        </div>
      </div>

      {(keysQuery.isError || settings.isError) && (
        <QueryErrorBanner
          title="API Key 数据加载失败"
          error={keysQuery.error || settings.error}
          onRetry={() => { void keysQuery.refetch(); void settings.refetch() }}
        />
      )}

      <div className="settings-status-grid quality-cache-grid api-key-stats" style={{ marginBottom: 16 }}>
        <div className="status-card"><KeyRound size={16} /><span>全部</span><strong>{stats.total}</strong></div>
        <div className="status-card"><KeyRound size={16} /><span>启用中</span><strong>{stats.enabled}</strong></div>
        <div className="status-card"><Shield size={16} /><span>admin</span><strong>{stats.admin}</strong></div>
      </div>

      {pendingSecret?.key && (
        <div className="settings-alert modern-settings-alert api-key-pending-secret" role="status">
          <div className="api-key-pending-head">
            <strong>{pendingSecret.rotated ? '轮换后的新 Key（请立即复制）' : '新建 Key 明文（请立即复制）'}</strong>
            <Button onClick={() => setPendingSecret(null)} title="关闭">
              <X size={14} />
            </Button>
          </div>
          <div className="mono api-key-pending-value">{pendingSecret.key}</div>
          <div className="toolbar">
            <Button variant="primary" onClick={() => void copyValue(String(pendingSecret.key), 'API Key')}>
              <Copy size={14} />复制 Key
            </Button>
            <span className="settings-helper-text">
              {pendingSecret.name} · {pendingSecret.role || 'read'} · 关闭后列表默认遮挡显示
            </span>
          </div>
        </div>
      )}

      <div className="workspace-grid api-keys-workspace">
        <section className="panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">签发新 Key</div>
              <div className="panel-subtitle">自动生成密钥，无需手填。需先设置管理密码。</div>
            </div>
          </div>
          <div className="field settings-form-item">
            <label>名称（可选）</label>
            <Input
              className="settings-input"
              placeholder="例如 mobile-app / ops-ci"
              value={nameDraft}
              onChange={e => setNameDraft(e.target.value)}
              maxLength={64}
            />
          </div>
          <div className="field settings-form-item" style={{ marginTop: 12 }}>
            <label>角色</label>
            <Select
              className="settings-input"
              value={role}
              onChange={v => setRole(v as 'read' | 'admin')}
              options={[
                { value: 'read', label: 'read · 只读（推荐对外）' },
                { value: 'admin', label: 'admin · 完整管理' },
              ]}
              style={{ width: '100%' }}
            />
          </div>
          <div className="settings-inline-note" style={{ marginTop: 12 }}>
            <Badge tone={passwordSet ? 'good' : 'warn'}>{passwordSet ? '管理密码已设置' : '请先设置管理密码'}</Badge>
            <span>请求头：<code>X-API-Key: epk_…</code></span>
          </div>
          <div className="toolbar" style={{ marginTop: 16 }}>
            <Button variant="primary" disabled={acting || !passwordSet} onClick={() => createMut.mutate()}>
              <Plus size={15} />{createMut.isPending ? '生成中…' : '一键生成 API Key'}
            </Button>
          </div>
        </section>

        <section className="panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">已签发凭证</div>
              <div className="panel-subtitle">主操作：复制 / 启停。更多：显示、重命名、轮换、删除。</div>
            </div>
          </div>

          {!keys.length ? (
            <div className="empty-state compact-empty">
              <strong>还没有 API Key</strong>
              <span>在左侧点「一键生成 API Key」，即可获得可复制的访问凭证。</span>
            </div>
          ) : (
            <div className="api-key-table">
              <div className="api-key-table-head">
                <span>名称 / 角色</span>
                <span>密钥</span>
                <span>状态</span>
                <span>操作</span>
              </div>
              {keys.map(row => {
                const name = String(row.name || '')
                const full = String(row.key || '')
                const show = !!revealed[name]
                const display = show && full ? full : maskKey(full, row.hint)
                const enabled = row.enabled !== false
                const isAdmin = row.role === 'admin'
                return (
                  <div
                    className={`api-key-table-row${highlightName === name ? ' is-highlight' : ''}${!enabled ? ' is-disabled' : ''}`}
                    key={name || full}
                  >
                    <div className="api-key-meta">
                      <strong title={name}>{name || '未命名'}</strong>
                      <Select
                        size="small"
                        value={(isAdmin ? 'admin' : 'read') as 'read' | 'admin'}
                        style={{ width: 108 }}
                        disabled={acting}
                        options={[
                          { value: 'read', label: 'read' },
                          { value: 'admin', label: 'admin' },
                        ]}
                        onChange={(v) => {
                          if (v === row.role) return
                          updateMut.mutate({ name, body: { role: v as 'read' | 'admin' } })
                        }}
                      />
                    </div>
                    <div className="api-key-secret mono" title={show ? full : '已遮挡'}>{display || '••••••••'}</div>
                    <div className="api-key-status">
                      <Switch
                        size="small"
                        checked={enabled}
                        disabled={acting}
                        onChange={(checked) => updateMut.mutate({ name, body: { enabled: checked } })}
                      />
                      <span className="settings-helper-text">{enabled ? '启用' : '禁用'}</span>
                    </div>
                    <div className="api-key-actions toolbar">
                      <Button onClick={() => void copyValue(full, 'API Key')} disabled={!full} title="复制完整 Key">
                        <Copy size={14} />复制
                      </Button>
                      <Dropdown menu={{ items: rowMenu(row) }} trigger={['click']} placement="bottomRight">
                        <Button disabled={acting} title="更多操作">
                          <MoreHorizontal size={14} />更多
                        </Button>
                      </Dropdown>
                    </div>
                  </div>
                )
              })}
            </div>
          )}

          <div className="settings-inline-note" style={{ marginTop: 16 }}>
            <span>
              <strong>read</strong>：nodes / extractor / 状态。
              <strong> admin</strong>：完整管理。
              禁用、轮换、删除均立即生效。
            </span>
          </div>
        </section>
      </div>

      <Modal
        title="重命名 API Key"
        open={!!renameTarget}
        onOk={submitRename}
        onCancel={() => setRenameTarget(null)}
        okText="保存"
        cancelText="取消"
        confirmLoading={updateMut.isPending}
        destroyOnHidden
      >
        <div className="field settings-form-item">
          <label>新名称</label>
          <Input
            value={renameDraft}
            maxLength={64}
            onChange={e => setRenameDraft(e.target.value)}
            onPressEnter={submitRename}
            placeholder="输入新名称"
            autoFocus
          />
        </div>
      </Modal>
    </div>
  )
}
