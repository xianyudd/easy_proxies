import type { ReactNode } from 'react'
import { PageHeader, type PageHeaderStat } from './PageHeader'
import { PageMotion } from '../motion/PageMotion'
import { useAppStore } from '../../store/appStore'

type PageProps = {
  title?: ReactNode
  eyebrow?: string
  description?: ReactNode
  actions?: ReactNode
  stats?: PageHeaderStat[]
  className?: string
  headerClassName?: string
  children: ReactNode
  toolbar?: ReactNode
}

/**
 * Standard page chrome for the design-system preview shell.
 * All feature pages should render through this for consistent density.
 */
export function Page({
  title,
  eyebrow,
  description,
  actions,
  stats,
  className = '',
  headerClassName = '',
  toolbar,
  children,
}: PageProps) {
  const activeTab = useAppStore(s => s.activeTab)
  return (
    <div className={`page ep-page ${className}`.trim()}>
      {title != null ? (
        <PageHeader
          className={headerClassName}
          eyebrow={eyebrow}
          title={title}
          description={description}
          actions={actions}
          stats={stats}
        />
      ) : null}
      {toolbar ? <div className="ep-page-toolbar">{toolbar}</div> : null}
      <div className="ep-page-body">
        <PageMotion motionKey={activeTab}>{children}</PageMotion>
      </div>
    </div>
  )
}

export type { PageHeaderStat }
