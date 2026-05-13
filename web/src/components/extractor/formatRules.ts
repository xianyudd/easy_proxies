import type { ExtractorFormat, ExtractorMode } from '../../types/extractor'

export const regions = [
  ['all','全部(all)'],['us','美国(us)'],['jp','日本(jp)'],['hk','香港(hk)'],['sg','新加坡(sg)'],['tw','台湾(tw)'],['kr','韩国(kr)'],['in','印度(in)'],['ae','阿联酋(ae)'],['ch','瑞士(ch)'],['au','澳大利亚(au)'],['de','德国(de)'],['gb','英国(gb)'],['ca','加拿大(ca)'],['other','其他未分类(other)'],
] as const
export const modes: Array<[ExtractorMode,string]> = [['multi-port','独立端口节点 multi-port'],['pool','默认池入口 pool'],['geoip','地区池入口 geoip'],['android','Android 无认证端口']]
export const formats: Array<[ExtractorFormat,string]> = [
  ['host_port','host:port'],['adb_command','ADB 命令'],['http_no_auth','http://host:port'],['socks5_url','socks5://username:password@host:port'],['socks5_no_auth','socks5://host:port'],['host_port_user_pass','host:port:username:password'],['user_pass_at_host_port','username:password@host:port'],['http_url','http://username:password@host:port'],['csv','host,port,username,password'],['pipe','host|port|username|password'],['curl_command','curl 命令'],['python_requests_json','Python requests JSON'],['clash_yaml','Clash YAML object'],['host_port_user_pass_refresh_remark','host:port:user:pass[refresh]{remark}'],['user_pass_at_host_port_refresh_remark','user:pass@host:port[refresh]{remark}'],['json','JSON'],
]
const allFormats = formats.map(([v]) => v)
export function allowedFormats(mode: ExtractorMode): ExtractorFormat[] {
  if (mode === 'geoip') return ['http_url', 'json']
  if (mode === 'android') return ['host_port', 'adb_command', 'http_no_auth', 'json']
  if (mode === 'pool') return ['http_url','curl_command','python_requests_json','socks5_url','host_port_user_pass','user_pass_at_host_port','csv','pipe','clash_yaml','json']
  return allFormats
}
export function preferredFormat(mode: ExtractorMode): ExtractorFormat {
  if (mode === 'geoip') return 'http_url'
  if (mode === 'android') return 'adb_command'
  if (mode === 'pool') return 'http_url'
  return 'host_port_user_pass'
}
export function modeHelp(mode: ExtractorMode): string {
  if (mode === 'geoip') return '地区池入口带 /us/ /jp/ 路径，只能用完整 URL 或 JSON。'
  if (mode === 'android') return 'Android 使用无认证本地端口，推荐 ADB 命令或 host:port。'
  if (mode === 'pool') return '默认池入口适合脚本、curl、requests 和长期常驻。'
  return '独立端口节点最适合指纹浏览器、浏览器扩展和账号隔离。'
}
