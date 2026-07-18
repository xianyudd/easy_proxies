import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Input, Select } from 'antd'
import { Copy, Eye, EyeOff, KeyRound, Plus, RefreshCw, Shield, Trash2 } from 'lucide-react'
import { createApiKey, deleteApiKey, getSettings, listApiKeys, type ApiKeyMeta } from '../api/settings'
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

function roleTone(role?: string): 'good' | 'warn' | 'neutral' | 'info' {
  return role === 'admin' ? 'warn' : 'info'
}

export function ApiKeysPage() {
  const queryClient = useQueryClient()
  const toast = useToast(s => s.show)
  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })
  const keysQuery = useQuery({ queryKey: ['api-keys'], queryFn: listApiKeys })
  const [nameDraft, setNameDraft] = useState('')
  const [role, setRole] = useState<'read' | 'admin'>('read')
  const [revealed, setRevealed] = useState<Record<string, boolean>>({})
  const [lastCreated, setLastCreated] = useState<ApiKeyMeta | null>(null)
  const [busy, setBusy] = useState(false)

  const passwordSet = Boolean((settings.data?.management as Record<string, unknown> | undefined)?.password_set)
  const keys = useMemo(() => {
    const rows = keysQuery.data?.api_keys
    return Array.isArray(rows) ? rows : []
  }, [keysQuery.data])

  const stats = useMemo(() => ({
    total: keys.length,
    read: keys.filter(k => (k.role || 'read') === 'read').length,
    admin: keys.filter(k => k.role === 'admin').length,
    enabled: keys.filter(k => k.enabled !== false).length,
  }), [keys])

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['api-keys'] })
    void queryClient.invalidateQueries({ queryKey: ['settings'] })
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
      setLastCreated(res.api_key || null)
      setNameDraft('')
      if (res.api_key?.name) {
        setRevealed(prev => ({ ...prev, [String(res.api_key?.name)]: true }))
      }
      toast(res.message || 'API Key 已生成', 'ok')
      refresh()
    },
    onError: (e) => toast(e instanceof Error ? e.message : '生成失败', 'error'),
  })

  const deleteMut = useMutation({
    mutationFn: (name: string) => deleteApiKey(name),
    onSuccess: (_res, name) => {
      if (lastCreated?.name === name) setLastCreated(null)
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

  return (
    <div className="page api-keys-page">
      <div className="page-header">
        <div>
          <div className="eyebrow">Access Control</div>
          <h1>API Key 管理</h1>
          <p>商业化访问凭证中心：一键签发、遮挡展示、一键复制、即时吊销。默认 read，可升级为 admin。</p>
        </div>
        <div className="toolbar">
          <Button onClick={() => { void keysQuery.refetch(); void settings.refetch() }} disabled={keysQuery.isFetching}>
            <RefreshCw size={15} />刷新
          </Button>
        </div>
      </div>

      {(keysQuery.isError || settings.isError) && (
        <QueryErrorBanner
          title="API Key 数据加载失败"
          error={keysQuery.error || settings.error}
          onRetry={() => { void keysQuery.refetch(); void settings.refetch() }}
        />
      )}

      <div className="settings-status-grid quality-cache-grid" style={{ marginBottom: 16 }}>
        <div className="status-card"><KeyRound size={16} /><span>全部 Key</span><strong>{stats.total}</strong></div>
        <div className="status-card"><Shield size={16} /><span>read</span><strong>{stats.read}</strong></div>
        <div className="status-card"><Shield size={16} /><span>admin</span><strong>{stats.admin}</strong></div>
        <div className="status-card"><KeyRound size={16} /><span>启用中</span><strong>{stats.enabled}</strong></div>
      </div>

      <div className="workspace-grid" style={{ display: 'grid', gridTemplateColumns: 'minmax(280px, 360px) minmax(0, 1fr)', gap: 16 }}>
        <section className="panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">签发新 Key</div>
              <div className="panel-subtitle">无需手填密钥。需先在系统设置中配置管理密码。</div>
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
            <span>请求头：<code>X-API-Key: epk_…</code> · 密码可在系统设置中查看/复制</span>
          </div>
          <div className="toolbar" style={{ marginTop: 16 }}>
            <Button variant="primary" disabled={busy || createMut.isPending || !passwordSet} onClick={() => createMut.mutate()}>
              <Plus size={15} />{busy || createMut.isPending ? '生成中…' : '一键生成 API Key'}
            </Button>
          </div>

          {lastCreated?.key && (
            <div className="settings-alert modern-settings-alert" role="status" style={{ marginTop: 12 }}>
              <strong>新建 Key 明文</strong>
              <div className="mono" style={{ wordBreak: 'break-all', margin: '8px 0' }}>{lastCreated.key}</div>
              <div className="toolbar">
                <Button onClick={() => void copyValue(String(lastCreated.key), 'API Key')}><Copy size={14} />复制 Key</Button>
                <span className="settings-helper-text">{lastCreated.name} · {lastCreated.role}</span>
              </div>
            </div>
          )}
        </section>

        <section className="panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">已签发凭证</div>
              <div className="panel-subtitle">默认遮挡显示，可临时查看并一键复制。</div>
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
                return (
                  <div className="api-key-table-row" key={name || full}>
                    <div className="api-key-meta">
                      <strong>{name || '未命名'}</strong>
                      <Badge tone={roleTone(row.role)}>{row.role || 'read'}</Badge>
                    </div>
                    <div className="api-key-secret mono" title={show ? full : '已遮挡'}>{display || '••••••••'}</div>
                    <div>
                      <Badge tone={row.enabled === false ? 'neutral' : 'good'}>
                        {row.enabled === false ? '禁用' : '启用'}
                      </Badge>
                    </div>
                    <div className="api-key-actions toolbar">
                      <Button
                        onClick={() => setRevealed(prev => ({ ...prev, [name]: !prev[name] }))}
                        title={show ? '遮挡' : '显示'}
                        disabled={!full}
                      >
                        {show ? <EyeOff size={14} /> : <Eye size={14} />}
                        {show ? '遮挡' : '显示'}
                      </Button>
                      <Button
                        onClick={() => void copyValue(full, 'API Key')}
                        disabled={!full}
                        title={!full ? '当前会话无法读取明文，请重新生成' : '复制完整 Key'}
                      >
                        <Copy size={14} />复制
                      </Button>
                      <Button
                        variant="danger"
                        disabled={deleteMut.isPending}
                        onClick={() => {
                          if (!name) return
                          if (!window.confirm(`删除 API Key「${name}」？调用方将立即失效。`)) return
                          deleteMut.mutate(name)
                        }}
                      >
                        <Trash2 size={14} />删除
                      </Button>
                    </div>
                  </div>
                )
              })}
            </div>
          )}

          <div className="settings-inline-note" style={{ marginTop: 16 }}>
            <span>
              <strong>read</strong>：nodes / extractor / 状态查询（export、reload、reveal 禁止）。
              <strong> admin</strong>：完整管理 API。
            </span>
          </div>
        </section>
      </div>
    </div>
  )
}
