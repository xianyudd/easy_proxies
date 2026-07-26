import type { ReactNode } from 'react'

type Props = {
  active: boolean
  label?: string
  children?: ReactNode
}

/**
 * Indeterminate top progress using CSS (no remount, no GSAP loop jank).
 */
export function LoadingPulse({ active, label = '处理中…', children }: Props) {
  return (
    <div className="ep-loading-pulse" data-active={active ? '1' : '0'}>
      {children}
      {active ? (
        <>
          <div className="ep-loading-pulse-track" aria-hidden>
            <div className="ep-loading-pulse-bar" />
          </div>
          <span className="sr-only">{label}</span>
        </>
      ) : null}
    </div>
  )
}
