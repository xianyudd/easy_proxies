import { useLayoutEffect, useRef, type ReactNode } from 'react'
import gsap from 'gsap'
import { ensureMotionConfigured, prefersReducedMotion } from '../../lib/motion'

type Props = {
  /** Bumps when content should play a soft success pulse */
  pulseKey?: string | number
  className?: string
  children: ReactNode
}

/** Soft success flash for result panels / selected bars. */
export function SuccessPulse({ pulseKey, className = '', children }: Props) {
  const ref = useRef<HTMLDivElement>(null)

  useLayoutEffect(() => {
    if (pulseKey == null || pulseKey === 0) return
    const el = ref.current
    if (!el || prefersReducedMotion()) return
    ensureMotionConfigured()
    const tween = gsap.fromTo(
      el,
      { boxShadow: '0 0 0 0 rgba(47,156,154,0)' },
      {
        boxShadow: '0 0 0 1px rgba(47,156,154,0.45), 0 12px 36px rgba(47,156,154,0.14)',
        duration: 0.28,
        yoyo: true,
        repeat: 1,
        ease: 'power1.out',
      },
    )
    return () => {
      tween.kill()
      gsap.set(el, { clearProps: 'boxShadow' })
    }
  }, [pulseKey])

  return (
    <div ref={ref} className={className}>
      {children}
    </div>
  )
}
