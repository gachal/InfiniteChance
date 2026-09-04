/**
 * 路由与登录守卫:未登录访问受保护页面跳转登录,
 * 未初始化时跳转初始化引导,登录/初始化完成后回仪表盘。
 */
import { createRouter, createWebHistory } from 'vue-router'

import { useAuth } from './auth'
import AssetsView from './views/AssetsView.vue'
import ChannelsView from './views/ChannelsView.vue'
import DashboardView from './views/DashboardView.vue'
import InitView from './views/InitView.vue'
import KeysView from './views/KeysView.vue'
import LoginView from './views/LoginView.vue'
import PromptTemplatesView from './views/PromptTemplatesView.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'dashboard', component: DashboardView, meta: { requiresAuth: true } },
    { path: '/channels', name: 'channels', component: ChannelsView, meta: { requiresAuth: true } },
    { path: '/keys', name: 'keys', component: KeysView, meta: { requiresAuth: true } },
    { path: '/prompt-templates', name: 'prompt-templates', component: PromptTemplatesView, meta: { requiresAuth: true } },
    { path: '/assets', name: 'assets', component: AssetsView, meta: { requiresAuth: true } },
    { path: '/login', name: 'login', component: LoginView },
    { path: '/init', name: 'init', component: InitView },
    { path: '/:pathMatch(.*)*', redirect: { name: 'dashboard' } },
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
      return { name: 'dashboard' }
    }
    return auth.initialized.value === false ? { name: 'init' } : true
  }

  if (to.meta.requiresAuth && !auth.isAuthenticated.value) {
    return { name: 'login', query: to.fullPath === '/' ? {} : { redirect: to.fullPath } }
  }
  return true
})
