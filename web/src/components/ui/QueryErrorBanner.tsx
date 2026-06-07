import { AlertCircle, RotateCcw } from 'lucide-react'
import { Button } from './Button'

type Props = {
  title: string
  error?: unknown
  onRetry?: () => void
  details?: string
}

function errorText(error: unknown) {
  if (error instanceof Error && error.message.trim()) return error.message
  if (typeof error === 'string' && error.trim()) return error
  return '接口请求失败，请稍后重试。'
}

export function QueryErrorBanner({ title, error, onRetry, details }: Props) {
  return <div className="query-error-banner" role="alert">
    <AlertCircle size={18} />
    <div className="query-error-copy">
      <strong>{title}</strong>
      <span>{details || errorText(error)}</span>
    </div>
    {onRetry ? <Button onClick={onRetry}><RotateCcw size={15} />重试</Button> : null}
  </div>
}
