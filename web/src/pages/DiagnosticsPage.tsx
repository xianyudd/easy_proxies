import { useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Activity, AlertCircle, CheckCircle2, Download, RefreshCw, Trash2 } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { getDebug, getLogs } from '../api/logs'
import { Button } from '../components/ui/Button'
import { Badge } from '../components/ui/Badge'
import { QueryErrorBanner } from '../components/ui/QueryErrorBanner'
import { useToast } from '../components/ui/Toast'

const LOG_RENDER_LIMIT = 1200
const LOG_LEVELS = [
  { value: 'all', label: '全部' },
  { value: 'error', label: 'ERROR' },
  { value: 'warn', label: 'WARN' },
  { value: 'info', label: 'INFO' },
  { value: 'debug', label: 'DEBUG' },
] as const

type LogLevel = 'error' | 'warn' | 'info' | 'debug' | 'neutral'
type LogFilter = typeof LOG_LEVELS[number]['value']
type LogRow = { id: number; text: string; level: LogLevel }
type FailureBucket = { key: string; label: string; value: number }

function percentValue(value: unknown) {
  const num = Number(value)
  return Number.isFinite(num) ? `${num.toFixed(1)}%` : '-'
}

function countValue(value: unknown) {
  const num = Number(value)
  return Number.isFinite(num) ? String(num) : '-'
}

function sumNumbers(rows: Record<string, unknown>[], key: string) {
  return rows.reduce((total, row) => total + (Number(row[key]) || 0), 0)
}

function avgLatency(rows: Record<string, unknown>[]) {
  const values = rows.map(row => Number(row.last_latency_ms) || 0).filter(value => value > 0)
  if (!values.length) return '-'
  return `${Math.round(values.reduce((total, value) => total + value, 0) / values.length)} ms`
}

function detectLogLevel(line: string): LogLevel {
  const explicit = line.match(/\b(error|err|fatal|panic|warn|warning|debug|trace|info)\b[:\]]?/i)?.[1]?.toLowerCase()
  if (explicit) {
    if (explicit === 'error' || explicit === 'err' || explicit === 'fatal' || explicit === 'panic') return 'error'
    if (explicit === 'warn' || explicit === 'warning') return 'warn'
    if (explicit === 'debug' || explicit === 'trace') return 'debug'
    if (explicit === 'info') return 'info'
  }
  if (/\b(fatal|panic)\b[:\]]?/i.test(line)) return 'error'
  if (/\b(error)\b[:\]]?/i.test(line)) return 'error'
  if (/\b(failed|failure|blacklisted|timeout|deadline exceeded|EOF|tls:|reality verification failed)\b/i.test(line)) return 'warn'
  if (/\b(success|ready|started|listening)\b[:\]]?/i.test(line)) return 'info'
  return 'neutral'
}

function classifyFailureReason(text: string) {
  const line = text.toLowerCase()
  if (/blacklisted/.test(line)) return 'blacklisted'
  if (/reality verification failed/.test(line)) return 'reality'
  if (/timeout|no recent network activity|deadline exceeded|i\/o timeout/.test(line)) return 'timeout'
  if (/\beof\b/.test(line)) return 'eof'
  if (/tls:|handshake failure/.test(line)) return 'tls'
  if (/unexpected http response status: (429|503)/.test(line)) return 'http-status'
  return ''
}

function isMetadataLikeNodeTag(value: unknown) {
  const text = String(value || '').trim().toLowerCase()
  return /^\d+(?:-\d+)?-gb$/.test(text) || /^\d{4}-\d{2}-\d{2}$/.test(text) || /^\d{1,3}$/.test(text)
}

function buildFailureBuckets(logRows: LogRow[], debugNodes: Record<string, unknown>[]): FailureBucket[] {
  const counts = new Map<string, number>()
  for (const row of logRows) {
    const reason = classifyFailureReason(row.text)
    if (reason) counts.set(reason, (counts.get(reason) || 0) + 1)
  }
  const metadataNodes = debugNodes.filter(node => isMetadataLikeNodeTag(node.tag || node.name)).length
  if (metadataNodes) counts.set('metadata', metadataNodes)
  return [
    { key: 'timeout', label: '超时 / 无活动', value: counts.get('timeout') || 0 },
    { key: 'reality', label: 'Reality 校验', value: counts.get('reality') || 0 },
    { key: 'eof', label: 'EOF 断开', value: counts.get('eof') || 0 },
    { key: 'tls', label: 'TLS 握手', value: counts.get('tls') || 0 },
    { key: 'http-status', label: 'HTTP 429/503', value: counts.get('http-status') || 0 },
    { key: 'blacklisted', label: '拉黑事件', value: counts.get('blacklisted') || 0 },
    { key: 'metadata', label: '疑似元数据节点', value: counts.get('metadata') || 0 },
  ].filter(item => item.value > 0)
}

function includesKeyword(line: string, keyword: string, caseSensitive: boolean) {
  if (!keyword) return true
  return caseSensitive ? line.includes(keyword) : line.toLowerCase().includes(keyword.toLowerCase())
}

function highlightKeyword(line: string, keyword: string, caseSensitive: boolean): ReactNode {
  if (!keyword) return line
  const source = caseSensitive ? line : line.toLowerCase()
  const needle = caseSensitive ? keyword : keyword.toLowerCase()
  const parts: ReactNode[] = []
  let cursor = 0
  let match = source.indexOf(needle)
  while (match !== -1) {
    if (match > cursor) parts.push(line.slice(cursor, match))
    const end = match + keyword.length
    parts.push(<mark key={`${match}-${end}`} className="terminal-highlight">{line.slice(match, end)}</mark>)
    cursor = end
    match = source.indexOf(needle, cursor)
  }
  if (cursor < line.length) parts.push(line.slice(cursor))
  return parts
}

export function DiagnosticsPage() {
  const [auto, setAuto] = useState(true)
  const [logs, setLogs] = useState('')
  const [manualLogRefreshPending, setManualLogRefreshPending] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [levelFilter, setLevelFilter] = useState<LogFilter>('all')
  const [caseSensitive, setCaseSensitive] = useState(false)
  const logRef = useRef<HTMLDivElement | null>(null)
  const toast = useToast(s=>s.show)
  const debug = useQuery({ queryKey:['debug'], queryFn:getDebug, refetchInterval:15000 })
  const logQuery = useQuery({ queryKey:['logs'], queryFn:getLogs, refetchInterval:auto?2000:false })
  useEffect(()=>{ if(logQuery.data) setLogs(String(logQuery.data.logs || '')) }, [logQuery.data])
  const logRows = useMemo<LogRow[]>(() => logs ? logs.split('\n').map((text, id) => ({ id, text, level: detectLogLevel(text) })) : [], [logs])
  const normalizedKeyword = keyword.trim()
  const filteredLogRows = useMemo(() => logRows.filter(row => (levelFilter === 'all' || row.level === levelFilter) && includesKeyword(row.text, normalizedKeyword, caseSensitive)), [caseSensitive, levelFilter, logRows, normalizedKeyword])
  const visibleLogRows = useMemo(() => filteredLogRows.slice(-LOG_RENDER_LIMIT), [filteredLogRows])
  const hiddenLogRows = Math.max(0, filteredLogRows.length - visibleLogRows.length)
  useEffect(() => {
    if (!auto) return
    const el = logRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [auto, visibleLogRows])
  const errorLines = useMemo(() => logRows.filter(row => row.level === 'error').length, [logRows])
  const warnLines = useMemo(() => logRows.filter(row => row.level === 'warn').length, [logRows])
  const debugData = useMemo(() => debug.data || {}, [debug.data])
  const debugNodes = useMemo<Record<string, unknown>[]>(() => Array.isArray(debugData.nodes) ? debugData.nodes as Record<string, unknown>[] : [], [debugData])
  const failureBuckets = useMemo(() => buildFailureBuckets(logRows, debugNodes), [debugNodes, logRows])
  const diagnosticSummary = useMemo(() => {
    const activeConnections = sumNumbers(debugNodes, 'active_connections')
    const failedNodes = debugNodes.filter(node => Number(node.failure_count) > 0).length
    const blacklistedNodes = debugNodes.filter(node => node.blacklisted === true).length
    const recentErrors = debugNodes.filter(node => String(node.last_error || '').trim()).length
    return [
      { label: '节点明细', value: `${debugNodes.length} 项` },
      { label: '活跃连接', value: String(activeConnections) },
      { label: '平均延迟', value: avgLatency(debugNodes) },
      { label: '失败节点', value: String(failedNodes) },
      { label: '拉黑节点', value: String(blacklistedNodes) },
      { label: '最近错误', value: String(recentErrors) },
      { label: '累计调用', value: countValue(debugData.total_calls) },
      { label: '成功调用', value: countValue(debugData.total_success) },
      { label: '成功率', value: percentValue(debugData.success_rate) },
    ].filter(item => item.value !== '-')
  }, [debugData, debugNodes])
  const issueCount = errorLines + warnLines + (debug.isError ? 1 : 0)
  const download = () => { const a=document.createElement('a'); a.href=URL.createObjectURL(new Blob([logs],{type:'text/plain'})); a.download='easy_proxies.log'; a.click(); URL.revokeObjectURL(a.href) }
  const clearFilters = () => { setKeyword(''); setLevelFilter('all'); setCaseSensitive(false) }
  const clearLogs = () => { setAuto(false); setLogs('') }
  const refreshLogs = async () => {
    if (manualLogRefreshPending || logQuery.isFetching) return
    setManualLogRefreshPending(true)
    try {
      const result = await logQuery.refetch()
      if (result.error) throw result.error
      setLogs(String(result.data?.logs || ''))
      toast('日志已刷新','ok')
    } catch (error) {
      toast(error instanceof Error ? `日志刷新失败：${error.message}` : '日志刷新失败','error')
    } finally {
      setManualLogRefreshPending(false)
    }
  }
  return <div className="page diagnostics-page diagnostics-workbench-page">
    <div className="page-header diagnostics-hero diagnostics-workbench-hero"><div><h1>日志诊断</h1><p>实时日志保持终端控制台体验，右侧只保留关键运行态摘要，避免原始数据和重复指标干扰排查。</p></div><div className="toolbar"><Badge tone={auto ? 'good' : 'neutral'}>{auto ? '自动刷新' : '已暂停'}</Badge><Button onClick={()=>setAuto(!auto)}>{auto?'暂停刷新':'自动刷新'}</Button></div></div>
    {debug.isError && <QueryErrorBanner title="运行态摘要加载失败" error={debug.error} onRetry={() => { void debug.refetch() }} />}
    {logQuery.isError && <QueryErrorBanner title="日志加载失败" error={logQuery.error} onRetry={() => { void logQuery.refetch() }} />}
    <div className="diagnostics-metrics summary-grid diagnostics-workbench-metrics">
      <div className="metric"><div className="label">日志行数</div><div className="value">{logRows.length}</div></div>
      <div className="metric"><div className="label">错误线索</div><div className="value error">{errorLines}</div></div>
      <div className="metric"><div className="label">警告线索</div><div className="value">{warnLines}</div></div>
      <div className="metric"><div className="label">API 状态</div><div className={`value ${debug.isError || logQuery.isError ? 'error' : 'success'}`}>{debug.isFetching || logQuery.isFetching?'SYNC':debug.isError || logQuery.isError?'ERROR':'READY'}</div></div>
    </div>
    <div className="diagnostics-layout refined-diagnostics-layout diagnostics-workbench">
      <div className="log-console refined-log-console terminal-console-panel diagnostics-log-panel">
        <div className="log-toolbar refined-log-toolbar terminal-toolbar">
          <div><div className="panel-title">实时日志</div><div className="panel-subtitle">{auto ? '每 2 秒自动刷新并滚动到底部' : '自动刷新已暂停'} · 匹配 {filteredLogRows.length}/{logRows.length} 行{hiddenLogRows ? ` · 仅渲染最近 ${LOG_RENDER_LIMIT} 行` : ''}</div></div>
          <div className="toolbar diagnostics-actions"><Button onClick={() => { void refreshLogs() }} disabled={manualLogRefreshPending || logQuery.isFetching}><RefreshCw size={15} />{manualLogRefreshPending || logQuery.isFetching ? '刷新中...' : '刷新'}</Button><Button onClick={clearLogs} disabled={manualLogRefreshPending}><Trash2 size={15} />清屏</Button><Button onClick={download} disabled={!logs}><Download size={15} />下载</Button></div>
          <div className="diagnostics-log-filters">
            <input className="diagnostics-log-search" value={keyword} onChange={event=>setKeyword(event.target.value)} placeholder="筛选关键词" aria-label="筛选日志关键词" />
            <select className="diagnostics-log-select" value={levelFilter} onChange={event=>setLevelFilter(event.target.value as LogFilter)} aria-label="筛选日志级别">{LOG_LEVELS.map(level => <option key={level.value} value={level.value}>{level.label}</option>)}</select>
            <label className="diagnostics-case-toggle"><input type="checkbox" checked={caseSensitive} onChange={event=>setCaseSensitive(event.target.checked)} />区分大小写</label>
            <Button onClick={clearFilters}>清除筛选</Button>
          </div>
        </div>
        <div className="terminal-frame diagnostics-terminal-frame">
          <div className="terminal-chrome"><span></span><span></span><span></span><strong>easy_proxies.log</strong></div>
          <div ref={logRef} className="terminal-logbox" role="log" aria-label="实时日志">
            {visibleLogRows.length ? visibleLogRows.map(row => <div key={row.id} className={`terminal-logline terminal-logline-${row.level}`}>{highlightKeyword(row.text || ' ', normalizedKeyword, caseSensitive)}</div>) : <div className="terminal-log-empty">{logs ? '当前筛选没有匹配日志' : '日志已清空；点击刷新可重新加载最新日志'}</div>}
          </div>
        </div>
      </div>
      <div className="dashboard-stack diagnostics-side diagnostics-control-panel">
        <div className="card diagnostics-json-card diagnostics-summary-card">
          <div className="section-title">运行态摘要</div>
          <div className="panel-subtitle">聚合后端调试接口的核心健康数据，不展示原始 JSON 和重复指标。</div>
          <div className="diagnostic-summary-list">
            {diagnosticSummary.length ? diagnosticSummary.map(item => <div key={item.label}><span>{item.label}</span><strong title={item.value}>{item.value}</strong></div>) : <div><span>状态</span><strong>暂无诊断数据</strong></div>}
          </div>
        </div>
        <div className="card diagnostics-json-card diagnostics-summary-card diagnostics-failure-card">
          <div className="section-title">失败原因聚合</div>
          <div className="panel-subtitle">按日志关键词和节点标签聚合，优先定位协议、网络和订阅污染问题。</div>
          <div className="diagnostic-summary-list">
            {failureBuckets.length ? failureBuckets.map(item => <div key={item.key}><span>{item.label}</span><strong>{item.value}</strong></div>) : <div><span>状态</span><strong>暂无失败分类</strong></div>}
          </div>
        </div>
        <div className="diagnostics-advice-card">
          {issueCount ? <><AlertCircle size={15} /><span>发现异常线索，建议优先查看日志中的 error / warning 行。</span></> : <><CheckCircle2 size={15} /><span>当前未发现明显异常线索，保持自动刷新即可。</span></>}
        </div>
        <div className="diagnostics-refresh-card"><Activity size={15} /><span>日志 2s 自动刷新，API 15s 同步状态。</span></div>
      </div>
    </div>
  </div>
}
