export const REGION_META: Record<string, { label: string; emoji: string; color: string }> = {
  all: { label: '全部', emoji: '🌐', color: '#64748b' },
  us: { label: '美国', emoji: '🇺🇸', color: '#3b82f6' },
  jp: { label: '日本', emoji: '🇯🇵', color: '#ef4444' },
  hk: { label: '香港', emoji: '🇭🇰', color: '#f97316' },
  sg: { label: '新加坡', emoji: '🇸🇬', color: '#10b981' },
  tw: { label: '台湾', emoji: '🇹🇼', color: '#14b8a6' },
  kr: { label: '韩国', emoji: '🇰🇷', color: '#8b5cf6' },
  in: { label: '印度', emoji: '🇮🇳', color: '#f59e0b' },
  ae: { label: '阿联酋', emoji: '🇦🇪', color: '#22c55e' },
  ch: { label: '瑞士', emoji: '🇨🇭', color: '#dc2626' },
  au: { label: '澳大利亚', emoji: '🇦🇺', color: '#06b6d4' },
  de: { label: '德国', emoji: '🇩🇪', color: '#eab308' },
  gb: { label: '英国', emoji: '🇬🇧', color: '#6366f1' },
  ca: { label: '加拿大', emoji: '🇨🇦', color: '#f43f5e' },
  other: { label: '其他', emoji: '🧩', color: '#94a3b8' },
}

export function regionMeta(code?: string) {
  return REGION_META[String(code || 'other').toLowerCase()] || REGION_META.other
}
