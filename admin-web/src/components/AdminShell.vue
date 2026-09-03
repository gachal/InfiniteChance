<script setup lang="ts">
// 管理后台布局外壳:标题栏 + 分区导航(网关管理/画布管理,规格定案的
// 两区)+ 会话操作。具体页面由路由呈现,经默认插槽嵌入。
import { useRouter } from 'vue-router'

import { useAuth } from '../auth'

const auth = useAuth()
const router = useRouter()

const sections = [
  {
    label: '网关管理',
    links: [
      { name: 'dashboard', label: '仪表盘' },
      { name: 'channels', label: '渠道管理' },
      { name: 'keys', label: 'API Key 管理' },
    ],
  },
  {
    label: '画布管理',
    links: [{ name: 'prompt-templates', label: '提示词模板' }],
  },
]

async function logout(): Promise<void> {
  auth.clearSession()
  await router.replace({ name: 'login' })
}
</script>

<template>
  <main class="shell">
    <header class="masthead">
      <div>
        <h1>InfiniteChance 管理后台</h1>
        <p class="subtitle">
          Token 网关 · 无限画布
        </p>
      </div>
      <div class="session">
        <span
          v-if="auth.username.value"
          class="whoami"
        >{{ auth.username.value }}</span>
        <button
          type="button"
          @click="logout"
        >
          退出登录
        </button>
      </div>
    </header>

    <nav
      class="nav"
      aria-label="管理分区"
    >
      <span
        v-for="section in sections"
        :key="section.label"
        class="nav-section"
      >
        <span class="nav-label">{{ section.label }}</span>
        <router-link
          v-for="link in section.links"
          :key="link.name"
          :to="{ name: link.name }"
          class="nav-link"
        >
          {{ link.label }}
        </router-link>
      </span>
    </nav>

    <slot />
  </main>
</template>

<style scoped>
.shell {
  max-width: 960px;
  margin: 0 auto;
  padding: 48px 24px;
}

.masthead {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.masthead h1 {
  margin: 0;
  font-size: 28px;
  letter-spacing: 0.5px;
}

.subtitle {
  margin: 6px 0 0;
  color: #8b91a7;
  font-size: 14px;
}

.session {
  display: flex;
  align-items: center;
  gap: 10px;
  padding-top: 4px;
}

.whoami {
  color: #8b91a7;
  font-size: 13px;
}

.session button {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 8px;
  padding: 6px 12px;
  color: inherit;
  font-size: 13px;
  cursor: pointer;
}

.session button:hover {
  background: rgba(255, 255, 255, 0.1);
}

.nav {
  display: flex;
  gap: 18px;
  margin-top: 20px;
  padding-bottom: 4px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}

.nav-section {
  display: flex;
  align-items: baseline;
  gap: 6px;
}

.nav-label {
  color: #5b627a;
  font-size: 12px;
  letter-spacing: 1px;
  margin-right: 4px;
  white-space: nowrap;
}

.nav-link {
  color: #8b91a7;
  text-decoration: none;
  font-size: 14px;
  padding: 8px 14px;
  border-radius: 10px 10px 0 0;
}

.nav-link:hover {
  color: inherit;
  background: rgba(255, 255, 255, 0.05);
}

.nav-link.router-link-active {
  color: inherit;
  font-weight: 600;
  background: rgba(76, 110, 245, 0.18);
}
</style>
