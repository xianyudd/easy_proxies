import { useState } from 'react'
import { Check, Copy, Terminal } from 'lucide-react'
import type { ExtractorEntry } from '../../types/extractor'
import { useToast } from '../ui/Toast'
import { copyToClipboard } from '../../lib/clipboard'

function stringifyEntry(entry: ExtractorEntry) {
  if (typeof entry === 'string') return entry
  return JSON.stringify(entry, null, 2)
}
function entryForCopy(entry: ExtractorEntry) {
  if (typeof entry === 'string') return entry
  if (typeof entry.url === 'string') return entry.url
  if (typeof entry.http === 'string') return entry.http
  if (typeof entry.https === 'string') return entry.https
  return JSON.stringify(entry, null, 2)
}
function curlFor(entry: ExtractorEntry) {
  const text = entryForCopy(entry)
  if (text.startsWith('curl ')) return text
  if (text.startsWith('http://') || text.startsWith('https://') || text.startsWith('socks5://')) return `curl -x ${text} http://cp.cloudflare.com/generate_204`
  const parts = text.split(':')
  if (parts.length === 4 && /^\d+$/.test(parts[1])) return `curl -x http://${parts[2]}:${parts[3]}@${parts[0]}:${parts[1]} http://cp.cloudflare.com/generate_204`
  if (parts.length === 2 && /^\d+$/.test(parts[1])) return `curl -x http://${text} http://cp.cloudflare.com/generate_204`
  return ''
}
export function entriesToText(entries: ExtractorEntry[]) {
  if (entries.some(e => typeof e !== 'string')) return JSON.stringify(entries, null, 2)
  return entries.map(String).join('\n')
}
const RESULT_PREVIEW_LIMIT = 50

/**
 * Dense one-line-per-proxy list: the proxy string is the row, the index is a
 * quiet gutter number, and actions stay out of the way until hover. Clicking
 * anywhere on a row copies it — the common case shouldn't need aiming.
 */
export function ProxyResultList({ entries }: {entries: ExtractorEntry[]}) {
  const toast = useToast(s => s.show)
  const [copiedIdx, setCopiedIdx] = useState<number | null>(null)

  const copy = async (text: string, label: string, idx?: number) => {
    await copyToClipboard(text, toast, label)
    if (idx == null) return
    setCopiedIdx(idx)
    window.setTimeout(() => setCopiedIdx(cur => (cur === idx ? null : cur)), 1200)
  }

  if (!entries.length) return null

  const hidden = entries.length - RESULT_PREVIEW_LIMIT
  const multiline = entries.some(e => typeof e !== 'string')

  return (
    <div className={`px-results${multiline ? ' is-block' : ''}`}>
      <ol className="px-result-list">
        {entries.slice(0, RESULT_PREVIEW_LIMIT).map((entry, idx) => {
          const main = entryForCopy(entry)
          const curl = curlFor(entry)
          const copied = copiedIdx === idx
          return (
            <li key={idx} className={`px-result-row${copied ? ' is-copied' : ''}`}>
              <button
                type="button"
                className="px-result-main"
                onClick={() => { void copy(main, '已复制这条', idx) }}
                title="点击复制这条"
              >
                <span className="px-result-idx" aria-hidden>{idx + 1}</span>
                <span className="px-result-text">{stringifyEntry(entry)}</span>
              </button>
              <span className="px-result-actions">
                <button
                  type="button"
                  className="px-result-act"
                  onClick={() => { void copy(main, '已复制这条', idx) }}
                  title="复制"
                  aria-label={`复制第 ${idx + 1} 条`}
                >
                  {copied ? <Check size={14} /> : <Copy size={14} />}
                </button>
                {curl && (
                  <button
                    type="button"
                    className="px-result-act"
                    onClick={() => { void copy(curl, 'curl 命令已复制') }}
                    title="复制 curl 测试命令"
                    aria-label={`复制第 ${idx + 1} 条的 curl 命令`}
                  >
                    <Terminal size={14} />
                  </button>
                )}
              </span>
            </li>
          )
        })}
      </ol>
      {hidden > 0 && (
        <p className="px-result-more" role="note">
          仅展示前 {RESULT_PREVIEW_LIMIT} 条，另有 {hidden} 条 · 用「复制全部」或「TXT」获取完整 {entries.length} 条。
        </p>
      )}
    </div>
  )
}
