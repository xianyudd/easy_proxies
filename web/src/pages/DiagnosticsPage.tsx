import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getDebug, getLogs } from '../api/logs'
import { Button } from '../components/ui/Button'
import { useToast } from '../components/ui/Toast'

export function DiagnosticsPage() {
  const [auto, setAuto] = useState(true)
  const [logs, setLogs] = useState('')
  const toast = useToast(s=>s.show)
  const debug = useQuery({ queryKey:['debug'], queryFn:getDebug, refetchInterval:15000 })
  const logQuery = useQuery({ queryKey:['logs'], queryFn:getLogs, refetchInterval:auto?2000:false })
  useEffect(()=>{ if(logQuery.data?.logs) setLogs(logQuery.data.logs) }, [logQuery.data])
  const download = () => { const a=document.createElement('a'); a.href=URL.createObjectURL(new Blob([logs],{type:'text/plain'})); a.download='easy_proxies.log'; a.click(); URL.revokeObjectURL(a.href) }
  return <div className="page"><div className="page-header"><div><h1>日志诊断</h1><p>查看实时日志、最近错误和 API 状态。</p></div><div className="toolbar"><Button onClick={()=>setAuto(!auto)}>{auto?'暂停刷新':'自动刷新'}</Button><Button onClick={()=>setLogs('')}>清屏</Button><Button onClick={download}>下载日志</Button></div></div><div className="grid-2"><div className="card"><div className="section-title">API / 诊断状态</div><pre className="hint" style={{whiteSpace:'pre-wrap'}}>{JSON.stringify(debug.data || {}, null, 2)}</pre></div><div className="card"><div className="section-title">操作</div><Button onClick={()=>logQuery.refetch().then(()=>toast('日志已刷新','ok'))}>刷新日志</Button></div></div><textarea className="input logbox" readOnly value={logs} /></div>
}
