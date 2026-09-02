import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// Dev proxy forwards /api/* to the canvas server so the page talks to the
// same-origin backend without CORS setup.
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5174,
    proxy: {
      '/api': {
        target: 'http://localhost:8081',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api/, ''),
      },
    },
  },
})
