/**
 * 路由与登录守卫:未登录访问受保护页面跳转登录,未初始化时跳转初始化引导,
 * 登录/初始化完成后回到画布列表。守卫逻辑与 admin-web 一致,
 * 只是账号体系由 canvas/server 提供(共享网关的 admin_accounts 与 JWT_SECRET)。
 */
import { createRouter, createWebHistory } from 'vue-router'

import { useAuth } from './auth'
import CanvasEditorView from './views/CanvasEditorView.vue'
import CanvasListView from './views/CanvasListView.vue'
import InitView from './views/InitView.vue'
import LoginView from './views/LoginView.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'canvases', component: CanvasListView, meta: { requiresAuth: true } },
    {
      path: '/canvas/:id',
      name: 'canvas-editor',
      component: CanvasEditorView,
      meta: { requiresAuth: true },
    },
    { path: '/login', name: 'login', component: LoginView },
    { path: '/init', name: 'init', component: InitView },
    { path: '/:pathMatch(.*)*', redirect: { name: 'canvases' } },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuth()
  try {
    await auth.ensureBooted()
  } catch {
    // 引导失败(后端不可达):放行去登录页,由视图展示错误并重试。
    if (to.name !== 'login' && to.name !== 'init') {
      return { name: 'login' }
    }
    return true
  }

  if (to.name === 'init') {
    // 引导只在全新库出现;已初始化或状态未知都回登录页。
    return auth.initialized.value === false && !auth.isAuthenticated.value
      ? true
      : { name: 'login' }
  }

  if (to.name === 'login') {
    if (auth.isAuthenticated.value) {
      return { name: 'canvases' }
    }
    return auth.initialized.value === false ? { name: 'init' } : true
  }

  if (to.meta.requiresAuth && !auth.isAuthenticated.value) {
    return { name: 'login', query: to.fullPath === '/' ? {} : { redirect: to.fullPath } }
  }
  return true
})
