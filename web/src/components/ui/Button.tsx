import type { ButtonHTMLAttributes, ReactNode } from 'react'

type Variant = 'primary'|'secondary'|'danger'|'ghost'
export function Button({ variant = 'secondary', children, className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement> & {variant?: Variant; children: ReactNode}) {
  return <button className={`btn btn-${variant} ${className}`} {...props}>{children}</button>
}
