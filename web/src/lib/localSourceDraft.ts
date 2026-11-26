export function localSourceFilterOptions(stats: Record<string, number>) {
 const labels: Record<string, string> = { subscription: '订阅', free_proxy: '免费', inline: '内置', nodes_file: '文件', local_source: '本地源', unknown: '未知' }
 return Object.entries(stats).sort((a,b) => b[1]-a[1] || a[0].localeCompare(b[0])).map(([value,count]) => ({ value, label: `${labels[value] || value} ${count}` }))
}
export function localMemberRows(source: { nodes: { uri: string; port?: number }[] }, health: { status: string; tag?: string }[]) {
 const labels: Record<string,string> = { available:'可用', unavailable:'不可用', untested:'未检测', unapplied:'未应用', not_running:'未运行' }
 return source.nodes.map((node,index) => ({ index, name:localMemberLabel(node.uri,index), port:node.port || '应用后分配', health:labels[health[index]?.status] || '状态待刷新', tag:health[index]?.tag }))
}

export function canSaveLocalDraft(content: string, previewed: string | null, valid: boolean) {
  return !!content.trim() && valid && content === previewed
}
export function localReplacementDiff(before: string, after: string) {
  const keys = (text: string) => new Set(text.replace(/^﻿/, '').split(/\r?\n/).map(s => s.trim()).filter(s => s && !s.startsWith('#')).map(s => s.split('#')[0]))
  const old = keys(before), next = keys(after)
  return { added: [...next].filter(s => !old.has(s)).length, removed: [...old].filter(s => !next.has(s)).length, retained: [...next].filter(s => old.has(s)).length }
}
export function localSaveIntent(original: string | null, content: string, previewed: string | null, valid: boolean) {
  const metadataOnly = original !== null && original === content
  return { allowed: metadataOnly || canSaveLocalDraft(content, previewed, valid), confirmReplacement: original !== null && !metadataOnly, content: metadataOnly ? undefined : content }
}
export function localPendingMessage(state?: { pending: boolean; revision: number }) {
  return state?.pending ? `草稿版本 ${state.revision} 尚未应用；普通重载和程序重启不会应用它。` : ''
}
export function localMemberLabel(uri: string, index: number) {
  const fragment = uri.split('#')[1]
  try { if (fragment) return decodeURIComponent(fragment) } catch { /* malformed label */ }
  return `成员 ${index + 1}`
}
