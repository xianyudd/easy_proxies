import { create } from 'zustand'

type Tab = 'extractor'|'overview'|'config'|'quality'|'status'|'settings'|'diagnostics'
type Theme = 'dark'|'light'
type AuthState = 'unknown'|'authenticated'|'unauthenticated'

interface AppState {
  activeTab: Tab
  theme: Theme
  authenticated: AuthState
  setActiveTab: (tab: Tab) => void
  setTheme: (theme: Theme) => void
  setAuthenticated: (value: AuthState) => void
}

export const useAppStore = create<AppState>((set) => ({
  activeTab: 'extractor',
  theme: 'dark',
  authenticated: 'unknown',
  setActiveTab: (activeTab) => set({ activeTab }),
  setTheme: (theme) => set({ theme }),
  setAuthenticated: (authenticated) => set({ authenticated }),
}))
