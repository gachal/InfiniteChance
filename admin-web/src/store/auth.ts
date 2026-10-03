import type { UserInfo } from '@vben/types';

import { ref } from 'vue';
import { useRouter } from 'vue-router';

import { LOGIN_PATH } from '@vben/constants';
import { preferences } from '@vben/preferences';
import { resetAllStores, useAccessStore, useUserStore } from '@vben/stores';

import { notification } from 'ant-design-vue';
import { defineStore } from 'pinia';

import { getAccessCodesApi, getAuthStatusApi, getUserInfoApi, loginApi } from '#/api';
import { $t } from '#/locales';

export const useAuthStore = defineStore('auth', () => {
  const accessStore = useAccessStore();
  const userStore = useUserStore();
  const router = useRouter();

  const loginLoading = ref(false);

  /**
   * 异步处理登录操作(POST /auth/login)
   * @param params 登录表单数据
   */
  async function authLogin(
    params: { password?: string; username?: string },
    onSuccess?: () => Promise<void> | void,
  ) {
    // 异步处理用户登录操作并获取 accessToken
    let userInfo: null | UserInfo = null;
    try {
      loginLoading.value = true;
      const session = await loginApi(params);

      // 如果成功获取到 accessToken
      if (session.token) {
        accessStore.setAccessToken(session.token);

        // 获取用户信息并存储到 userStore 中
        const [fetchUserInfoResult, accessCodes] = await Promise.all([
          fetchUserInfo(),
          getAccessCodesApi(),
        ]);

        userInfo = fetchUserInfoResult;

        accessStore.setAccessCodes(accessCodes);

        if (accessStore.loginExpired) {
          accessStore.setLoginExpired(false);
        } else {
          if (onSuccess) {
            await onSuccess();
          } else {
            await router.push(
              userInfo.homePath || preferences.app.defaultHomePath,
            );
          }
        }

        if (userInfo?.realName) {
          notification.success({
            description: `${$t('authentication.loginSuccessDesc')}:${userInfo?.realName}`,
            duration: 3,
            message: $t('authentication.loginSuccess'),
          });
        }
      }
    } finally {
      loginLoading.value = false;
    }

    return {
      userInfo,
    };
  }

  /**
   * 后端 JWT 无状态,登出即清本地会话并回登录页(不调后端)。
   */
  async function logout(redirect: boolean = true) {
    resetAllStores();
    accessStore.setLoginExpired(false);

    // 已经在登录页时不能再带 redirect:此时 currentRoute.fullPath 就是登录页本身,
    // 再编码一层会得到「登录页?redirect=编码后的登录页」,下一次又在这个基础上再包一层,
    // 反复登出会让 URL 逐跳变长,且没有上限。
    const currentRoute = router.currentRoute.value;
    const alreadyOnLogin = currentRoute.path === LOGIN_PATH;

    // 回登录页带上当前路由地址
    await router.replace({
      path: LOGIN_PATH,
      query:
        redirect && !alreadyOnLogin
          ? { redirect: encodeURIComponent(currentRoute.fullPath) }
          : {},
    });
  }

  async function fetchUserInfo() {
    const userInfo = await getUserInfoApi();
    userStore.setUserInfo(userInfo);
    return userInfo;
  }

  /**
   * 引导状态(GET /auth/status):null = 未知(网络故障),false = 未初始化。
   * 会话内缓存;失败的探测不缓存,后端恢复后的下一次导航可重试。
   */
  async function ensureAuthStatus(): Promise<null | boolean> {
    if (authStatus === null) {
      authStatus = getAuthStatusApi()
        .then((status) => status.initialized)
        .catch(() => {
          // 失败不缓存,且按「未知」处理(守卫放行,登录页展示连接错误)
          authStatus = null;
          return null;
        });
    }
    return authStatus;
  }

  /**
   * 首管理员向导创建成功后调用:后端 initialized 已翻真,失效会话内
   * 的旧缓存,守卫不再把向导完成后的导航弹回 Init 页。
   */
  function invalidateAuthStatus(): void {
    authStatus = null;
  }

  function $reset() {
    loginLoading.value = false;
  }

  return {
    $reset,
    authLogin,
    ensureAuthStatus,
    fetchUserInfo,
    invalidateAuthStatus,
    loginLoading,
    logout,
  };
});

// store setup 之外持有引导状态缓存(模块级单例)
let authStatus: Promise<null | boolean> | null = null;
