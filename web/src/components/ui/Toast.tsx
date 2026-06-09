import { useEffect } from 'react'
import { App } from 'antd'
import { create } from 'zustand'

interface ToastState {
  message: string
  kind: 'ok'|'error'|'info'
  nonce: number
  show: (message: string, kind?: ToastState['kind']) => void
}

let toastTimer: number | undefined

export const useToast = create<ToastState>((set) => ({
  message: '',
  kind: 'info',
  nonce: 0,
  show: (content, kind = 'info') => {
    if (toastTimer !== undefined) window.clearTimeout(toastTimer)
    set(state => ({ message: content, kind, nonce: state.nonce + 1 }))
    toastTimer = window.setTimeout(() => {
      set({ message: '' })
      toastTimer = undefined
    }, 2600)
  },
}))

export function Toast() {
  const { message: messageApi } = App.useApp()
  const content = useToast(s => s.message)
  const kind = useToast(s => s.kind)
  const nonce = useToast(s => s.nonce)

  useEffect(() => {
    if (!content) return
    if (kind === 'ok') messageApi.success(content)
    else if (kind === 'error') messageApi.error(content)
    else messageApi.info(content)
  }, [content, kind, nonce, messageApi])

  return null
}
