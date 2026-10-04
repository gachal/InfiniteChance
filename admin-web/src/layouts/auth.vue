<script lang="ts" setup>
import { computed } from 'vue';

import { AuthPageLayout } from '@vben/layouts';
import { preferences } from '@vben/preferences';

import { $t } from '#/locales';

defineOptions({ name: 'AuthPageLayoutView' });

const appName = computed(() => preferences.app.name);
const logo = computed(() => preferences.logo.source);
const logoDark = computed(() => preferences.logo.sourceDark);
// 登录页左侧介绍区的品牌插图(public/slogan.png)
const sloganImage = `${import.meta.env.BASE_URL}slogan.png`;
</script>

<template>
  <AuthPageLayout
    :app-name="appName"
    :logo="logo"
    :logo-dark="logoDark"
    :page-description="$t('authentication.pageDesc')"
    :page-title="$t('authentication.pageTitle')"
    :slogan-image="sloganImage"
  >
    <!-- 自定义工具栏 -->
    <!-- <template #toolbar></template> -->
  </AuthPageLayout>
</template>

<style scoped>
/* vben 把 slogan 图写死为 h-64 w-2/5(固定高、宽随容器 40%),方图必被横向拉伸;
   slogan.png 是 1024×1024 方图,锁回等高方形,object-contain 兜底非方图素材 */
:deep(img[src$='slogan.png']) {
  width: 16rem;
  object-fit: contain;
}
</style>
