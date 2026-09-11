import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// Dev proxy forwards /api/* to the canvas server so the page talks to the
// same-origin backend without CORS setup.
export default defineConfig({
  plugins: [vue()],
  build: {
    target: 'es2020',
    rollupOptions: {
      output: {
        // 框架与画布引擎稳定缓存:业务改动不再拖上 vue/vue-flow 一起失效。
        manualChunks: {
          vue: ['vue', 'vue-router'],
          flow: ['@vue-flow/core', '@vue-flow/background'],
        },
      },
    },
  },
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
