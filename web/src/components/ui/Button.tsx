import { Button as AntButton } from 'antd'
import type { ButtonProps as AntButtonProps } from 'antd'
import type { ReactNode } from 'react'

type Variant = 'primary'|'secondary'|'danger'|'ghost'

type Props = Omit<AntButtonProps, 'type' | 'danger' | 'variant'> & { variant?: Variant; children: ReactNode }

export function Button({ variant = 'secondary', children, className = '', ...props }: Props) {
  const antProps: Pick<AntButtonProps, 'type' | 'danger'> = variant === 'primary'
    ? { type: 'primary' }
    : variant === 'danger'
      ? { type: 'primary', danger: true }
      : variant === 'ghost'
        ? { type: 'text' }
        : { type: 'default' }
  return <AntButton className={`ep-button ${className}`} {...antProps} {...props}>{children}</AntButton>
}
