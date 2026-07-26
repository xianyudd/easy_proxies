/**
 * Trigger a browser download of plain-text content. Shared by the log export
 * (Diagnostics) and proxy export (Extractor) flows.
 */
export function downloadText(filename: string, text: string, mime = 'text/plain;charset=utf-8') {
  const url = URL.createObjectURL(new Blob([text], { type: mime }))
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 0)
}
