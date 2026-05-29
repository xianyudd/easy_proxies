import { Tag } from 'antd'
import type { ReactNode } from 'react'

const colors = {
  neutral: 'default',
  good: 'success',
  warn: 'warning',
  bad: 'error',
  info: 'processing',
} as const

export function Badge({ children, tone = 'neutral' }: {children: ReactNode; tone?: keyof typeof colors}) {
  return <Tag className="ep-badge" color={colors[tone]}>{children}</Tag>
}
