type Notify = (message: string, kind?: 'ok' | 'error' | 'info') => void

function clipboardErrorMessage(error: unknown) {
  if (error instanceof Error && error.message.trim()) return `复制失败：${error.message}`
  return '复制失败：浏览器拒绝访问剪贴板，请手动复制。'
}

export async function copyToClipboard(text: string, notify: Notify, successMessage = '已复制') {
  if (!text) {
    notify('没有可复制的内容', 'error')
    return false
  }
  try {
    if (!navigator.clipboard?.writeText) throw new Error('当前环境不支持 Clipboard API')
    await navigator.clipboard.writeText(text)
    notify(successMessage, 'ok')
    return true
  } catch (error) {
    notify(clipboardErrorMessage(error), 'error')
    return false
  }
}
