import { create } from 'zustand'

interface ToastState { message: string; kind: 'ok'|'error'|'info'; show: (message: string, kind?: ToastState['kind']) => void }
export const useToast = create<ToastState>((set) => ({
  message: '',
  kind: 'info',
  show: (message, kind = 'info') => {
    set({ message, kind })
    window.setTimeout(() => set({ message: '' }), 2600)
  },
}))
export function Toast() {
  const { message, kind } = useToast()
  if (!message) return null
  return <div className={`toast toast-${kind}`}>{message}</div>
}
