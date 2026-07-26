import React from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntApp, ConfigProvider, theme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import App from './App'
import { useAppStore } from './store/appStore'
import './styles/globals.css'

const queryClient = new QueryClient({ defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } } })

/** Align antd with theme.css tokens (minimal engineering style, indigo brand). */
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
          colorPrimary: isDark ? '#7c85e8' : '#5661d6',
          colorSuccess: isDark ? '#4ade80' : '#15803d',
          colorWarning: isDark ? '#fbbf24' : '#b45309',
          colorError: isDark ? '#f87171' : '#dc2626',
          colorInfo: isDark ? '#60a5fa' : '#2563eb',
          colorBgBase: isDark ? '#101113' : '#f7f8fa',
          colorTextBase: isDark ? '#ededef' : '#16181d',
          colorBorder: isDark ? 'rgba(255, 255, 255, 0.14)' : 'rgba(18, 24, 38, 0.16)',
          colorBorderSecondary: isDark ? 'rgba(255, 255, 255, 0.09)' : 'rgba(18, 24, 38, 0.1)',
          borderRadius: 8,
          controlHeight: 34,
          fontFamily: '"Inter", "SF Pro Text", "Segoe UI Variable", "Segoe UI", "Noto Sans SC", "PingFang SC", "Microsoft YaHei", ui-sans-serif, system-ui, sans-serif',
          fontFamilyCode: 'ui-monospace, "SF Mono", SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace',
        },
        components: {
          Button: { controlHeight: 34, borderRadius: 8, fontWeight: 500, primaryShadow: 'none', defaultShadow: 'none', dangerShadow: 'none' },
          Input: { controlHeight: 36, borderRadius: 8, paddingInline: 12 },
          InputNumber: { controlHeight: 36, borderRadius: 8 },
          Select: { controlHeight: 36, borderRadius: 8 },
          Checkbox: { borderRadiusSM: 5 },
          Table: { headerBorderRadius: 8, cellPaddingBlock: 10, cellPaddingInline: 12 },
          Tag: { borderRadiusSM: 5 },
          Modal: { borderRadiusLG: 12 },
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
