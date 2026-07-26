import React from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntApp, ConfigProvider, theme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import App from './App'
import { useAppStore } from './store/appStore'
import './styles/globals.css'

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } } })

/** Align antd with theme.css tokens (teal brand, not default blue). */
function ThemedApp() {
  const currentTheme = useAppStore(s => s.theme)
  const isDark = currentTheme === 'dark'
  return (
    <ConfigProvider
      locale={zhCN}
      button={{ autoInsertSpace: false }}
      theme={{
        algorithm: isDark ? theme.darkAlgorithm : theme.defaultAlgorithm,
        token: {
          colorPrimary: isDark ? '#5bc0be' : '#2f9c9a',
          colorSuccess: isDark ? '#34d399' : '#10b981',
          colorWarning: isDark ? '#fbbf24' : '#f59e0b',
          colorError: isDark ? '#fb7185' : '#ef4444',
          colorInfo: isDark ? '#60a5fa' : '#3b82f6',
          colorBgBase: isDark ? '#070d1a' : '#f5f7f8',
          colorTextBase: isDark ? '#f5f7fb' : '#0b132b',
          borderRadius: 10,
          fontFamily: '"Aptos", "Segoe UI Variable", "Helvetica Neue", "Noto Sans SC", ui-sans-serif, system-ui, sans-serif',
          fontFamilyCode: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
        },
        components: {
          Button: { controlHeight: 38, borderRadius: 10 },
          Input: { controlHeight: 40, borderRadius: 10, paddingInline: 14 },
          Select: { controlHeight: 40, borderRadius: 10 },
          Checkbox: { borderRadiusSM: 6 },
          Table: { headerBorderRadius: 10, cellPaddingBlock: 12, cellPaddingInline: 14 },
          Tag: { borderRadiusSM: 999 },
          Modal: { borderRadiusLG: 14 },
        },
      }}
    >
      <AntApp>
        <App />
      </AntApp>
    </ConfigProvider>
  )
}

const rootElement = document.getElementById('root')

if (!rootElement) {
  throw new Error('Easy Proxies WebUI failed to start: missing #root container')
}

createRoot(rootElement).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemedApp />
    </QueryClientProvider>
  </React.StrictMode>,
)
