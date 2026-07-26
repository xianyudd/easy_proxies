import gsap from 'gsap'

/** True when user prefers reduced motion. */
export function prefersReducedMotion(): boolean {
  if (typeof window === 'undefined') return true
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/** Shared GSAP defaults for the design preview shell. */
export function configureMotion() {
  gsap.defaults({
    ease: 'power2.out',
    duration: 0.35,
    overwrite: 'auto',
  })
}

/** Force element(s) fully visible and clear GSAP inline styles. */
export function reveal(targets: gsap.TweenTarget) {
  gsap.set(targets, { clearProps: 'all', autoAlpha: 1, opacity: 1, y: 0, x: 0, scale: 1 })
}

let configured = false
export function ensureMotionConfigured() {
  if (configured) return
  configured = true
  configureMotion()
}
