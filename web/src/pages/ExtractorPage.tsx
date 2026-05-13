import { useMutation } from '@tanstack/react-query'
import { getExtractor } from '../api/extractor'
import { Button } from '../components/ui/Button'
import { useToast } from '../components/ui/Toast'
import { ExtractorForm } from '../components/extractor/ExtractorForm'
import { QuickExtractBar } from '../components/extractor/QuickExtractBar'
import { ProxyResultList, entriesToText } from '../components/extractor/ProxyResultList'
import { useExtractorStore } from '../store/extractorStore'
import type { ExtractorParams } from '../types/extractor'

export function ExtractorPage() {
  const params = useExtractorStore(s => s.params)
  const entries = useExtractorStore(s => s.entries)
  const meta = useExtractorStore(s => s.meta)
  const warnings = useExtractorStore(s => s.warnings)
  const setParams = useExtractorStore(s => s.setParams)
  const setResult = useExtractorStore(s => s.setResult)
  const clear = useExtractorStore(s => s.clear)
  const toast = useToast(s => s.show)
  const mutation = useMutation({ mutationFn: getExtractor, onSuccess: (data) => { setResult(data); toast('代理已生成', 'ok') }, onError: (e) => toast(e instanceof Error ? e.message : '提取失败', 'error') })
  const run = (patch?: Partial<ExtractorParams>) => {
    const next = { ...params, ...(patch || {}) }
    setParams(next)
    mutation.mutate(next)
  }
  const runAndCopy = async () => {
    const data = await mutation.mutateAsync(params)
    setResult(data)
    const out = entriesToText(data.entries || [])
    if (out) await navigator.clipboard.writeText(out)
    toast('已生成并复制', 'ok')
  }
  const text = entriesToText(entries)
  const copyAll = async () => { if (!text) return toast('请先生成代理', 'error'); await navigator.clipboard.writeText(text); toast('已复制全部', 'ok') }
  const download = () => { if (!text) return toast('请先生成代理', 'error'); const a = document.createElement('a'); a.href = URL.createObjectURL(new Blob([text], {type:'text/plain;charset=utf-8'})); a.download = 'proxy_extractor.txt'; a.click(); URL.revokeObjectURL(a.href) }
  return <div className="page">
    <div className="page-header"><div><h1>代理提取</h1><p>选择条件后生成可直接导入的代理。默认输出 10 条 multi-port，并显示真实凭据。</p></div><div className="toolbar"><Button variant="primary" onClick={() => run()} disabled={mutation.isPending}>生成</Button><Button onClick={copyAll}>复制全部</Button><Button onClick={download}>下载 TXT</Button></div></div>
    <QuickExtractBar run={run} />
    <div className="grid-2">
      <div className="card"><div className="section-title">提取参数</div><ExtractorForm /><div className="split-actions" style={{marginTop: 12}}><Button variant="primary" onClick={() => run()} disabled={mutation.isPending}>{mutation.isPending ? '生成中...' : '生成代理'}</Button><Button onClick={runAndCopy}>生成并复制</Button><Button variant="danger" onClick={clear}>清空</Button></div></div>
      <div className="card"><div className="page-header"><div><div className="section-title">提取结果</div><div className="muted">{meta}</div></div><div className="toolbar"><Button onClick={copyAll}>复制全部</Button><Button onClick={download}>下载</Button></div></div>{warnings.map(w => <div className="hint" key={w}>{w}</div>)}<ProxyResultList entries={entries} /><textarea className="input mono" readOnly value={text} style={{height: 260}} /></div>
    </div>
  </div>
}
