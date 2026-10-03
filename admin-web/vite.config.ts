import { defineConfig } from '@vben/vite-config';

// dev 代理照旧:/api/* → 网关(:8080)、/canvas-api/* → 画布服务(:8081),
// 同前缀去前缀约定与部署形态一致(nginx/desktop 同契约)。
// ADMIN_DEV_GATEWAY / ADMIN_DEV_CANVAS 可覆写目标(隔离库冒烟时指向临时网关)。
export default defineConfig(async () => {
  return {
    application: {},
    vite: {
      server: {
        proxy: {
          '/api': {
            changeOrigin: true,
            rewrite: (path) => path.replace(/^\/api/, ''),
            target: process.env.ADMIN_DEV_GATEWAY ?? 'http://localhost:8080',
          },
          '/canvas-api': {
            changeOrigin: true,
            rewrite: (path) => path.replace(/^\/canvas-api/, ''),
            target: process.env.ADMIN_DEV_CANVAS ?? 'http://localhost:8081',
          },
        },
      },
    },
  };
});
