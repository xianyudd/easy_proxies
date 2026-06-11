import { useExtractorStore } from '../../store/extractorStore'
import type { ExtractorParams } from '../../types/extractor'

export function QuickExtractBar({ run, geoipEnabled = true, disabled = false }: {run: (patch: Partial<ExtractorParams>) => void; geoipEnabled?: boolean; disabled?: boolean}) {
  const setParams = useExtractorStore(s => s.setParams)
  const quick = (patch: Partial<ExtractorParams>) => { if (disabled) return; setParams(patch); run(patch) }
  return <div className="quick-grid">
    <button className="quick" disabled={disabled} onClick={() => quick({ mode:'multi-port', region:'all', format:'host_port_user_pass', count:10, reveal:true })}><strong>随机 10 条</strong><span>multi-port 批量导入</span></button>
    <button className="quick" disabled={disabled} onClick={() => quick({ mode:'multi-port', region:'jp', format:'host_port_user_pass', count:5, reveal:true })}><strong>日本 5 条</strong><span>JP 独立端口</span></button>
    <button className="quick" disabled={disabled || !geoipEnabled} title={geoipEnabled ? 'GeoIP 用户名选区 URL' : 'GeoIP 地区池未启用'} onClick={() => !disabled && geoipEnabled && quick({ mode:'geoip', region:'us', format:'http_url', count:1, reveal:true })}><strong>美国地区池</strong><span>{geoipEnabled ? 'GeoIP 用户名选区 URL' : 'GeoIP 未启用'}</span></button>
    <button className="quick" disabled={disabled} onClick={() => quick({ mode:'android', region:'jp', format:'adb_command', count:1, reveal:true })}><strong>Android JP</strong><span>ADB 命令</span></button>
  </div>
}
