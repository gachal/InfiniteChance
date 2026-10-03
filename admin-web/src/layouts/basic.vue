<script lang="ts" setup>
// 基础布局:单管理员形态裁剪——无通知中心/锁屏,用户下拉只留退出与
// 清空偏好(上游示例的通知 mock、文档/GitHub 链接全部去掉)。
import { computed } from 'vue';

import { AuthenticationLoginExpiredModal } from '@vben/common-ui';
import { BasicLayout, UserDropdown } from '@vben/layouts';
import { preferences } from '@vben/preferences';
import { useAccessStore, useUserStore } from '@vben/stores';

import { useAuthStore } from '#/store';
import LoginForm from '#/views/_core/authentication/login.vue';

defineOptions({ name: 'BasicLayoutView' });

const userStore = useUserStore();
const authStore = useAuthStore();
const accessStore = useAccessStore();

const menus = computed(() => [
  {
    handler: () => {
      authStore.logout();
    },
    icon: 'lucide:log-out',
    text: '退出登录',
  },
]);

const avatar = computed(() => {
  return userStore.userInfo?.avatar ?? preferences.app.defaultAvatar;
});

async function handleLogout() {
  await authStore.logout(false);
}
</script>

<template>
  <BasicLayout
    :avatar
    :menus
    :text="userStore.userInfo?.realName"
    @clear-preferences-and-logout="handleLogout"
    @logout="handleLogout"
  >
    <template #user-dropdown>
      <UserDropdown
        :avatar
        :description="userStore.userInfo?.username"
        :menus
        :text="userStore.userInfo?.realName"
        tag-text="管理员"
        @clear-preferences-and-logout="handleLogout"
        @logout="handleLogout"
      />
    </template>
    <template #extra>
      <AuthenticationLoginExpiredModal
        v-model:open="accessStore.loginExpired"
        :avatar
      >
        <LoginForm />
      </AuthenticationLoginExpiredModal>
    </template>
  </BasicLayout>
</template>
