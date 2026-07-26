import { Input, Pagination, Progress, Select } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, Ban, Copy, Gauge, RefreshCw, Search, ShieldCheck, Wand2 } from 'lucide-react'
import { blacklistNode, getNodesPage, probeAllNodes, probeNode, releaseNode } from '../api/nodes'
import { Button } from '../components/ui/Button'
import { QueryErrorBanner } from '../components/ui/QueryErrorBanner'
import { useToast } from '../components/ui/Toast'
import { copyToClipboard } from '../lib/clipboard'
import { regionMeta } from '../components/charts/region'
import type { NodeSnapshot } from '../types/node'
import { Page } from '../components/layout/Page'
import { useAppStore } from '../store/appStore'
import { hashForTab } from '../app/routes'

const SOURCE_LABELS: Record<string, string> = {
  all: '全部',
  inline: '内置',
  nodes_file: '文件',
  subscription: '订阅',
  free_proxy: '免费',
  unknown: '未知',
}

function statusOf(node: NodeSnapshot): { key: 'good' | 'bad' | 'warn' | 'info'; label: string } {
  if (node.blacklisted) return { key: 'bad', label: '拉黑' }
  if (node.available && node.initial_check_done) return { key: 'good', label: '可用' }
  if (node.available) return { key: 'info', label: '待确认' }
  if (node.initial_check_done) return { key: 'bad', label: '不可用' }
  return { key: 'warn', label: '未检测' }
}

/** Short labels for dense proxy list (never full ISO legal names). */
const REGION_SHORT: Record<string, string> = {
  all: '全部',
  jp: '日本',
  hk: '香港',
  tw: '台湾',
  sg: '新加坡',
  us: '美国',
  kr: '韩国',
  gb: '英国',
  uk: '英国',
  de: '德国',
  fr: '法国',
  ca: '加拿大',
  au: '澳洲',
  in: '印度',
  my: '马来',
  th: '泰国',
  vn: '越南',
  ph: '菲律宾',
  id: '印尼',
  cn: '中国',
  other: '其他',
}

function regionLabel(region?: string) {
  const code = String(region || 'other').toLowerCase()
  if (REGION_SHORT[code]) return REGION_SHORT[code]
  const full = regionMeta(code).label || code.toUpperCase()
  // Fallback: first 2–4 chars of long ISO names, prefer code upper
  if (full.length > 6) return code.toUpperCase()
  return full || '—'
}

function latencyMs(value: unknown) {
  const ms = Number(value)
  return Number.isFinite(ms) && ms >= 0 ? Math.round(ms) : null
}

function latencyTone(ms: number | null) {
  if (ms == null) return 'none'
  if (ms < 150) return 'fast'
  if (ms < 350) return 'ok'
  if (ms < 800) return 'mid'
  return 'slow'
}

function safeCount(input: unknown) {
  const value = Number(input)
  return Number.isFinite(value) && value >= 0 ? Math.trunc(value) : 0
}

function safeRows<T>(rows: unknown): T[] {
  return Array.isArray(rows) ? rows : []
}

function safeRecord(value: unknown): Record<string, number> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  return Object.fromEntries(Object.entries(value as Record<string, unknown>).map(([k, raw]) => [k, safeCount(raw)]))
}

export function NodeOverviewPage() {
  const [region, setRegion] = useState('all')
  const [source, setSource] = useState('all')
  const [availability, setAvailability] = useState('available')
  const [latency, setLatency] = useState('all')
  const [sortKey, setSortKey] = useState<'name' | 'latency' | 'latency_desc' | 'region' | 'source'>('latency')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(100)
  const [q, setQ] = useState('')
  const [selectedTag, setSelectedTag] = useState<string | null>(null)
  const [probingTag, setProbingTag] = useState<string | null>(null)
  const [banningTag, setBanningTag] = useState<string | null>(null)
  const [probeAllProgress, setProbeAllProgress] = useState<{ current: number; total: number } | null>(null)
  const toast = useToast(s => s.show)
  const setActiveTab = useAppStore(s => s.setActiveTab)
  const queryClient = useQueryClient()

  const queryParams = {
    page,
    page_size: pageSize,
    region,
    source,
    availability,
    latency,
    sort: sortKey,
    q: q.trim() || undefined,
  }

  const { data, isLoading, isFetching, isError, error, refetch } = useQuery({
    queryKey: ['nodes-page', queryParams],
    queryFn: () => getNodesPage(queryParams),
    refetchInterval: 12000,
  })

  const rows = safeRows<NodeSnapshot>(data?.nodes)

  useEffect(() => {
    if (data?.page && data.page !== page) setPage(data.page)
  }, [data?.page, page])

  const regions = useMemo(() => {
    const stats = safeRecord(data?.region_stats)
    return Object.entries(stats).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  }, [data?.region_stats])

  const sources = useMemo(() => {
    const stats = safeRecord(data?.source_stats)
    return Object.entries(stats).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  }, [data?.source_stats])

  const regionHealthy = safeRecord(data?.region_healthy)
  const availableCount = Object.values(regionHealthy).reduce((s, n) => s + n, 0)
  const totalNodes = safeCount(data?.total_nodes)
  const filtered = safeCount(data?.total_filtered)
  const summaryUnavailable = isError && !data

  const selected = useMemo(() => {
    if (!selectedTag) return null
    return rows.find(n => String(n.tag || '') === selectedTag)
      || null
  }, [rows, selectedTag])

  // Prefer first row as soft focus when nothing selected
  useEffect(() => {
    if (!selectedTag && rows[0]?.tag) {
      // don't auto-select — keep empty until user clicks (clearer UX)
    }
  }, [rows, selectedTag])

  const resetPage = <T,>(setter: (v: T) => void) => (v: T) => {
    setter(v)
    setPage(1)
  }

  const copyNode = (node: NodeSnapshot) => {
    const ms = latencyMs(node.last_latency_ms)
    void copyToClipboard(
      [node.name || node.tag || '-', `tag=${node.tag || '-'}`, `region=${node.region || '-'}`, ms != null ? `${ms}ms` : 'n/a'].join(' · '),
      toast,
      '已复制',
    )
  }

  const probeMut = useMutation({
    mutationFn: (tag: string) => probeNode(tag),
    onMutate: (tag) => setProbingTag(tag),
    onSettled: () => setProbingTag(null),
    onSuccess: (res, tag) => {
      if (res?.error) toast(`${tag}: ${res.error}`, 'error')
      else toast(res?.latency_ms != null ? `${tag} · ${Math.round(Number(res.latency_ms))} ms` : `${tag} 完成`, 'ok')
      void queryClient.invalidateQueries({ queryKey: ['nodes-page'] })
      void queryClient.invalidateQueries({ queryKey: ['nodes-summary'] })
    },
    onError: (e) => toast(e instanceof Error ? e.message : '探测失败', 'error'),
  })

  const banMut = useMutation({
    mutationFn: ({ tag, blacklisted }: { tag: string; blacklisted: boolean }) =>
      blacklisted ? releaseNode(tag) : blacklistNode(tag),
    onMutate: ({ tag }) => setBanningTag(tag),
    onSettled: () => setBanningTag(null),
    onSuccess: (_res, { tag, blacklisted }) => {
      toast(blacklisted ? `${tag} 已解封` : `${tag} 已拉黑 24h`, 'ok')
      void queryClient.invalidateQueries({ queryKey: ['nodes-page'] })
      void queryClient.invalidateQueries({ queryKey: ['nodes-summary'] })
    },
    onError: (e) => toast(e instanceof Error ? e.message : '操作失败', 'error'),
  })

  const probeAllMut = useMutation({
    mutationFn: () => probeAllNodes({
      onStart: total => setProbeAllProgress({ current: 0, total }),
      onProgress: p => setProbeAllProgress({ current: p.current, total: p.total }),
    }),
    onSuccess: summary => {
      toast(`全量测速完成 · 成功 ${summary.success} · 失败 ${summary.failed}`, summary.failed ? 'info' : 'ok')
    },
    onError: (e) => toast(e instanceof Error ? e.message : '全量测速失败', 'error'),
    onSettled: () => {
      setProbeAllProgress(null)
      // An interrupted stream still probed part of the pool — refresh either way.
      void queryClient.invalidateQueries({ queryKey: ['nodes-page'] })
      void queryClient.invalidateQueries({ queryKey: ['nodes-summary'] })
    },
  })

  const goExtract = (node: NodeSnapshot) => {
    const reg = String(node.region || 'all')
    try {
      sessionStorage.setItem('ep-extract-region', reg === 'other' ? 'all' : reg)
    } catch { /* ignore */ }
    setActiveTab('extractor')
    window.history.replaceState(null, '', hashForTab('extractor'))
    toast(`已带到提取页 · ${regionLabel(reg)}`, 'info')
  }

  const regionChips = useMemo(() => {
    const top = regions.slice(0, 8)
    return [{ code: 'all', count: filtered || totalNodes }, ...top.map(([code, count]) => ({ code, count }))]
  }, [regions, filtered, totalNodes])

  return (
    <Page
      className="overview-page overview-proxies"
      title="节点"
      description="选中节点后测速、复制或带去提取。"
      actions={
        <>
          <Button
            onClick={() => probeAllMut.mutate()}
            disabled={probeAllMut.isPending}
          >
            <Gauge size={14} className={probeAllMut.isPending ? 'spin' : undefined} />
            {probeAllMut.isPending
              ? probeAllProgress
                ? `测速中 ${probeAllProgress.current}/${probeAllProgress.total}`
                : '测速中'
              : '全量测速'}
          </Button>
          <Button onClick={() => { void refetch() }} disabled={isFetching}>
            <RefreshCw size={14} className={isFetching ? 'spin' : undefined} />
            刷新
          </Button>
        </>
      }
      stats={[
        { label: '可用', value: summaryUnavailable ? '—' : availableCount },
        { label: '总计', value: summaryUnavailable ? '—' : totalNodes },
      ]}
    >
      {isError && (
        <QueryErrorBanner title="节点加载失败" error={error} onRetry={() => { void refetch() }} />
      )}

      <div className="px-shell">
        {/* Current selection */}
        <div className={`px-current ${selected ? '' : 'is-empty'}`}>
          {selected ? (
            <>
              <div className="px-current-left">
                <span className="px-current-label">Selected</span>
                <div className="px-current-title">
                  <strong>{selected.name || selected.tag || '—'}</strong>
                  <span>{selected.tag || '—'}</span>
                </div>
                <div className="px-current-meta">
                  {(() => {
                    const st = statusOf(selected)
                    const ms = latencyMs(selected.last_latency_ms)
                    return (
                      <>
                        <span className={`px-status is-${st.key}`}>{st.label}</span>
                        <span className="px-cell">{regionLabel(selected.region)}</span>
                        <span className={`px-lat is-${latencyTone(ms)}`}>
                          {ms == null ? '—' : ms}<small>{ms == null ? '' : 'ms'}</small>
                        </span>
                      </>
                    )
                  })()}
                </div>
              </div>
              <div className="px-current-actions">
                <Button
                  size="small"
                  disabled={!selected.tag || probingTag === selected.tag}
                  onClick={() => selected.tag && probeMut.mutate(String(selected.tag))}
                >
                  <Activity size={14} />
                  {probingTag === selected.tag ? '测速中' : '测速'}
                </Button>
                <Button
                  size="small"
                  disabled={!selected.tag || banningTag === selected.tag}
                  onClick={() => selected.tag && banMut.mutate({ tag: String(selected.tag), blacklisted: !!selected.blacklisted })}
                >
                  {selected.blacklisted ? <ShieldCheck size={14} /> : <Ban size={14} />}
                  {banningTag === selected.tag ? '处理中' : selected.blacklisted ? '解封' : '拉黑'}
                </Button>
                <Button size="small" onClick={() => copyNode(selected)}>
                  <Copy size={14} />复制
                </Button>
                <Button size="small" variant="primary" onClick={() => goExtract(selected)}>
                  <Wand2 size={14} />提取
                </Button>
              </div>
            </>
          ) : (
            <div className="px-current-empty">点击行选中节点，在这里测速、拉黑、复制或带去提取。</div>
          )}
        </div>

        {/* Toolbar */}
        <div className="px-toolbar">
          <div className="px-search">
            <Search size={14} />
            <Input
              allowClear
              variant="borderless"
              placeholder="搜索名称或 tag"
              value={q}
              onChange={e => {
                setQ(e.target.value)
                setPage(1)
              }}
              aria-label="搜索节点"
            />
          </div>
          <Select
            size="middle"
            value={source}
            onChange={resetPage(setSource)}
            options={[
              { value: 'all', label: '来源' },
              ...sources.map(([code, count]) => ({
                value: code,
                label: `${SOURCE_LABELS[code] || code} ${count}`,
              })),
            ]}
            popupMatchSelectWidth={false}
          />
          <Select
            size="middle"
            value={availability}
            onChange={resetPage(setAvailability)}
            options={[
              { value: 'available', label: '仅可用' },
              { value: 'all', label: '全部状态' },
              { value: 'unavailable', label: '不可用' },
              { value: 'blacklisted', label: '拉黑' },
              { value: 'unchecked', label: '未检测' },
            ]}
            popupMatchSelectWidth={false}
          />
          <Select
            size="middle"
            value={latency}
            onChange={resetPage(setLatency)}
            options={[
              { value: 'all', label: '延迟' },
              { value: 'fast', label: '<800ms' },
              { value: 'slow', label: '≥800ms' },
              { value: 'tested', label: '已测' },
              { value: 'untested', label: '未测' },
            ]}
            popupMatchSelectWidth={false}
          />
          <Select
            size="middle"
            value={sortKey}
            onChange={resetPage(setSortKey)}
            options={[
              { value: 'latency', label: '延迟 ↑' },
              { value: 'latency_desc', label: '延迟 ↓' },
              { value: 'region', label: '地区' },
              { value: 'source', label: '来源' },
              { value: 'name', label: '名称' },
            ]}
            popupMatchSelectWidth={false}
          />
          <span className="px-toolbar-count">
            {summaryUnavailable ? '—' : `${filtered} 个结果`}
            {isFetching ? ' · 同步中' : ''}
          </span>
        </div>

        {/* Probe-all progress */}
        {probeAllProgress && probeAllProgress.total > 0 && (
          <div className="px-probe-strip" role="status" aria-label="全量测速进度">
            <Progress
              percent={Math.round((probeAllProgress.current / probeAllProgress.total) * 100)}
              size="small"
              showInfo={false}
              status="active"
            />
            <span className="mono">{probeAllProgress.current}/{probeAllProgress.total}</span>
          </div>
        )}

        {/* Region chips */}
        <div className="px-regions" role="tablist" aria-label="按地区筛选">
          {regionChips.map(item => (
            <button
              key={item.code}
              type="button"
              role="tab"
              aria-selected={region === item.code}
              className={`px-chip ${region === item.code ? 'is-on' : ''}`}
              onClick={() => resetPage(setRegion)(item.code)}
            >
              {item.code === 'all' ? '全部' : regionLabel(item.code)}
              <em>{item.count}</em>
            </button>
          ))}
        </div>

        {/* List */}
        <div className="px-table-scroll">
          <div className="px-cols px-table-head" aria-hidden>
            <span className="px-c-radio" />
            <span className="px-c-name">节点</span>
            <span className="px-c-region">地区</span>
            <span className="px-c-source">来源</span>
            <span className="px-c-status">状态</span>
            <span className="px-c-lat">延迟</span>
            <span className="px-c-actions">操作</span>
          </div>
          {!rows.length ? (
            <div className="px-empty">
              <strong>{isLoading ? '加载节点…' : '没有匹配节点'}</strong>
              <span>{isError ? '检查接口或重新登录' : '放宽筛选，或点刷新'}</span>
            </div>
          ) : (
            <div className="px-table-body" role="listbox" aria-label="节点列表">
              {rows.map((node, idx) => {
                const tag = String(node.tag || '')
                const on = !!tag && tag === selectedTag
                const st = statusOf(node)
                const ms = latencyMs(node.last_latency_ms)
                return (
                  <div
                    key={`${tag || 'n'}-${idx}`}
                    role="option"
                    aria-selected={on}
                    className={`px-cols px-row ${on ? 'is-on' : ''}`}
                    tabIndex={0}
                    onClick={() => setSelectedTag(on ? null : tag || null)}
                    onKeyDown={e => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        setSelectedTag(on ? null : tag || null)
                      }
                    }}
                  >
                    <span className="px-c-radio px-radio" aria-hidden><i /></span>
                    <span className="px-c-name px-node">
                      <strong title={String(node.name || node.tag || '')}>{node.name || node.tag || '—'}</strong>
                      <span title={tag}>{tag || '—'}</span>
                    </span>
                    <span className="px-c-region px-cell" title={regionLabel(node.region)}>{regionLabel(node.region)}</span>
                    <span className="px-c-source px-cell">{SOURCE_LABELS[String(node.source || 'unknown')] || '—'}</span>
                    <span className={`px-c-status px-status is-${st.key}`}>{st.label}</span>
                    <span className={`px-c-lat px-lat is-${latencyTone(ms)}`}>
                      {ms == null ? '—' : `${ms}ms`}
                    </span>
                    <span
                      className="px-c-actions px-actions"
                      onClick={e => e.stopPropagation()}
                      onKeyDown={e => e.stopPropagation()}
                    >
                      <Button
                        size="small"
                        disabled={!tag || probingTag === tag}
                        onClick={() => tag && probeMut.mutate(tag)}
                      >
                        {probingTag === tag ? '…' : '测速'}
                      </Button>
                      <Button size="small" onClick={() => copyNode(node)}>复制</Button>
                    </span>
                  </div>
                )
              })}
            </div>
          )}
        </div>

        <div className="px-foot">
          <span className="muted" style={{ fontSize: 12 }}>
            {summaryUnavailable ? '' : `本页 ${rows.length} · 筛选 ${filtered}`}
          </span>
          <Pagination
            size="small"
            current={page}
            pageSize={pageSize}
            total={data?.total_filtered || 0}
            showSizeChanger
            pageSizeOptions={[50, 100, 200]}
            showTotal={(total, range) => `第 ${range[0]}-${range[1]} 条 / 共 ${total} 条`}
            onChange={(p, ps) => {
              setPage(p)
              if (ps !== pageSize) setPageSize(ps)
            }}
          />
        </div>
      </div>
    </Page>
  )
}
