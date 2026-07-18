import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Dropdown, Input, Modal, Progress, Select, Switch } from 'antd'
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
  ShieldCheck,
  Trash2,
} from 'lucide-react'
import { createApiKey, deleteApiKey, getSettings, listApiKeys, updateApiKey, type ApiKeyMeta } from '../api/settings'
import { Button } from '../components/ui/Button'
import { Badge } from '../components/ui/Badge'
import { QueryErrorBanner } from '../components/ui/QueryErrorBanner'
import { useToast } from '../components/ui/Toast'
import { copyToClipboard } from '../lib/clipboard'

/** One-time secret modal auto-closes quickly; copy pauses the timer. */
const SECRET_MODAL_SECONDS = 12

function maskKey(key?: string, hint?: string) {
  if (hint) return hint
  const value = String(key || '')
  if (!value) return '••••••••••••'
  if (value.length <= 12) return '•'.repeat(Math.max(8, value.length))
  return `${value.slice(0, 10)} ··· ${value.slice(-4)}`
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
  const [secretCountdown, setSecretCountdown] = useState(0)
  const [busy, setBusy] = useState(false)
  const [renameTarget, setRenameTarget] = useState<string | null>(null)
  const [renameDraft, setRenameDraft] = useState('')
  const [highlightName, setHighlightName] = useState<string | null>(null)
  const secretTimerRef = useRef<number | undefined>(undefined)

  const passwordSet = Boolean((settings.data?.management as Record<string, unknown> | undefined)?.password_set)
  const keys = useMemo(() => {
    const rows = keysQuery.data?.api_keys
    return Array.isArray(rows) ? rows : []
  }, [keysQuery.data])

  const stats = useMemo(() => ({
    total: keys.length,
    admin: keys.filter(k => k.role === 'admin').length,
    enabled: keys.filter(k => k.enabled !== false).length,
    disabled: keys.filter(k => k.enabled === false).length,
  }), [keys])

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['api-keys'] })
    void queryClient.invalidateQueries({ queryKey: ['settings'] })
  }

  const flashRow = (name?: string) => {
    if (!name) return
    setHighlightName(name)
    window.setTimeout(() => setHighlightName(cur => (cur === name ? null : cur)), 2800)
  }

  const clearSecretTimer = () => {
    if (secretTimerRef.current !== undefined) {
      window.clearInterval(secretTimerRef.current)
      secretTimerRef.current = undefined
    }
  }

  const closeSecretModal = () => {
    clearSecretTimer()
    setPendingSecret(null)
    setSecretCountdown(0)
  }

  const openSecretModal = (ak: ApiKeyMeta | null | undefined) => {
    if (!ak?.key) return
    clearSecretTimer()
    setPendingSecret(ak)
    setSecretCountdown(SECRET_MODAL_SECONDS)
    if (ak.name) {
      setRevealed(prev => ({ ...prev, [String(ak.name)]: false }))
      flashRow(String(ak.name))
    }
  }

  useEffect(() => {
    if (!pendingSecret?.key) {
      clearSecretTimer()
      return
    }
    clearSecretTimer()
    secretTimerRef.current = window.setInterval(() => {
      setSecretCountdown((left) => {
        if (left <= 1) {
          clearSecretTimer()
          setPendingSecret(null)
          return 0
        }
        return left - 1
      })
    }, 1000)
    return clearSecretTimer
  }, [pendingSecret?.key, pendingSecret?.name])

  const createMut = useMutation({
    mutationFn: async () => {
      if (!passwordSet) {
        throw new Error('请先在「系统设置 → 管理与日志」设置管理密码，再生成 API Key')
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
      setNameDraft('')
      openSecretModal(res.api_key)
      toast('密钥已生成，请在弹窗中复制', 'ok')
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
        openSecretModal(ak)
        toast('密钥已轮换，请复制新 Key', 'ok')
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
      if (pendingSecret?.name === name) closeSecretModal()
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

  const copySecret = async () => {
    if (!pendingSecret?.key) return
    await copyValue(String(pendingSecret.key), 'API Key')
    closeSecretModal()
  }

  const acting = busy || createMut.isPending || updateMut.isPending || deleteMut.isPending

  const confirmRotate = (name: string) => {
    Modal.confirm({
      title: `轮换「${name}」？`,
      content: '旧 Key 立即失效。新密钥只会在弹窗中显示一次。',
      okText: '确认轮换',
      okType: 'danger',
      cancelText: '取消',
      centered: true,
      onOk: () => updateMut.mutateAsync({ name, body: { rotate: true } }),
    })
  }

  const confirmDelete = (name: string) => {
    Modal.confirm({
      title: `删除「${name}」？`,
      content: '调用方将立即失效，且无法恢复。',
      okText: '删除',
      okType: 'danger',
      cancelText: '取消',
      centered: true,
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

  const secretProgress = secretCountdown > 0
    ? Math.round((secretCountdown / SECRET_MODAL_SECONDS) * 100)
    : 0

  return (
    <div className="page api-keys-page">
      <section className="api-keys-hero">
        <div className="api-keys-hero-copy">
          <div className="eyebrow">Access Control</div>
          <h1>API Key</h1>
          <p>
            为脚本与外部服务签发访问凭证。默认 <strong>read</strong> 只读拉代理；
            <strong> admin</strong> 拥有完整管理权限。密钥明文仅在创建/轮换时显示一次。
          </p>
          <div className="api-keys-hero-pills">
            <span>X-API-Key</span>
            <span>read / admin</span>
            <span>即时吊销</span>
          </div>
        </div>
        <div className="api-keys-hero-stats">
          <div>
            <span>全部</span>
            <strong>{stats.total}</strong>
          </div>
          <div>
            <span>启用</span>
            <strong>{stats.enabled}</strong>
          </div>
          <div>
            <span>Admin</span>
            <strong>{stats.admin}</strong>
          </div>
          <div>
            <span>禁用</span>
            <strong>{stats.disabled}</strong>
          </div>
        </div>
      </section>

      <div className="extractor-flow api-keys-flow" aria-label="使用流程">
        <div className="extractor-flow-step">
          <span>1</span>
          <div>
            <strong>签发</strong>
            <p>生成 read 或 admin</p>
          </div>
        </div>
        <div className="extractor-flow-step">
          <span>2</span>
          <div>
            <strong>复制使用</strong>
            <p>Header: X-API-Key</p>
          </div>
        </div>
        <div className="extractor-flow-step">
          <span>3</span>
          <div>
            <strong>轮换 / 吊销</strong>
            <p>泄露时立即失效</p>
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

      <div className="api-keys-workspace">
        <section className="panel api-keys-issue-panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">签发凭证</div>
              <div className="panel-subtitle">自动生成安全密钥，无需手填。</div>
            </div>
            <Button onClick={() => { void keysQuery.refetch(); void settings.refetch() }} disabled={keysQuery.isFetching}>
              <RefreshCw size={15} />
            </Button>
          </div>

          <div className="api-keys-issue-form">
            <div className="field">
              <label>名称</label>
              <Input
                size="large"
                placeholder="可选，如 mobile-app"
                value={nameDraft}
                onChange={e => setNameDraft(e.target.value)}
                maxLength={64}
                allowClear
              />
            </div>
            <div className="field">
              <label>权限角色</label>
              <Select
                size="large"
                value={role}
                onChange={v => setRole(v as 'read' | 'admin')}
                style={{ width: '100%' }}
                options={[
                  { value: 'read', label: 'read · 只读（推荐对外）' },
                  { value: 'admin', label: 'admin · 完整管理' },
                ]}
              />
            </div>

            <div className={`api-keys-auth-chip ${passwordSet ? 'is-ok' : 'is-warn'}`}>
              {passwordSet ? <ShieldCheck size={15} /> : <Shield size={15} />}
              <div>
                <strong>{passwordSet ? '管理密码已就绪' : '需要管理密码'}</strong>
                <span>{passwordSet ? '可安全签发 API Key' : '请先到系统设置中配置'}</span>
              </div>
            </div>

            <Button
              variant="primary"
              size="large"
              className="api-keys-issue-btn"
              disabled={acting || !passwordSet}
              onClick={() => createMut.mutate()}
            >
              <Plus size={16} />
              {createMut.isPending ? '生成中…' : '一键生成 API Key'}
            </Button>

            <div className="api-keys-issue-hint mono">
              curl -H &apos;X-API-Key: epk_…&apos; http://host:9091/api/extractor?...
            </div>
          </div>
        </section>

        <section className="panel api-keys-list-panel">
          <div className="panel-header">
            <div>
              <div className="panel-title">凭证列表</div>
              <div className="panel-subtitle">
                主操作：复制 / 启停。更多：显示、重命名、轮换、删除。
              </div>
            </div>
            <Badge tone={keys.length ? 'info' : 'neutral'}>{keys.length} keys</Badge>
          </div>

          {!keys.length ? (
            <div className="api-keys-empty">
              <div className="api-keys-empty-icon"><KeyRound size={28} /></div>
              <strong>还没有 API Key</strong>
              <p>在左侧生成第一把密钥。创建后会弹出可复制的明文窗口，{SECRET_MODAL_SECONDS}s 后自动关闭。</p>
            </div>
          ) : (
            <div className="api-key-list">
              {keys.map(row => {
                const name = String(row.name || '')
                const full = String(row.key || '')
                const show = !!revealed[name]
                const display = show && full ? full : maskKey(full, row.hint)
                const enabled = row.enabled !== false
                const isAdmin = row.role === 'admin'
                return (
                  <article
                    key={name || full}
                    className={`api-key-card${highlightName === name ? ' is-highlight' : ''}${!enabled ? ' is-disabled' : ''}`}
                  >
                    <div className="api-key-card-main">
                      <div className="api-key-card-title">
                        <strong title={name}>{name || '未命名'}</strong>
                        <Select
                          size="small"
                          value={(isAdmin ? 'admin' : 'read') as 'read' | 'admin'}
                          className="api-key-role-select"
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
                      <div className="api-key-card-secret mono" title={show ? full : '已遮挡'}>
                        {display || '••••••••••••'}
                      </div>
                    </div>
                    <div className="api-key-card-side">
                      <div className="api-key-card-toggle">
                        <Switch
                          checked={enabled}
                          disabled={acting}
                          onChange={(checked) => updateMut.mutate({ name, body: { enabled: checked } })}
                        />
                        <span>{enabled ? '启用' : '禁用'}</span>
                      </div>
                      <div className="api-key-card-actions">
                        <Button onClick={() => void copyValue(full, 'API Key')} disabled={!full}>
                          <Copy size={14} />复制
                        </Button>
                        <Dropdown menu={{ items: rowMenu(row) }} trigger={['click']} placement="bottomRight">
                          <Button disabled={acting}>
                            <MoreHorizontal size={14} />
                          </Button>
                        </Dropdown>
                      </div>
                    </div>
                  </article>
                )
              })}
            </div>
          )}
        </section>
      </div>

      {/* One-time secret reveal — modal + countdown (not a toast) */}
      <Modal
        open={!!pendingSecret?.key}
        title={null}
        footer={null}
        centered
        width={520}
        destroyOnHidden
        maskClosable={false}
        onCancel={closeSecretModal}
        className="api-key-secret-modal"
      >
        <div className="api-key-secret-modal-body">
          <div className="api-key-secret-modal-icon">
            <KeyRound size={22} />
          </div>
          <h2>{pendingSecret?.rotated ? '新密钥已轮换' : 'API Key 已创建'}</h2>
          <p className="api-key-secret-modal-desc">
            明文仅此一次显示。关闭或倒计时结束后无法再次查看完整密钥，请立即复制保存。
          </p>
          <div className="api-key-secret-meta">
            <Badge tone={pendingSecret?.role === 'admin' ? 'warn' : 'info'}>
              {pendingSecret?.role || 'read'}
            </Badge>
            <span className="mono">{pendingSecret?.name}</span>
          </div>
          <div className="api-key-secret-box mono">{pendingSecret?.key}</div>
          <div className="api-key-secret-actions">
            <Button variant="primary" size="large" onClick={() => void copySecret()}>
              <Copy size={16} />复制密钥
            </Button>
            <Button size="large" onClick={closeSecretModal}>
              完成
            </Button>
          </div>
          <div className="api-key-secret-timer">
            <Progress
              percent={secretProgress}
              showInfo={false}
              size="small"
              strokeColor="var(--primary)"
              trailColor="color-mix(in srgb, var(--border) 70%, transparent)"
            />
            <span>{secretCountdown}s 后自动关闭</span>
          </div>
        </div>
      </Modal>

      <Modal
        title="重命名 API Key"
        open={!!renameTarget}
        onOk={submitRename}
        onCancel={() => setRenameTarget(null)}
        okText="保存"
        cancelText="取消"
        confirmLoading={updateMut.isPending}
        destroyOnHidden
        centered
      >
        <div className="field" style={{ marginTop: 8 }}>
          <label>新名称</label>
          <Input
            size="large"
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
