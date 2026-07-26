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

/** Shared page title block — keeps spacing/type consistent across screens (design preview). */
export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
  stats,
  className = '',
}: PageHeaderProps) {
  return (
    <div className={`page-header ep-page-header ${className}`.trim()}>
      <div className="ep-page-header-main">
        {eyebrow ? <div className="eyebrow">{eyebrow}</div> : null}
        <div className="ep-page-header-title-row">
          <h1>{title}</h1>
          {actions ? <div className="toolbar ep-page-header-actions">{actions}</div> : null}
        </div>
        {description ? <p className="ep-page-header-desc">{description}</p> : null}
      </div>
      {stats && stats.length > 0 ? (
        <div className="ep-page-header-stats" aria-label="页面统计">
          {stats.map((s) => (
            <div key={String(s.label)} className="ep-page-header-stat">
              <span>{s.label}</span>
              <strong>{s.value}</strong>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  )
}
