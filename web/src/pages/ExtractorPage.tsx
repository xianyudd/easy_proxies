import { useMutation, useQuery } from '@tanstack/react-query'
import { getExtractor } from '../api/extractor'
import { getSettings } from '../api/settings'
import { Button } from '../components/ui/Button'
import { useToast } from '../components/ui/Toast'
import { ExtractorForm } from '../components/extractor/ExtractorForm'
import { QuickExtractBar } from '../components/extractor/QuickExtractBar'
import { ProxyResultList, entriesToText } from '../components/extractor/ProxyResultList'
import { useExtractorStore } from '../store/extractorStore'
import { copyToClipboard } from '../lib/clipboard'
import type { ExtractorEntry, ExtractorParams } from '../types/extractor'

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
  const settingsReady = !!settings.data && !settings.isError
  const geoipEnabled = settingsReady ? Boolean((settings.data?.geoip as Record<string, unknown> | undefined)?.enabled) : false
  const mutation = useMutation({ mutationFn: getExtractor, onSuccess: (data) => { setResult(data); toast('代理已生成', 'ok') }, onError: (e) => toast(e instanceof Error ? e.message : '提取失败', 'error') })
  const run = (patch?: Partial<ExtractorParams>) => {
    if (mutation.isPending) return
    const next = { ...params, ...(patch || {}) }
    setParams(next)
    mutation.mutate(next)
  }
  const runAndCopy = async () => {
    if (mutation.isPending) return
    try {
      const data = await mutation.mutateAsync(params)
      setResult(data)
      const generatedEntries = safeArray<ExtractorEntry>(data.entries)
      const out = entriesToText(generatedEntries)
      if (out) await copyToClipboard(out, toast, '已生成并复制')
      else toast('已生成，但没有可复制的内容', 'info')
    } catch (e) {
      toast(e instanceof Error ? `生成并复制失败：${e.message}` : '生成并复制失败', 'error')
    }
  }
  const text = entriesToText(entries)
  const copyAll = async () => { if (!text) return toast('请先生成代理', 'error'); await copyToClipboard(text, toast, '已复制全部') }
  const download = () => {
    if (!text) return toast('请先生成代理', 'error')
    const a = document.createElement('a')
    const url = URL.createObjectURL(new Blob([text], {type:'text/plain;charset=utf-8'}))
    a.href = url
    a.download = 'proxy_extractor.txt'
    a.click()
    window.setTimeout(() => URL.revokeObjectURL(url), 0)
  }
  return <div className="page">
    <div className="page-header"><div><h1>代理提取</h1><p>先选择模式和区域，再生成可复制、可下载的代理结果。常用预设收纳在参数区，减少页面跳动。</p></div></div>
    <div className="workspace-grid">
      <div className="dashboard-stack">
        <div className="card control-panel">
          <div className="panel-header"><div><div className="panel-title">提取参数</div><div className="panel-subtitle">配置一次后可直接生成或生成并复制。</div></div></div>
          {settings.isError && <div className="hint">设置加载失败，GeoIP 地区池入口已暂时禁用；普通多端口/默认池提取仍可使用。</div>}
          <ExtractorForm geoipEnabled={geoipEnabled} />
          <div className="control-actions" style={{marginTop: 14}}>
            <Button className="primary-wide" variant="primary" onClick={() => run()} disabled={mutation.isPending}>{mutation.isPending ? '生成中...' : '生成代理'}</Button>
            <div className="action-row"><Button onClick={runAndCopy} disabled={mutation.isPending}>{mutation.isPending ? '生成中...' : '生成并复制'}</Button><Button variant="danger" onClick={clear} disabled={mutation.isPending}>清空结果</Button></div>
          </div>
        </div>
        <div className="card">
          <div className="panel-header"><div><div className="panel-title">快速预设</div><div className="panel-subtitle">适合批量导入、地区池和 Android 调试。</div></div></div>
          <QuickExtractBar run={run} geoipEnabled={geoipEnabled} disabled={mutation.isPending} />
        </div>
      </div>
      <div className="card result-panel">
        <div className="panel-header">
          <div><div className="panel-title">提取结果</div><div className="panel-subtitle">{meta || '尚未生成代理'}</div></div>
          <div className="toolbar"><Button onClick={copyAll}>复制全部</Button><Button onClick={download}>下载 TXT</Button></div>
        </div>
        <div>{warnings.map(w => <div className="hint" key={w}>{w}</div>)}{entries.length ? <ProxyResultList entries={entries} /> : <div className="empty-state"><div><strong>等待生成代理</strong><span>左侧选择区域、模式和格式，点击生成后这里会展示结构化结果。</span></div></div>}</div>
        <textarea className="input mono result-textarea" readOnly value={text} placeholder="原始文本输出会显示在这里" />
      </div>
    </div>
  </div>
}
