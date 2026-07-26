import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Dropdown, Input, Modal, Progress, Select, Switch, Table } from 'antd'
import type { ColumnsType, TableProps } from 'antd/es/table'
import type { MenuProps } from 'antd'
import {
  Copy,
  Eye,
  EyeOff,
  KeyRound,
  MoreHorizontal,
  Pause,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  RotateCcw,
  Shield,
  ShieldCheck,
  Terminal,
  Trash2,
  ChevronDown,
  ChevronRight,
  X,
} from 'lucide-react'
import {
  createApiKey,
  deleteApiKey,
  getSettings,
  listApiKeys,
  runBulkApiKeyAction,
  updateApiKey,
  type ApiKeyBulkAction,
  type ApiKeyMeta,
} from '../api/settings'
import { Page } from '../components/layout/Page'
import { Button } from '../components/ui/Button'
import { Badge } from '../components/ui/Badge'
import { QueryErrorBanner } from '../components/ui/QueryErrorBanner'
import { useToast } from '../components/ui/Toast'
import { copyToClipboard } from '../lib/clipboard'

/** One-time secret modal auto-closes quickly; copy closes immediately. */
const SECRET_MODAL_SECONDS = 12
/** Show multi-select + bulk bar only when managing multiple credentials. */
const BULK_THRESHOLD = 2

type PlaySample = {
  id: string
  label: string
  method: 'GET'
  path: string
  needAdmin?: boolean
  hint: string
}

const PLAY_SAMPLES: PlaySample[] = [
  {
    id: 'nodes',
    label: '节点列表',
    method: 'GET',
    path: '/api/nodes?page=1&page_size=3',
    hint: 'read 可用',
  },
  {
    id: 'extractor',
    label: '提取代理',
    method: 'GET',
    path: '/api/extractor?region=all&mode=pool&format=http_url&count=1',
    hint: 'read 可用 · 默认打码',
  },
  {
    id: 'auth-status',
    label: '鉴权状态',
    method: 'GET',
    path: '/api/auth/status',
    hint: '任意 Key',
  },
  {
    id: 'export',
    label: '导出明文',
    method: 'GET',
    path: '/api/export?scheme=http',
    needAdmin: true,
    hint: '需 admin',
  },
]

type PlayResult = {
  status: number
  ms: number
  body: string
  contentType: string
  bytes: number
  sampleId: string
  at: number
}

function formatPlayBody(raw: string, contentType: string): { text: string; kind: 'json' | 'text' } {
  const trimmed = raw.trim()
  if (!trimmed) return { text: '(empty)', kind: 'text' }
  const looksJson = contentType.includes('json') || trimmed.startsWith('{') || trimmed.startsWith('[')
  if (looksJson) {
    try {
      return { text: JSON.stringify(JSON.parse(trimmed), null, 2), kind: 'json' }
    } catch {
      /* fall through */
    }
  }
  return { text: raw, kind: 'text' }
}

function playStatusTone(status: number): 'good' | 'warn' | 'bad' | 'neutral' {
  if (status >= 200 && status < 300) return 'good'
  if (status === 401 || status === 403) return 'warn'
  if (status === 0 || status >= 500) return 'bad'
  if (status >= 400) return 'warn'
  return 'neutral'
}

function playStatusLabel(status: number): string {
  if (status === 0) return '网络错误'
  if (status === 200) return 'OK'
  if (status === 401) return '未授权'
  if (status === 403) return '权限不足'
  if (status === 404) return '未找到'
  if (status >= 500) return '服务错误'
  if (status >= 400) return '请求错误'
  return '已响应'
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

function maskKey(key?: string, hint?: string) {
  if (hint) return hint
  const value = String(key || '')
  if (!value) return '••••••••••••'
  if (value.length <= 12) return '•'.repeat(Math.max(8, value.length))
  return `${value.slice(0, 10)} ··· ${value.slice(-4)}`
}

function rowKeyOf(row: ApiKeyMeta, index: number) {
  return String(row.name || row.hint || `key-${index}`)
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
  const [secretPaused, setSecretPaused] = useState(false)
  const [busy, setBusy] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [renameTarget, setRenameTarget] = useState<string | null>(null)
  const [renameDraft, setRenameDraft] = useState('')
  const [highlightName, setHighlightName] = useState<string | null>(null)
  const [selectedNames, setSelectedNames] = useState<string[]>([])
  const [playKeyName, setPlayKeyName] = useState<string>('')
  const [playSampleId, setPlaySampleId] = useState<string>(PLAY_SAMPLES[0].id)
  const [playRunning, setPlayRunning] = useState(false)
  const [playResult, setPlayResult] = useState<PlayResult | null>(null)
  const [playOpen, setPlayOpen] = useState(false)
  const secretTimerRef = useRef<number | undefined>(undefined)

  const passwordSet = Boolean((settings.data?.management as Record<string, unknown> | undefined)?.password_set)
  const keys = useMemo(() => {
    const rows = keysQuery.data?.api_keys
    return Array.isArray(rows) ? rows : []
  }, [keysQuery.data])

  const bulkEnabled = keys.length >= BULK_THRESHOLD

  // Default playground key to first available secret-bearing key.
  useEffect(() => {
    if (!keys.length) {
      setPlayKeyName('')
      return
    }
    if (playKeyName && keys.some(k => k.name === playKeyName)) return
    const first = keys.find(k => k.key) || keys[0]
    setPlayKeyName(String(first?.name || ''))
  }, [keys, playKeyName])

  const playKey = useMemo(
    () => keys.find(k => k.name === playKeyName),
    [keys, playKeyName],
  )
  const playSample = useMemo(
    () => PLAY_SAMPLES.find(s => s.id === playSampleId) || PLAY_SAMPLES[0],
    [playSampleId],
  )
  const playOrigin = typeof window !== 'undefined' ? window.location.origin : 'http://127.0.0.1:9091'
  const playCurl = useMemo(() => {
    const key = playKey?.key?.trim()
    const token = key || '<YOUR_API_KEY>'
    return `curl -sS -H 'X-API-Key: ${token}' '${playOrigin}${playSample.path}'`
  }, [playKey?.key, playOrigin, playSample.path])

  const playView = useMemo(() => {
    if (!playResult) return null
    const formatted = formatPlayBody(playResult.body, playResult.contentType)
    return {
      ...playResult,
      display: formatted.text,
      kind: formatted.kind,
      tone: playStatusTone(playResult.status),
      label: playStatusLabel(playResult.status),
      sampleLabel: PLAY_SAMPLES.find(s => s.id === playResult.sampleId)?.label || playResult.sampleId,
    }
  }, [playResult])

  const runPlayCommand = async () => {
    const key = playKey?.key?.trim()
    if (!key) {
      toast('请选择一把含明文的 API Key（需 admin 登录后列表可见）', 'error')
      return
    }
    if (playKey?.enabled === false) {
      toast('该 Key 已禁用，请先启用', 'error')
      return
    }
    if (playSample.needAdmin && playKey?.role !== 'admin') {
      toast('该示例需要 admin 角色 Key', 'error')
      return
    }
    setPlayRunning(true)
    setPlayResult(null)
    const t0 = performance.now()
    try {
      const res = await fetch(`${playOrigin}${playSample.path}`, {
        method: playSample.method,
        headers: {
          Accept: 'application/json, text/plain, */*',
          'X-API-Key': key,
        },
      })
      const buf = await res.arrayBuffer()
      const bytes = buf.byteLength
      const text = new TextDecoder('utf-8').decode(buf)
      const ms = Math.round(performance.now() - t0)
      const body = text.length > 8000 ? `${text.slice(0, 8000)}\n…(已截断，共 ${formatBytes(bytes)})` : text
      setPlayResult({
        status: res.status,
        ms,
        body,
        contentType: res.headers.get('content-type') || '',
        bytes,
        sampleId: playSample.id,
        at: Date.now(),
      })
      if (res.ok) toast(`试跑成功 ${res.status} · ${ms}ms`, 'ok')
      else toast(`试跑返回 ${res.status} · ${playStatusLabel(res.status)}`, 'error')
    } catch (e) {
      const ms = Math.round(performance.now() - t0)
      setPlayResult({
        status: 0,
        ms,
        body: e instanceof Error ? e.message : '请求失败',
        contentType: '',
        bytes: 0,
        sampleId: playSample.id,
        at: Date.now(),
      })
      toast('试跑失败', 'error')
    } finally {
      setPlayRunning(false)
    }
  }

  const stats = useMemo(() => ({
    total: keys.length,
    admin: keys.filter(k => k.role === 'admin').length,
    enabled: keys.filter(k => k.enabled !== false).length,
    disabled: keys.filter(k => k.enabled === false).length,
  }), [keys])

  // Drop selections that no longer exist; clear selection when bulk UI hides.
  useEffect(() => {
    if (!bulkEnabled) {
      setSelectedNames([])
      return
    }
    const alive = new Set(keys.map((k, i) => rowKeyOf(k, i)))
    setSelectedNames(prev => prev.filter(n => alive.has(n)))
  }, [keys, bulkEnabled])

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
    setSecretPaused(false)
  }

  const openSecretModal = (ak: ApiKeyMeta | null | undefined) => {
    if (!ak?.key) return
    clearSecretTimer()
    setPendingSecret(ak)
    setSecretCountdown(SECRET_MODAL_SECONDS)
    setSecretPaused(false)
    if (ak.name) {
      setRevealed(prev => ({ ...prev, [String(ak.name)]: false }))
      flashRow(String(ak.name))
    }
  }

  useEffect(() => {
    if (!pendingSecret?.key || secretPaused) {
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
  }, [pendingSecret?.key, pendingSecret?.name, secretPaused])

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
        setSelectedNames(prev => prev.map(n => (n === vars.name ? String(vars.body.name) : n)))
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
      setSelectedNames(prev => prev.filter(n => n !== name))
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

  const acting = busy || bulkBusy || createMut.isPending || updateMut.isPending || deleteMut.isPending

  const runBulk = async (action: ApiKeyBulkAction, label: string) => {
    if (!selectedNames.length) return
    // Only touch keys that actually need the change (avoid "success 2" no-ops).
    let targets = selectedNames
    if (action.type === 'enable') {
      targets = selectedRows.filter(k => k.enabled === false).map(k => String(k.name || '')).filter(Boolean)
    } else if (action.type === 'disable') {
      targets = selectedRows.filter(k => k.enabled !== false).map(k => String(k.name || '')).filter(Boolean)
    } else if (action.type === 'set_role') {
      targets = selectedRows
        .filter(k => (k.role || 'read') !== action.role)
        .map(k => String(k.name || ''))
        .filter(Boolean)
    }
    if (!targets.length) {
      toast('所选密钥已是目标状态，无需变更', 'info')
      return
    }
    setBulkBusy(true)
    try {
      const result = await runBulkApiKeyAction(targets, action)
      if (result.fail === 0) {
        toast(`${label}成功（${result.ok}）`, 'ok')
      } else {
        toast(`${label}完成：成功 ${result.ok}，失败 ${result.fail}`, result.ok ? 'info' : 'error')
      }
      setSelectedNames([])
      refresh()
    } catch (e) {
      toast(e instanceof Error ? e.message : `${label}失败`, 'error')
    } finally {
      setBulkBusy(false)
    }
  }

  const confirmBulk = (action: ApiKeyBulkAction, title: string, content: string, label: string) => {
    Modal.confirm({
      title,
      content,
      okText: '确认',
      okType: action.type === 'delete' ? 'danger' : 'primary',
      cancelText: '取消',
      centered: true,
      onOk: () => runBulk(action, label),
    })
  }

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

  const columns: ColumnsType<ApiKeyMeta> = useMemo(() => [
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      width: 180,
      ellipsis: true,
      render: (value: string | undefined) => <strong className="api-key-name-cell">{value || '未命名'}</strong>,
    },
    {
      title: '角色',
      dataIndex: 'role',
      key: 'role',
      width: 120,
      render: (value: string | undefined, row) => {
        const name = String(row.name || '')
        const current = value === 'admin' ? 'admin' : 'read'
        return (
          <Select
            size="small"
            value={current}
            className="api-key-role-select"
            disabled={acting || !name}
            options={[
              { value: 'read', label: 'read' },
              { value: 'admin', label: 'admin' },
            ]}
            onChange={(v) => {
              if (v === current) return
              updateMut.mutate({ name, body: { role: v as 'read' | 'admin' } })
            }}
          />
        )
      },
    },
    {
      title: '密钥',
      key: 'secret',
      ellipsis: true,
      render: (_: unknown, row) => {
        const name = String(row.name || '')
        const full = String(row.key || '')
        const show = !!revealed[name]
        const display = show && full ? full : maskKey(full, row.hint)
        return (
          <div className="api-key-table-secret mono" title={show ? full : '已遮挡'}>
            {display || '••••••••••••'}
          </div>
        )
      },
    },
    {
      title: '状态',
      key: 'enabled',
      width: 110,
      render: (_: unknown, row) => {
        const name = String(row.name || '')
        const enabled = row.enabled !== false
        return (
          <div className="api-key-table-status">
            <Switch
              size="small"
              checked={enabled}
              disabled={acting || !name}
              onChange={(checked) => updateMut.mutate({ name, body: { enabled: checked } })}
            />
            <span>{enabled ? '启用' : '禁用'}</span>
          </div>
        )
      },
    },
    {
      title: '操作',
      key: 'actions',
      width: 148,
      fixed: 'right',
      render: (_: unknown, row) => {
        const name = String(row.name || '')
        const full = String(row.key || '')
        return (
          <div className="api-key-table-actions">
            <Button onClick={() => void copyValue(full, 'API Key')} disabled={!full} title="复制">
              <Copy size={14} />
            </Button>
            <Dropdown menu={{ items: rowMenu(row) }} trigger={['click']} placement="bottomRight">
              <Button disabled={acting || !name} title="更多">
                <MoreHorizontal size={14} />
              </Button>
            </Dropdown>
          </div>
        )
      },
    },
  ], [acting, revealed, updateMut.isPending, deleteMut.isPending, bulkBusy])

  const rowSelection: TableProps<ApiKeyMeta>['rowSelection'] | undefined = bulkEnabled
    ? {
        selectedRowKeys: selectedNames,
        onChange: (keys) => setSelectedNames(keys.map(String)),
        getCheckboxProps: (row) => ({ disabled: acting || !row.name }),
      }
    : undefined

  const secretProgress = secretCountdown > 0
    ? Math.round((secretCountdown / SECRET_MODAL_SECONDS) * 100)
    : 0

  const selectedCount = selectedNames.length

  const selectedRows = useMemo(
    () => keys.filter((k, i) => selectedNames.includes(rowKeyOf(k, i))),
    [keys, selectedNames],
  )

  const selectedStats = useMemo(() => {
    const total = selectedRows.length
    const enabled = selectedRows.filter(k => k.enabled !== false).length
    const disabled = total - enabled
    const read = selectedRows.filter(k => (k.role || 'read') !== 'admin').length
    const admin = total - read
    return {
      total,
      allEnabled: total > 0 && enabled === total,
      allDisabled: total > 0 && disabled === total,
      allRead: total > 0 && read === total,
      allAdmin: total > 0 && admin === total,
      canEnable: disabled > 0,
      canDisable: enabled > 0,
      canSetRead: admin > 0,
      canSetAdmin: read > 0,
    }
  }, [selectedRows])

  const roleMenu: MenuProps['items'] = [
    {
      key: 'read',
      label: '设为 read（只读）',
      disabled: acting || !selectedStats.canSetRead,
      onClick: () => confirmBulk(
        { type: 'set_role', role: 'read' },
        `将 ${selectedCount} 把 Key 设为 read？`,
        '仅保留只读权限，写操作将立即失败。',
        '批量设为 read',
      ),
    },
    {
      key: 'admin',
      label: '设为 admin（完整管理）',
      disabled: acting || !selectedStats.canSetAdmin,
      onClick: () => confirmBulk(
        { type: 'set_role', role: 'admin' },
        `将 ${selectedCount} 把 Key 设为 admin？`,
        '将获得完整管理权限，请确认调用方可信。',
        '批量设为 admin',
      ),
    },
  ]

  const headerStats = [
    { label: '全部', value: stats.total },
    { label: '启用', value: stats.enabled },
    { label: 'Admin', value: stats.admin },
    ...(stats.disabled > 0 ? [{ label: '禁用', value: stats.disabled }] : []),
  ]

  return (
    <Page
      className="api-keys-page"
      title="API Key"
      description="为脚本与服务签发访问凭证；明文仅在创建/轮换时显示一次。"
      actions={
        <Button onClick={() => { void keysQuery.refetch(); void settings.refetch() }} disabled={keysQuery.isFetching}>
          <RefreshCw size={15} className={keysQuery.isFetching ? 'spin' : undefined} />刷新
        </Button>
      }
      stats={headerStats}
    >

      {(keysQuery.isError || settings.isError) && (
        <QueryErrorBanner
          title="API Key 数据加载失败"
          error={keysQuery.error || settings.error}
          onRetry={() => { void keysQuery.refetch(); void settings.refetch() }}
        />
      )}

      {!passwordSet && (
        <div className="settings-alert modern-settings-alert" role="status">
          <Shield size={16} />
          <div>
            <strong>需要管理密码</strong>
            <span>签发 API Key 前请先到「系统设置 → 管理与日志」配置管理密码。</span>
          </div>
        </div>
      )}

      <div className="api-keys-stack">
        <div className="ftoolbar api-keys-issue-bar" role="region" aria-label="签发凭证">
          <Input
            className="f-grow"
            placeholder="名称（可选，如 mobile-app）"
            value={nameDraft}
            onChange={e => setNameDraft(e.target.value)}
            maxLength={64}
            allowClear
          />
          <Select
            className="f-select"
            value={role}
            onChange={v => setRole(v as 'read' | 'admin')}
            popupMatchSelectWidth={false}
            options={[
              { value: 'read', label: 'read · 只读（推荐对外）' },
              { value: 'admin', label: 'admin · 完整管理' },
            ]}
          />
          <Button
            variant="primary"
            className="api-keys-issue-btn"
            disabled={acting || !passwordSet}
            onClick={() => createMut.mutate()}
          >
            <Plus size={16} />
            {createMut.isPending ? '生成中…' : '生成 API Key'}
          </Button>
          <span className="f-end">{bulkEnabled ? '勾选可批量启用 / 禁用 / 改角色 / 删除' : `${keys.length} keys`}</span>
        </div>

        <section className="sec api-keys-list-panel">

          {selectedCount > 0 && bulkEnabled && (
            <div className="api-key-bulk-bar" role="region" aria-label="批量操作">
              <div className="api-key-bulk-meta">
                <span className="api-key-bulk-count">{selectedCount}</span>
                <div>
                  <strong>已选 {selectedCount} 项</strong>
                  <button type="button" className="api-key-bulk-clear" onClick={() => setSelectedNames([])}>
                    清除选择
                  </button>
                </div>
              </div>
              <div className="api-key-bulk-actions">
                <Button
                  disabled={acting || !selectedStats.canEnable}
                  title={!selectedStats.canEnable ? '所选密钥均已启用' : '批量启用'}
                  onClick={() => void runBulk({ type: 'enable' }, '批量启用')}
                >
                  启用
                </Button>
                <Button
                  disabled={acting || !selectedStats.canDisable}
                  title={!selectedStats.canDisable ? '所选密钥均已禁用' : '批量禁用'}
                  onClick={() => void runBulk({ type: 'disable' }, '批量禁用')}
                >
                  禁用
                </Button>
                <span className="api-key-bulk-sep" aria-hidden />
                <Dropdown menu={{ items: roleMenu }} trigger={['click']} placement="bottomRight" disabled={acting}>
                  <Button disabled={acting || (!selectedStats.canSetRead && !selectedStats.canSetAdmin)}>
                    改角色
                  </Button>
                </Dropdown>
                <span className="api-key-bulk-sep" aria-hidden />
                <Button
                  variant="danger"
                  disabled={acting}
                  onClick={() => confirmBulk(
                    { type: 'delete' },
                    `删除 ${selectedCount} 把 API Key？`,
                    '相关调用方将立即失效，且无法恢复。',
                    '批量删除',
                  )}
                >
                  <Trash2 size={14} />删除
                </Button>
              </div>
            </div>
          )}

          {!keys.length ? (
            <div className="api-keys-empty">
              <div className="api-keys-empty-icon"><KeyRound size={28} /></div>
              <strong>还没有 API Key</strong>
              <p>在上方签发条生成第一把密钥。创建后会弹出可复制的明文窗口，{SECRET_MODAL_SECONDS}s 后自动关闭。</p>
            </div>
          ) : (
            <>
              <div className="api-key-table-view">
                <Table<ApiKeyMeta>
                  className="api-key-data-table"
                  size="small"
                  rowKey={(row, index) => rowKeyOf(row, index ?? 0)}
                  columns={columns}
                  dataSource={keys}
                  pagination={keys.length > 10 ? {
                    pageSize: 10,
                    size: 'small',
                    showSizeChanger: true,
                    pageSizeOptions: [10, 20, 50],
                    showTotal: (total, range) => `第 ${range[0]}-${range[1]} 条 / 共 ${total} 条`,
                  } : false}
                  scroll={{ x: 760 }}
                  rowSelection={rowSelection}
                  loading={keysQuery.isFetching && !keys.length}
                  rowClassName={(row, index) => {
                    const name = rowKeyOf(row, index)
                    const classes: string[] = []
                    if (highlightName === name || highlightName === row.name) classes.push('api-key-row-highlight')
                    if (row.enabled === false) classes.push('api-key-row-disabled')
                    if (selectedNames.includes(name)) classes.push('api-key-row-selected')
                    return classes.join(' ')
                  }}
                  locale={{ emptyText: '暂无 API Key' }}
                />
              </div>
              <div className="api-key-mobile-list" aria-label="移动端 API Key 卡片列表">
                {keys.map((row, index) => {
                  const name = String(row.name || '')
                  const keyId = rowKeyOf(row, index)
                  const full = String(row.key || '')
                  const show = !!revealed[name]
                  const display = show && full ? full : maskKey(full, row.hint)
                  const enabled = row.enabled !== false
                  const currentRole = row.role === 'admin' ? 'admin' : 'read'
                  const selected = selectedNames.includes(keyId)
                  const isHighlight = highlightName === keyId || highlightName === name
                  return (
                    <article
                      key={keyId}
                      className={[
                        'api-key-card',
                        enabled ? '' : 'is-disabled',
                        isHighlight ? 'is-highlight' : '',
                        selected ? 'is-selected' : '',
                      ].filter(Boolean).join(' ')}
                    >
                      <div className="api-key-card-main">
                        <div className="api-key-card-title">
                          {bulkEnabled && (
                            <input
                              type="checkbox"
                              className="api-key-card-check"
                              aria-label={`选择 ${name || '未命名'}`}
                              checked={selected}
                              disabled={acting || !name}
                              onChange={(e) => {
                                setSelectedNames(prev => (
                                  e.target.checked
                                    ? Array.from(new Set([...prev, keyId]))
                                    : prev.filter(item => item !== keyId)
                                ))
                              }}
                            />
                          )}
                          <strong title={name || '未命名'}>{name || '未命名'}</strong>
                          <Badge tone={currentRole === 'admin' ? 'warn' : 'info'}>{currentRole}</Badge>
                          {!enabled && <Badge tone="neutral">禁用</Badge>}
                        </div>
                        <div className="api-key-card-secret mono" title={show ? full : '已遮挡'}>
                          {display || '••••••••••••'}
                        </div>
                        <div className="api-key-card-meta">
                          <div className="field console-field">
                            <label>角色</label>
                            <Select
                              size="small"
                              value={currentRole}
                              className="api-key-role-select"
                              disabled={acting || !name}
                              options={[
                                { value: 'read', label: 'read' },
                                { value: 'admin', label: 'admin' },
                              ]}
                              onChange={(v) => {
                                if (v === currentRole) return
                                updateMut.mutate({ name, body: { role: v as 'read' | 'admin' } })
                              }}
                            />
                          </div>
                          <div className="api-key-card-toggle">
                            <Switch
                              size="small"
                              checked={enabled}
                              disabled={acting || !name}
                              onChange={(checked) => updateMut.mutate({ name, body: { enabled: checked } })}
                            />
                            <span>{enabled ? '启用' : '禁用'}</span>
                          </div>
                        </div>
                      </div>
                      <div className="api-key-card-side">
                        <div className="api-key-card-actions">
                          <Button onClick={() => void copyValue(full, 'API Key')} disabled={!full} title="复制">
                            <Copy size={14} />
                          </Button>
                          <Button
                            onClick={() => setRevealed(prev => ({ ...prev, [name]: !prev[name] }))}
                            disabled={!full}
                            title={show ? '遮挡密钥' : '显示密钥'}
                          >
                            {show ? <EyeOff size={14} /> : <Eye size={14} />}
                          </Button>
                          <Dropdown menu={{ items: rowMenu(row) }} trigger={['click']} placement="bottomRight">
                            <Button disabled={acting || !name} title="更多">
                              <MoreHorizontal size={14} />
                            </Button>
                          </Dropdown>
                        </div>
                      </div>
                    </article>
                  )
                })}
              </div>
            </>
          )}
        </section>
      </div>

      <details className="panel api-keys-guide" style={{ marginTop: 4 }}>
        <summary>使用说明 · X-API-Key 与角色</summary>
        <div className="api-keys-guide-body">
          <div>请求头：<code>X-API-Key: epk_…</code>（签发后仅弹窗明文一次，请立即复制）。</div>
          <div><strong>read</strong>：适合对外只读拉取节点/提取代理。</div>
          <div><strong>admin</strong>：完整管理权限，勿下发到不可信环境。</div>
          <div>禁用、轮换、删除均立即生效；批量操作仅在 ≥2 把密钥时出现。</div>
        </div>
      </details>

      <section className={`panel api-keys-playground${playOpen ? '' : ' is-collapsed'}`}>
        <div
          className="panel-header"
          onClick={() => setPlayOpen(v => !v)}
          onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setPlayOpen(v => !v) } }}
          role="button"
          tabIndex={0}
          aria-expanded={playOpen}
        >
          <div>
            <div className="panel-title">
              <Terminal size={16} style={{ verticalAlign: '-2px', marginRight: 6 }} />
              命令试验区
              <Badge tone="neutral">调试</Badge>
            </div>
            <div className="panel-subtitle">选一把 Key 和示例接口，复制 curl 或直接试跑（结果仅本地显示）。</div>
          </div>
          <Button className="api-keys-playground-toggle" onClick={(e) => { e.stopPropagation(); setPlayOpen(v => !v) }}>
            {playOpen ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
            {playOpen ? '收起' : '展开'}
          </Button>
        </div>
        <div className="api-keys-play-body">
        <div className="api-keys-play-grid">
          <div className="field">
            <label>使用 Key</label>
            <Select
              size="large"
              style={{ width: '100%' }}
              value={playKeyName || undefined}
              placeholder={keys.length ? '选择 API Key' : '请先签发 Key'}
              disabled={!keys.length}
              options={keys.map(k => ({
                value: String(k.name || ''),
                label: `${k.name || '未命名'} · ${k.role || 'read'}${k.enabled === false ? ' · 禁用' : ''}`,
              }))}
              onChange={setPlayKeyName}
            />
          </div>
          <div className="field">
            <label>示例接口</label>
            <Select
              size="large"
              style={{ width: '100%' }}
              value={playSampleId}
              options={PLAY_SAMPLES.map(s => ({
                value: s.id,
                label: `${s.label}${s.needAdmin ? ' (admin)' : ''}`,
              }))}
              onChange={setPlaySampleId}
            />
          </div>
          <div className="api-keys-play-meta">
            <Badge tone={playSample.needAdmin ? 'warn' : 'info'}>{playSample.hint}</Badge>
            {playKey?.role && <Badge tone={playKey.role === 'admin' ? 'warn' : 'neutral'}>{playKey.role}</Badge>}
            {playKey?.enabled === false && <Badge tone="neutral">已禁用</Badge>}
          </div>
        </div>
        <div className="api-keys-play-cmd mono" title={playCurl}>{playCurl}</div>
        <div className="toolbar api-keys-play-actions">
          <Button onClick={() => void copyValue(playCurl, '命令')}>
            <Copy size={14} />复制命令
          </Button>
          <Button
            variant="primary"
            disabled={playRunning || !keys.length}
            onClick={() => void runPlayCommand()}
          >
            <Play size={14} />{playRunning ? '试跑中…' : '试跑'}
          </Button>
        </div>
        {playView ? (
          <div className={`api-keys-play-result is-${playView.tone}`}>
            <div className="api-keys-play-result-head">
              <div className="api-keys-play-result-title">
                <Badge tone={playView.tone}>
                  {playView.status === 0 ? 'ERR' : playView.status}
                </Badge>
                <strong>{playView.label}</strong>
                <span className="api-keys-play-result-sample">{playView.sampleLabel}</span>
              </div>
              <div className="api-keys-play-result-metrics">
                <span>{playView.ms} ms</span>
                <span>{formatBytes(playView.bytes)}</span>
                {playView.kind === 'json' && <span>JSON</span>}
              </div>
            </div>
            <div className="api-keys-play-result-toolbar">
              <Button onClick={() => void copyValue(playView.display, '响应')}>
                <Copy size={14} />复制响应
              </Button>
              <Button onClick={() => setPlayResult(null)}>
                <X size={14} />清空
              </Button>
            </div>
            <pre className={`mono api-keys-play-result-body is-${playView.kind}`}>{playView.display}</pre>
          </div>
        ) : (
          <div className="api-keys-play-result is-empty">
            <div className="api-keys-play-empty">
              <Play size={18} />
              <div>
                <strong>尚未试跑</strong>
                <p>选择 Key 与示例后点「试跑」，这里会显示状态码、耗时与格式化响应。</p>
              </div>
            </div>
          </div>
        )}
        </div>
      </section>

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
              strokeColor={secretPaused ? 'var(--muted)' : 'var(--primary)'}
              trailColor="color-mix(in srgb, var(--border) 70%, transparent)"
            />
            <div className="api-key-secret-timer-row">
              <span>{secretPaused ? `已暂停 · 保留 ${secretCountdown}s` : `${secretCountdown}s 后自动关闭`}</span>
              <div className="toolbar">
                <Button size="small" onClick={() => setSecretPaused(p => !p)}>
                  {secretPaused ? <><Play size={13} />继续</> : <><Pause size={13} />暂停</>}
                </Button>
                <Button size="small" onClick={() => setSecretCountdown(SECRET_MODAL_SECONDS)}>
                  <RotateCcw size={13} />延长
                </Button>
              </div>
            </div>
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
    </Page>
  )
}
