import type { LucideIcon } from 'lucide-react'
import {
  Activity,
  FileSearch,
  Gauge,
  KeyRound,
  List,
  MapPin,
  ServerCog,
  Settings,
  ShieldCheck,
} from 'lucide-react'

/** App tab ids — keep in sync with store + pages. */
export type AppTab =
  | 'extractor'
  | 'overview'
  | 'review'
  | 'config'
  | 'quality'
  | 'status'
  | 'api-keys'
  | 'settings'
  | 'diagnostics'

export type NavGroupId = 'use' | 'nodes' | 'observe' | 'access' | 'system'

export type AppRoute = {
  id: AppTab
  hash: string
  /** Alternate hashes that resolve to this tab */
  aliases?: string[]
  title: string
  eyebrow: string
  description: string
  icon: LucideIcon
  group: NavGroupId
}

export const NAV_GROUPS: { id: NavGroupId; label: string }[] = [
  { id: 'use', label: '使用' },
  { id: 'nodes', label: '节点' },
  { id: 'observe', label: '观测' },
  { id: 'access', label: '访问' },
  { id: 'system', label: '系统' },
]

export const APP_ROUTES: AppRoute[] = [
  {
    id: 'extractor',
    hash: 'extractor',
    title: '代理提取',
    eyebrow: 'Workbench',
    description: '按范围生成代理结果，复制或下载，主路径尽量少跳转。',
    icon: FileSearch,
    group: 'use',
  },
  {
    id: 'overview',
    hash: 'nodes',
    aliases: ['overview'],
    title: '节点总览',
    eyebrow: 'Inventory',
    description: '浏览节点清单、延迟与可用性，快速定位异常入口。',
    icon: List,
    group: 'nodes',
  },
  {
    id: 'review',
    hash: 'region-review',
    aliases: ['unclassified'],
    title: '待确认节点',
    eyebrow: 'Review',
    description: '确认地区归属，减少错误路由与池污染。',
    icon: MapPin,
    group: 'nodes',
  },
  {
    id: 'config',
    hash: 'config',
    aliases: ['node-config'],
    title: '节点配置',
    eyebrow: 'Config',
    description: '维护节点配置条目与重载相关操作。',
    icon: ServerCog,
    group: 'nodes',
  },
  {
    id: 'quality',
    hash: 'quality',
    title: '节点质量',
    eyebrow: 'Quality',
    description: 'CF / 风险与综合质量扫描，筛选更稳的出口。',
    icon: ShieldCheck,
    group: 'nodes',
  },
  {
    id: 'status',
    hash: 'status',
    title: '运行状态',
    eyebrow: 'Monitor',
    description: '健康度、趋势与异常节点同屏，优先处理问题。',
    icon: Gauge,
    group: 'observe',
  },
  {
    id: 'diagnostics',
    hash: 'diagnostics',
    title: '日志诊断',
    eyebrow: 'Console',
    description: '实时日志与运行摘要，排查时保持终端效率。',
    icon: Activity,
    group: 'observe',
  },
  {
    id: 'api-keys',
    hash: 'api-keys',
    aliases: ['apikeys'],
    title: 'API Key',
    eyebrow: 'Access Control',
    description: '签发、启停、轮换与试验访问凭证。',
    icon: KeyRound,
    group: 'access',
  },
  {
    id: 'settings',
    hash: 'settings',
    aliases: ['subscriptions', 'free-proxy', 'pool', 'multi-port', 'routing', 'quality-check', 'management'],
    title: '系统设置',
    eyebrow: 'System',
    description: '订阅、池模式、路由与管理面等全局配置。',
    icon: Settings,
    group: 'system',
  },
]

const byId = new Map(APP_ROUTES.map(r => [r.id, r]))

/** hash (with or without #) → tab */
export function tabFromHash(hash: string): AppTab {
  const raw = (hash || '').replace(/^#/, '').trim().toLowerCase()
  if (!raw) return 'extractor'
  for (const r of APP_ROUTES) {
    if (r.hash === raw) return r.id
    if (r.aliases?.includes(raw)) return r.id
  }
  return 'extractor'
}

export function routeById(id: AppTab): AppRoute {
  return byId.get(id) || APP_ROUTES[0]
}

export function hashForTab(id: AppTab): string {
  return `#${routeById(id).hash}`
}
