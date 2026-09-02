<script setup lang="ts">
import { HealthCard } from '@infinitechance/ui'
</script>

<template>
  <main class="shell">
    <header class="masthead">
      <h1>InfiniteChance 管理后台</h1>
      <p class="subtitle">
        Token 网关 · 无限画布
      </p>
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
</style>
