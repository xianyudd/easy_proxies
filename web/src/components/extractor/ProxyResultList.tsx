import type { ExtractorEntry } from '../../types/extractor'
import { Button } from '../ui/Button'
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
export function ProxyResultList({ entries }: {entries: ExtractorEntry[]}) {
  const toast = useToast(s => s.show)
  const copy = async (text: string, label = '已复制') => { await copyToClipboard(text, toast, label) }
  if (!entries.length) return <div className="empty-cell">还没有结果。请选择参数后点击生成。</div>
  return <div className="result-list">{entries.slice(0, 50).map((entry, idx) => {
    const main = entryForCopy(entry)
    const curl = curlFor(entry)
    return <div className="result-card" key={idx}>
      <div className="result-card-head"><span className="badge badge-info">#{idx + 1}</span><div className="toolbar"><Button onClick={() => copy(main, '单条已复制')}>复制</Button>{curl && <Button onClick={() => copy(curl, 'curl 已复制')}>curl</Button>}</div></div>
      <div className="result-text">{stringifyEntry(entry)}</div>
    </div>
  })}</div>
}
