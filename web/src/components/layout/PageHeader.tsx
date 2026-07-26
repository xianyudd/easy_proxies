import type { ReactNode } from 'react'

export type PageHeaderStat = {
  label: string
  value: ReactNode
}

type PageHeaderProps = {
  eyebrow?: string
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
  stats?: PageHeaderStat[]
  className?: string
}

/**
 * Compact toolbar-style page header:
 * one row — title (+one-line description) | inline text metrics | actions.
 * No stat cards, no eyebrow: the top nav already gives section context.
 */
export function PageHeader({
  title,
  description,
  actions,
  stats,
  className = '',
}: PageHeaderProps) {
  return (
    <div className={`page-header pg-head ${className}`.trim()}>
      <div className="pg-head-main">
        <h1>{title}</h1>
        {description ? <p className="pg-desc">{description}</p> : null}
      </div>
      {stats && stats.length > 0 ? (
        <div className="pg-stats" aria-label="页面统计">
          {stats.map((s) => (
            <span key={String(s.label)} className="pg-stat">
              <em>{s.label}</em>
              <strong>{s.value}</strong>
            </span>
          ))}
        </div>
      ) : null}
      {actions ? <div className="toolbar pg-actions">{actions}</div> : null}
    </div>
  )
}
