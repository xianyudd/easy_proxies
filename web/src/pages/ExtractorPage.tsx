import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Switch } from 'antd'
import { ChevronDown, ClipboardCopy, Eraser, FileDown, FileSearch, Play } from 'lucide-react'
import { getExtractor } from '../api/extractor'
import { getSettings } from '../api/settings'
import { Button } from '../components/ui/Button'
import { Badge } from '../components/ui/Badge'
import { useToast } from '../components/ui/Toast'
import { ExtractorForm } from '../components/extractor/ExtractorForm'
import { ProxyResultList, entriesToText } from '../components/extractor/ProxyResultList'
import { formats } from '../components/extractor/formatRules'
import { useExtractorStore } from '../store/extractorStore'
import { copyToClipboard } from '../lib/clipboard'
import { downloadText } from '../lib/download'
import { prefersReducedMotion } from '../lib/motion'
import type { ExtractorEntry, ExtractorParams } from '../types/extractor'
import { Page } from '../components/layout/Page'
import { LoadingPulse } from '../components/motion/LoadingPulse'
import { SuccessPulse } from '../components/motion/SuccessPulse'

function safeArray<T>(value: unknown): T[] {
  return Array.isArray(value) ? value : []
}

export function ExtractorPage() {
  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })
  const params = useExtractorStore(s => s.params)
  const entries = useExtractorStore(s => s.entries)
  const meta = useExtractorStore(s => s.meta)
  const warnings = useExtractorStore(s => s.warnings)
  const setParams = useExtractorStore(s => s.setParams)
  const setResult = useExtractorStore(s => s.setResult)
  const clear = useExtractorStore(s => s.clear)
  const toast = useToast(s => s.show)
  const [copyAlso, setCopyAlso] = useState(true)
  const [rawOpen, setRawOpen] = useState(false)
  const [runId, setRunId] = useState(0)
  const resultRef = useRef<HTMLDivElement>(null)

  const settingsReady = !!settings.data && !settings.isError
  const geoipEnabled = settingsReady
    ? Boolean((settings.data?.geoip as Record<string, unknown> | undefined)?.enabled)
    : false

  // Soft handoff from node overview "用于提取"
  useEffect(() => {
    try {
      const reg = sessionStorage.getItem('ep-extract-region')
      if (reg) {
        setParams({ region: reg as never })
        sessionStorage.removeItem('ep-extract-region')
      }
    } catch { /* ignore */ }
  }, [setParams])

  const mutation = useMutation({
    mutationFn: getExtractor,
    onSuccess: async (data, vars) => {
      setResult(data)
      setRunId(n => n + 1)
      const generated = safeArray<ExtractorEntry>(data.entries)
      const out = entriesToText(generated)
      if (copyAlso && out) {
        await copyToClipboard(out, toast, '已生成并复制')
      } else {
        toast(generated.length ? `已生成 ${generated.length} 条` : '已生成', 'ok')
      }
      // Auto-expand raw only when output is not a simple line list
      if (generated.some(e => typeof e !== 'string')) setRawOpen(true)
      void vars
    },
    onError: (e) => toast(e instanceof Error ? e.message : '提取失败', 'error'),
  })

  const busy = mutation.isPending
  const resultCount = entries.length
  const text = entriesToText(entries)
  const formatLabel = formats.find(([value]) => value === params.format)?.[1] || params.format

  const run = (patch?: Partial<ExtractorParams>) => {
    if (busy) return
    const next = { ...params, ...(patch || {}) }
    setParams(next)
    mutation.mutate(next)
  }

  const copyAll = async () => {
    if (!text) return toast('请先生成代理', 'error')
    await copyToClipboard(text, toast, '已复制全部')
  }

  const download = () => {
    if (!text) return toast('请先生成代理', 'error')
    downloadText('proxy_extractor.txt', text)
  }

  // When results arrive, keep raw collapsed for string lists
  useEffect(() => {
    if (!resultCount) setRawOpen(false)
  }, [resultCount])

  // Single-column flow: scroll to results after each successful run
  useEffect(() => {
    if (runId === 0) return
    resultRef.current?.scrollIntoView({
      behavior: prefersReducedMotion() ? 'auto' : 'smooth',
      block: 'start',
    })
  }, [runId])

  return (
    <Page
      className="extractor-page extractor-page-v2"
      title="代理提取"
      description="定参数，生成，按条或整段复制。"
      stats={[{ label: '结果', value: resultCount }]}
      actions={
        <Badge tone={busy ? 'warn' : resultCount ? 'good' : 'neutral'}>
          {busy ? '生成中' : resultCount ? `${resultCount} 条` : '待命'}
        </Badge>
      }
    >
      <div className="extractor-grid">
        {/* INPUT */}
        <section className="card extractor-card extractor-input-card">
          <div className="panel-header">
            <div>
              <div className="panel-title">参数</div>
              <div className="panel-subtitle">设置模式、格式与数量，点「生成并复制」即可。</div>
            </div>
          </div>

          <div className="extractor-input-body">
            {settings.isError && (
              <div className="hint extractor-inline-hint">设置加载失败，GeoIP 地区池可能不可用；其它模式仍可试。</div>
            )}

            <ExtractorForm geoipEnabled={geoipEnabled} />

            <div className="extractor-toggles" role="group" aria-label="生成选项">
              <div className="extractor-toggle">
                <div className="extractor-toggle-copy">
                  <strong>显示真实凭据</strong>
                  <em>关闭时输出掩码</em>
                </div>
                <Switch
                  size="small"
                  aria-label="显示真实凭据"
                  checked={params.reveal}
                  onChange={v => setParams({ reveal: v })}
                  disabled={busy}
                />
              </div>
              <div className="extractor-toggle">
                <div className="extractor-toggle-copy">
                  <strong>生成后自动复制</strong>
                  <em>结果写入剪贴板</em>
                </div>
                <Switch
                  size="small"
                  aria-label="生成后自动复制"
                  checked={copyAlso}
                  onChange={v => setCopyAlso(v)}
                  disabled={busy}
                />
              </div>
            </div>
          </div>

          <div className="extractor-primary-actions">
            <Button
              className="extractor-run-btn"
              variant="primary"
              loading={busy}
              onClick={() => run()}
            >
              {busy ? null : <Play size={15} />}
              {busy ? '生成中…' : copyAlso ? '生成并复制' : '生成代理'}
            </Button>
            <Button
              className="extractor-clear-btn"
              variant="ghost"
              disabled={busy || (!resultCount && !warnings.length)}
              onClick={() => {
                clear()
                setRawOpen(false)
              }}
              title="清空当前结果"
              aria-label="清空结果"
            >
              <Eraser size={15} />
              清空
            </Button>
          </div>
        </section>

        {/* OUTPUT */}
        <div ref={resultRef} className="extractor-result-anchor">
        <SuccessPulse pulseKey={runId} className="card extractor-card extractor-output-card">
          <LoadingPulse active={busy} label="正在生成代理…">
            <div className="panel-header">
              <div>
                <div className="panel-title">结果</div>
                <div className="panel-subtitle">
                  {busy
                    ? '请求进行中…'
                    : meta || (resultCount ? `${params.mode} · ${formatLabel}` : '生成后显示列表与原始文本')}
                </div>
              </div>
              <div className="toolbar">
                <Button onClick={() => void copyAll()} disabled={!text || busy}>
                  <ClipboardCopy size={15} />复制全部
                </Button>
                <Button onClick={download} disabled={!text || busy}>
                  <FileDown size={15} />TXT
                </Button>
              </div>
            </div>

            {resultCount > 0 ? (
              <div className="extractor-output-meta">
                <span>
                  <em>{resultCount}</em> 条 · {formatLabel}
                </span>
                <span>{params.reveal ? '明文凭据' : '凭据已隐藏'}</span>
              </div>
            ) : null}

            {warnings.map(w => (
              <div className="hint extractor-inline-hint" key={w}>
                {w}
              </div>
            ))}

            <div className="extractor-output-main">
              {entries.length ? (
                <ProxyResultList entries={entries} />
              ) : (
                <div className="extractor-empty" data-busy={busy ? '1' : '0'}>
                  <div className="extractor-empty-icon" aria-hidden>
                    <FileSearch size={22} strokeWidth={1.8} />
                  </div>
                  <div className="extractor-empty-copy">
                    <strong>{busy ? '生成中…' : '还没有结果'}</strong>
                    <span>
                      {busy
                        ? '完成后会显示在这里，并可按条复制。'
                        : '在左侧设置参数后点「生成并复制」。'}
                    </span>
                  </div>
                </div>
              )}
            </div>

            <div className={`extractor-raw ${rawOpen ? 'is-open' : ''}`}>
              <button
                type="button"
                className="extractor-raw-toggle"
                onClick={() => setRawOpen(v => !v)}
                aria-expanded={rawOpen}
              >
                <ChevronDown size={15} className={rawOpen ? 'is-open' : ''} />
                原始输出
                <span className="extractor-raw-hint">{text ? `${text.length} chars` : '空'}</span>
              </button>
              {rawOpen ? (
                <textarea
                  className="input mono result-textarea"
                  readOnly
                  value={text}
                  placeholder="原始文本"
                />
              ) : null}
            </div>
          </LoadingPulse>
        </SuccessPulse>
        </div>
      </div>
    </Page>
  )
}
