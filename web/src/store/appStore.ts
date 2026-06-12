import { create } from 'zustand'

type Tab = 'extractor'|'overview'|'review'|'config'|'quality'|'status'|'settings'|'diagnostics'
type Theme = 'dark'|'light'
type AuthState = 'unknown'|'authenticated'|'unauthenticated'

const THEME_STORAGE_KEY = 'easy-proxies-theme'

function initialTheme(): Theme {
  if (typeof window === 'undefined') return 'dark'
  try {
    return window.localStorage.getItem(THEME_STORAGE_KEY) === 'light' ? 'light' : 'dark'
  } catch {
    return 'dark'
  }
}

function persistTheme(theme: Theme) {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, theme)
  } catch {
    // Ignore storage failures; theme still applies for the current session.
  }
}

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
  theme: initialTheme(),
  authenticated: 'unknown',
  setActiveTab: (activeTab) => set({ activeTab }),
  setTheme: (theme) => {
    persistTheme(theme)
    set({ theme })
  },
  setAuthenticated: (authenticated) => set({ authenticated }),
}))
