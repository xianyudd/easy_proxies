import React from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntApp, ConfigProvider, theme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import App from './App'
import { useAppStore } from './store/appStore'
import './styles/globals.css'

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } } })

function ThemedApp() {
  const currentTheme = useAppStore(s => s.theme)
  const isDark = currentTheme === 'dark'
  return <ConfigProvider
    locale={zhCN}
    theme={{
      algorithm: isDark ? theme.darkAlgorithm : theme.defaultAlgorithm,
      token: {
        colorPrimary: '#2563eb',
        colorSuccess: '#059669',
        colorWarning: '#d97706',
        colorError: '#dc2626',
        colorBgBase: isDark ? '#0b1220' : '#f6f8fb',
        colorTextBase: isDark ? '#e5e7eb' : '#111827',
        borderRadius: 10,
        fontFamily: '"IBM Plex Sans", "Noto Sans SC", system-ui, sans-serif',
      },
      components: {
        Button: { controlHeight: 38, borderRadius: 11 },
        Input: { controlHeight: 40, borderRadius: 12, paddingInline: 14 },
        Checkbox: { borderRadiusSM: 5 },
        Table: { headerBorderRadius: 10, cellPaddingBlock: 12, cellPaddingInline: 14 },
        Tag: { borderRadiusSM: 999 },
      },
    }}
  >
    <AntApp>
      <App />
    </AntApp>
  </ConfigProvider>
}

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemedApp />
    </QueryClientProvider>
  </React.StrictMode>,
)
