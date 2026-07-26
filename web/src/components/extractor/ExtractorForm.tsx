import { InputNumber, Select } from 'antd'
import { useExtractorStore } from '../../store/extractorStore'
import type { ExtractorFormat, ExtractorMode, ExtractorRegion } from '../../types/extractor'
import { allowedFormats, formats, modeHelp, modes, preferredFormat, regions } from './formatRules'

export function ExtractorForm({ geoipEnabled = true }: { geoipEnabled?: boolean }) {
  const params = useExtractorStore(s => s.params)
  const setParams = useExtractorStore(s => s.setParams)
  const allowed = allowedFormats(params.mode)

  function setMode(mode: ExtractorMode) {
    if (mode === 'geoip' && !geoipEnabled) return
    setParams({
      mode,
      format: allowedFormats(mode).includes(params.format) ? params.format : preferredFormat(mode),
      region: mode === 'pool' ? 'all' : params.region,
    })
  }

  return (
    <div className="form extractor-form">
      <div className="field">
        <label htmlFor="extractor-mode">模式</label>
        <Select
          id="extractor-mode"
          aria-label="提取模式"
          value={params.mode}
          onChange={value => setMode(value as ExtractorMode)}
          options={modes.map(([v, l]) => ({
            value: v,
            label: v === 'geoip' && !geoipEnabled ? `${l}（未启用）` : l,
            disabled: v === 'geoip' && !geoipEnabled,
          }))}
        />
      </div>
      <div className="field">
        <label htmlFor="extractor-format">格式</label>
        <Select
          id="extractor-format"
          aria-label="输出格式"
          value={params.format}
          onChange={value => setParams({ format: value as ExtractorFormat })}
          options={formats.filter(([v]) => allowed.includes(v)).map(([v, l]) => ({ value: v, label: l }))}
        />
      </div>
      <div className="form-grid-2">
        <div className="field">
          <label htmlFor="extractor-region">区域</label>
          <Select
            id="extractor-region"
            aria-label="代理区域"
            value={params.region}
            onChange={value => setParams({ region: value as ExtractorRegion })}
            showSearch
            optionFilterProp="label"
            options={regions.map(([v, l]) => ({ value: v, label: l }))}
          />
        </div>
        <div className="field">
          <label htmlFor="extractor-count">数量</label>
          <InputNumber
            id="extractor-count"
            aria-label="提取数量"
            min={1}
            max={500}
            value={params.count}
            onChange={value => setParams({ count: Math.max(1, Math.min(500, Number(value) || 1)) })}
            style={{ width: '100%' }}
          />
        </div>
      </div>
      <div className="extractor-mode-help" role="note">
        {params.mode === 'geoip' && !geoipEnabled
          ? 'GeoIP 地区池未启用；请到系统设置启用后再用地区池入口。'
          : modeHelp(params.mode)}
      </div>
    </div>
  )
}
