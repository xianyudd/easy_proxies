/**
 * Bootstrap screen shown while the session probe is in flight.
 *
 * Replaces flashing the password form on every browser reload: until auth
 * resolves we don't know whether a login is even required, so show neutral
 * branding with an indeterminate bar instead of an input the user may never
 * need to touch.
 */
export function BootScreen({ label = '正在校验会话…' }: { label?: string }) {
  return (
    <div className="ep-boot" role="status" aria-live="polite">
      <div className="ep-boot-inner">
        <div className="ep-boot-mark">EP</div>
        <div className="ep-boot-bar" aria-hidden>
          <div className="ep-boot-bar-run" />
        </div>
        <div className="ep-boot-label">{label}</div>
      </div>
    </div>
  )
}
