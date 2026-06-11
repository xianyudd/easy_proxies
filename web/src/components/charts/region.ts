import { ISO3166_COUNTRIES, ISO3166_COUNTRY_BY_CODE } from '../../data/iso3166'

const COLORS = ['#3b82f6', '#ef4444', '#f97316', '#10b981', '#14b8a6', '#8b5cf6', '#f59e0b', '#22c55e', '#dc2626', '#06b6d4', '#eab308', '#6366f1', '#f43f5e', '#0ea5e9', '#84cc16', '#a855f7', '#ec4899', '#64748b']

export const REGION_META: Record<string, { label: string; emoji: string; color: string }> = {
  all: { label: '全部', emoji: '🌐', color: '#64748b' },
  ...Object.fromEntries(ISO3166_COUNTRIES.map((item, index) => [item.code, { label: item.label, emoji: item.emoji, color: COLORS[index % COLORS.length] }])),
  other: { label: '其他', emoji: '🧩', color: '#94a3b8' },
}

export const REGION_OPTIONS = [
  { value: 'all', label: '全部(all)' },
  ...ISO3166_COUNTRIES.map(item => ({ value: item.code, label: `${item.label}(${item.code})` })),
  { value: 'other', label: '其他(other)' },
]

export const MANUAL_REGION_OPTIONS = ISO3166_COUNTRIES.map(item => ({ value: item.code, label: `${item.label}(${item.code})` }))

export function regionMeta(code?: string) {
  const key = String(code || 'other').toLowerCase()
  const fromIso = ISO3166_COUNTRY_BY_CODE[key]
  return REGION_META[key] || (fromIso ? { label: fromIso.label, emoji: fromIso.emoji, color: '#64748b' } : REGION_META.other)
}
