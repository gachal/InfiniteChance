<script setup lang="ts">
// 仪表盘:两个服务的健康状态;布局与导航由 vben 布局提供(35 号票)。
import { HealthCard } from '@infinitechance/ui'

</script>

<template>
  <main class="admin-page">
    <HealthCard title="网关服务健康" />
    <HealthCard
      title="画布服务健康"
      base="/canvas-api"
      error-label="无法连接画布服务"
    />
  </main>
</template>
