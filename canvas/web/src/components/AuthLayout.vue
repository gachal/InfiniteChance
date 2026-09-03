<script setup lang="ts">
// 登录/初始化共用的居中卡片外壳。
</script>

<template>
  <main class="auth-shell">
    <section class="auth-card">
      <h1>InfiniteChance 无限画布</h1>
      <p class="tagline">
        提示词 · 图片 · 视频
      </p>
      <slot />
    </section>
  </main>
</template>

<style scoped>
.auth-shell {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}

.auth-card {
  width: 100%;
  max-width: 380px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 16px;
  padding: 36px 32px;
}

.auth-card h1 {
  margin: 0;
  font-size: 22px;
  letter-spacing: 0.5px;
  text-align: center;
}

.tagline {
  margin: 6px 0 24px;
  color: #8b91a7;
  font-size: 13px;
  text-align: center;
}
</style>
