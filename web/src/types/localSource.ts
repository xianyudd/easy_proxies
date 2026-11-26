export interface LocalSourceSummary {
  id: string
  name: string
  enabled: boolean
  member_count: number
}
export interface LocalSource extends Omit<LocalSourceSummary, 'member_count'> {
  nodes: { uri: string; port?: number }[]
}
export interface LocalSourcesState {
  revision: number
  active_revision: number
  pending: boolean
  applying: boolean
  sources: LocalSourceSummary[]
}
export interface LocalSourceDetail {
 revision: number
 source: LocalSource
 member_health: { status: 'available' | 'unavailable' | 'untested' | 'unapplied' | 'not_running'; tag?: string }[]
}
export interface LocalSourcePreview {
 revision: number
 shared_count: number
 shared_non_local: number
 shared_draft: number
 shared_active: number
  member_count: number
  duplicates: { line: number; reason: string }[] | null
  protocols: Record<string, number>
}
