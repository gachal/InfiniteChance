import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// Dev proxy forwards /api/* to the gateway server and /canvas-api/* to the
// canvas server, so the page talks to same-origin backends without CORS setup.
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api/, ''),
      },
      '/canvas-api': {
        target: 'http://localhost:8081',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/canvas-api/, ''),
      },
    },
  },
})
