import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath } from 'url'
import { dirname, resolve } from 'path'

const __dirname = dirname(fileURLToPath(import.meta.url))

export default defineConfig(({ mode }) => {
  // Read .env from the project root (one level up from ui/)
  const env = loadEnv(mode, resolve(__dirname, '..'), '')
  const backendPort = env.PORT || '8080'

  return {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        '@': resolve(__dirname, 'src'),
      },
    },
    build: {
      outDir: '../internal/ui/dist',
      emptyOutDir: true,
      rollupOptions: {
        output: {
          // Vendor code changes far less often than the app: separate chunks
          // stay cached in browsers across Dockyard upgrades.
          manualChunks(id) {
            if (!id.includes('node_modules')) return undefined
            if (/[\\/]node_modules[\\/](react|react-dom|scheduler)[\\/]/.test(id)) return 'react'
            if (/[\\/]node_modules[\\/](radix-ui|@radix-ui)[\\/]/.test(id)) return 'radix'
            if (/[\\/]node_modules[\\/](i18next|react-i18next|i18next-browser-languagedetector)[\\/]/.test(id)) return 'i18n'
            if (/[\\/]node_modules[\\/]@tanstack[\\/]/.test(id)) return 'query'
            return 'vendor'
          },
        },
      },
    },
    server: {
      proxy: {
        '/api': { target: `http://localhost:${backendPort}`, changeOrigin: true },
        '/health': { target: `http://localhost:${backendPort}`, changeOrigin: true },
      },
    },
  }
})
