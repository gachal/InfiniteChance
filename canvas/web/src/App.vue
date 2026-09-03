<script setup lang="ts">
// 根组件:路由出口。会话守卫见 router.ts。
</script>

<template>
  <!-- key 挂 fullPath:画布 id 变化时强制重建编辑器,避免复用旧 flow 状态。 -->
  <router-view :key="$route.fullPath" />
</template>
