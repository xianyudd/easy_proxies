import { create } from 'zustand'
import type { ExtractorEntry, ExtractorParams, ExtractorResponse } from '../types/extractor'

interface ExtractorState {
  params: ExtractorParams
  entries: ExtractorEntry[]
  meta: string
  warnings: string[]
  setParams: (patch: Partial<ExtractorParams>) => void
  setResult: (data: ExtractorResponse) => void
  clear: () => void
}

export const defaultExtractorParams: ExtractorParams = {
  region: 'all',
  mode: 'multi-port',
  format: 'host_port_user_pass',
  count: 10,
  reveal: true,
}

function safeArray<T>(value: unknown): T[] {
  return Array.isArray(value) ? value : []
}

export const useExtractorStore = create<ExtractorState>((set) => ({
  params: defaultExtractorParams,
  entries: [],
  meta: '尚未生成',
  warnings: [],
  setParams: (patch) => set((state) => ({ params: { ...state.params, ...patch } })),
  setResult: (data) => set({
    entries: safeArray<ExtractorEntry>(data.entries),
    warnings: safeArray<string>(data.warnings),
    meta: `模式: ${data.mode} | 区域: ${data.region} | 格式: ${data.effective_format} | 输出: ${data.output_count}/${data.requested_count} | ${data.masked ? '已脱敏' : '真实凭据'}`,
  }),
  clear: () => set({ entries: [], warnings: [], meta: '尚未生成' }),
}))
