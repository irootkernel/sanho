import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  // Load env variables
  const env = loadEnv(mode, process.cwd(), '')

  // Determine proxy target:
  // - Docker: VITE_KKACHI_API_URL (e.g., http://server:5789)
  // - Local: localhost:5789
  const proxyTarget = env.VITE_KKACHI_API_URL || 'http://localhost:5789'

  return {
    plugins: [react()],
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    server: {
      proxy: {
        // Proxy /api requests to the kkachi-server
        // Web always calls /api/state, server receives /api/state as-is
        // No rewrite needed - server handles /api/state directly
        '/api': {
          target: proxyTarget,
          changeOrigin: true,
        },
      },
    },
    test: {
      globals: true,
      environment: 'jsdom',
      setupFiles: ['./src/test/setup.ts'],
      include: ['src/**/*.{test,spec}.{ts,tsx}'],
    },
  }
})
