import { useEffect, useRef, useState } from 'react'
import gsap from 'gsap'
import { ensureMotionConfigured, prefersReducedMotion } from '../../lib/motion'

const SESSION_KEY = 'ep-boot-splash-seen'

/**
 * One-shot boot splash per browser tab session (design preview only).
 */
export function BootSplash() {
  const [visible, setVisible] = useState(() => {
    if (typeof window === 'undefined') return false
    if (prefersReducedMotion()) return false
    try {
      return sessionStorage.getItem(SESSION_KEY) !== '1'
    } catch {
      return true
    }
  })

  const rootRef = useRef<HTMLDivElement>(null)
  const markRef = useRef<HTMLDivElement>(null)
  const fillRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!visible) return
    ensureMotionConfigured()
    const root = rootRef.current
    const mark = markRef.current
    const fill = fillRef.current
    if (!root || !mark || !fill) {
      setVisible(false)
      return
    }

    try {
      sessionStorage.setItem(SESSION_KEY, '1')
    } catch { /* ignore */ }

    const ctx = gsap.context(() => {
      const tl = gsap.timeline({
        onComplete: () => setVisible(false),
      })
      tl.fromTo(
        mark,
        { scale: 0.82, autoAlpha: 0 },
        { scale: 1, autoAlpha: 1, duration: 0.28, ease: 'back.out(1.4)' },
      )
        .fromTo(
          fill,
          { scaleX: 0 },
          { scaleX: 1, duration: 0.42, ease: 'power2.inOut' },
          '-=0.06',
        )
        .to(root, { autoAlpha: 0, duration: 0.22, ease: 'power1.out' }, '+=0.05')
    }, root)

    const maxWait = window.setTimeout(() => setVisible(false), 1200)
    return () => {
      window.clearTimeout(maxWait)
      ctx.revert()
    }
  }, [visible])

  if (!visible) return null

  return (
    <div ref={rootRef} className="ep-boot-splash" aria-hidden>
      <div className="ep-boot-inner">
        <div ref={markRef} className="ep-boot-mark">EP</div>
        <div className="ep-boot-bar">
          <div ref={fillRef} className="ep-boot-bar-fill" />
        </div>
        <div className="ep-boot-label">Easy Proxies</div>
      </div>
    </div>
  )
}
