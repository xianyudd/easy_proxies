import { useExtractorStore } from '../../store/extractorStore'
import type { ExtractorFormat, ExtractorMode, ExtractorRegion } from '../../types/extractor'
import { allowedFormats, formats, modeHelp, modes, preferredFormat, regions } from './formatRules'

export function ExtractorForm({ geoipEnabled = true }: { geoipEnabled?: boolean }) {
  const params = useExtractorStore(s => s.params)
  const setParams = useExtractorStore(s => s.setParams)
  const allowed = allowedFormats(params.mode)
  function setMode(mode: ExtractorMode) {
    if (mode === 'geoip' && !geoipEnabled) return
    setParams({ mode, format: allowedFormats(mode).includes(params.format) ? params.format : preferredFormat(mode), region: mode === 'pool' ? 'all' : params.region })
  }
  return <div className="form">
    <div className="form-grid-2">
      <div className="field"><label htmlFor="extractor-region">区域</label><select id="extractor-region" className="input" aria-label="代理区域" value={params.region} onChange={e => setParams({ region: e.target.value as ExtractorRegion })}>{regions.map(([v,l]) => <option key={v} value={v}>{l}</option>)}</select></div>
      <div className="field"><label htmlFor="extractor-count">数量</label><input id="extractor-count" className="input" aria-label="提取数量" type="number" min={1} max={500} value={params.count} onChange={e => setParams({ count: Math.max(1, Number(e.target.value) || 1) })} /></div>
    </div>
    <div className="field"><label htmlFor="extractor-mode">模式</label><select id="extractor-mode" className="input" aria-label="提取模式" value={params.mode} onChange={e => setMode(e.target.value as ExtractorMode)}>{modes.map(([v,l]) => <option key={v} value={v} disabled={v === 'geoip' && !geoipEnabled}>{v === 'geoip' && !geoipEnabled ? `${l}（未启用）` : l}</option>)}</select></div>
    <div className="field"><label htmlFor="extractor-format">格式</label><select id="extractor-format" className="input" aria-label="输出格式" value={params.format} onChange={e => setParams({ format: e.target.value as ExtractorFormat })}>{formats.filter(([v]) => allowed.includes(v)).map(([v,l]) => <option key={v} value={v}>{l}</option>)}</select></div>
    <label className="split-actions"><input type="checkbox" aria-label="显示真实凭据" checked={params.reveal} onChange={e => setParams({ reveal: e.target.checked })} /> 显示真实凭据</label>
    <div className="hint">{params.mode === 'geoip' && !geoipEnabled ? 'GeoIP 地区池当前未启用；请到系统设置启用 GeoIP 后再使用地区池入口。' : modeHelp(params.mode)}</div>
  </div>
}
