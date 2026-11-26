import type { ConfigNode } from '../types/configNode'
import type { SettingsResponse } from '../types/settings'

function trimmedText(value: unknown) {
  return typeof value === 'string' ? value.trim() : ''
}

function subscriptionDisplayName(value: unknown) {
  const name = trimmedText(value)
  return name.toLowerCase() === '魔戒.net' ? '魔戒' : name
}

export function nodeSourceLabel(node: Pick<ConfigNode, 'subscription_name' | 'local_sources'>, fallback: string) {
  const local = node.local_sources?.map(source => source.name).join('、')
  const subscription = subscriptionDisplayName(node.subscription_name)
  return [subscription, local].filter(Boolean).join(' · ') || fallback
}

export function filterConfigNodes(rows: ConfigNode[], search: string, sourceFilter: string) {
  const term = search.trim().toLowerCase()
  return rows.filter(node => {
    const source = String(node.source || 'unknown')
    if (sourceFilter !== 'all' && source !== sourceFilter) return false
    if (!term) return true
    const haystack = `${node.name || ''} ${node.uri || ''} ${node.source || ''} ${node.port || ''} ${trimmedText(node.subscription_name)} ${node.local_sources?.map(source => source.name).join(' ') || ''}`
    return haystack.toLowerCase().includes(term)
  })
}

export function subscriptionDraftLabels(
  draftUrls: string[],
  settings?: Pick<SettingsResponse, 'subscriptions' | 'subscription_names'>,
) {
  const urls = settings?.subscriptions
  const names = settings?.subscription_names
  const labelsByUrl = new Map<string, string[]>()

  // Match names to the saved URLs before matching against editable rows.
  if (Array.isArray(urls) && Array.isArray(names)) {
    urls.forEach((value, index) => {
      const url = trimmedText(value)
      if (!url) return
      const namesForUrl = labelsByUrl.get(url) ?? []
      namesForUrl.push(subscriptionDisplayName(names[index]))
      labelsByUrl.set(url, namesForUrl)
    })
  }

  return draftUrls.map(url => labelsByUrl.get(url.trim())?.shift() ?? '')
}
