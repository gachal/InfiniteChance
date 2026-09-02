<script setup lang="ts">
// 仪表盘:两个服务的健康状态;布局与导航由 AdminShell 提供。
import { HealthCard } from '@infinitechance/ui'

import AdminShell from '../components/AdminShell.vue'
</script>

<template>
  <AdminShell>
    <HealthCard title="网关服务健康" />
    <HealthCard
      title="画布服务健康"
      base="/canvas-api"
      error-label="无法连接画布服务"
    />
  </AdminShell>
</template>
