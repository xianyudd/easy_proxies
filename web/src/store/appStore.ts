import { create } from 'zustand'

type Tab = 'extractor'|'overview'|'quality'|'status'|'settings'|'diagnostics'
type Theme = 'dark'|'light'

interface AppState {
  activeTab: Tab
  theme: Theme
  authenticated: boolean
  setActiveTab: (tab: Tab) => void
  setTheme: (theme: Theme) => void
  setAuthenticated: (value: boolean) => void
}

export const useAppStore = create<AppState>((set) => ({
  activeTab: 'extractor',
  theme: 'dark',
  authenticated: true,
  setActiveTab: (activeTab) => set({ activeTab }),
  setTheme: (theme) => set({ theme }),
  setAuthenticated: (authenticated) => set({ authenticated }),
}))
