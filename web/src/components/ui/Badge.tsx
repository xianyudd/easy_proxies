import type { ReactNode } from 'react'
export function Badge({ children, tone = 'neutral' }: {children: ReactNode; tone?: 'neutral'|'good'|'warn'|'bad'|'info'}) {
  return <span className={`badge badge-${tone}`}>{children}</span>
}
