import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// Dev proxy forwards /api/* to the gateway server and /canvas-api/* to the
// canvas server, so the page talks to same-origin backends without CORS setup.
export default defineConfig({
  plugins: [vue()],
  build: {
    target: 'es2020',
    rollupOptions: {
      output: {
        // 框架代码稳定缓存:业务改动不再拖上 vue/vue-router 一起失效。
        manualChunks: {
          vue: ['vue', 'vue-router'],
        },
      },
    },
  },
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
