import { useLayoutEffect, useRef, type ReactNode } from 'react'
import gsap from 'gsap'
import { ensureMotionConfigured, prefersReducedMotion, reveal } from '../../lib/motion'

type PageMotionProps = {
  children: ReactNode
  className?: string
  /** Changes when route/tab changes so enter animation re-runs */
  motionKey?: string | number
  stagger?: string
}

/**
 * Page body enter: short fade + rise. Never leaves content stuck hidden.
 */
export function PageMotion({
  children,
  className = '',
  motionKey,
  stagger = ':scope > *',
}: PageMotionProps) {
  const rootRef = useRef<HTMLDivElement>(null)

  useLayoutEffect(() => {
    ensureMotionConfigured()
    const root = rootRef.current
    if (!root) return

    const targets = Array.from(root.querySelectorAll<HTMLElement>(stagger))
    const safeTargets = targets.length ? targets : [root]

    if (prefersReducedMotion()) {
      reveal(safeTargets)
      return
    }

    const ctx = gsap.context(() => {
      gsap.fromTo(
        safeTargets,
        { autoAlpha: 0, y: 10 },
        {
          autoAlpha: 1,
          y: 0,
          duration: 0.32,
          stagger: 0.04,
          ease: 'power2.out',
          onComplete: () => reveal(safeTargets),
          onInterrupt: () => reveal(safeTargets),
        },
      )
    }, root)

    // Failsafe: if something goes wrong, show content after 600ms
    const failSafe = window.setTimeout(() => reveal(safeTargets), 600)

    return () => {
      window.clearTimeout(failSafe)
      ctx.revert()
      reveal(safeTargets)
    }
  }, [stagger, motionKey])

  return (
    <div ref={rootRef} className={`ep-page-motion ${className}`.trim()}>
      {children}
    </div>
  )
}
