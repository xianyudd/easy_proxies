import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:9091',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: '../internal/monitor/assets/dist',
    emptyOutDir: true,
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks: {
          echarts: ['echarts'],
          vendor: ['react', 'react-dom', '@tanstack/react-query', 'zustand'],
        },
      },
    },
  },
})
