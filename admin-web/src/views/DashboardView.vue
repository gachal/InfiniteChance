<script setup lang="ts">
import { useRouter } from 'vue-router'

import { HealthCard } from '@infinitechance/ui'

import { useAuth } from '../auth'

const auth = useAuth()
const router = useRouter()

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

    <HealthCard title="网关服务健康" />
    <HealthCard
      title="画布服务健康"
      base="/canvas-api"
      error-label="无法连接画布服务"
    />
  </main>
</template>

<style scoped>
.shell {
  max-width: 640px;
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
</style>
