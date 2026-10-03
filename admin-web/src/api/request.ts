/**
 * 该文件已按本仓后端契约调整:
 * - 管理 API 成功响应是裸载荷(无 {code,data} 包装):defaultResponseInterceptor
 *   配 responseReturn 'body' 后走裸体解包分支(successCode 不参与),
 *   requestClient 的泛型返回值即响应体;
 * - 错误形状是 {error:{code,message}},错误提示优先取 error.message;
 * - 后端无 /auth/refresh,enableRefreshToken 恒为 false,401 → 重新认证
 *   (loginExpiredMode 'page' → 清会话回登录页,见 guard/authStore)。
 */
import type { RequestClientOptions } from '@vben/request';

import { useAppConfig } from '@vben/hooks';
import { preferences } from '@vben/preferences';
import {
  authenticateResponseInterceptor,
  defaultResponseInterceptor,
  errorMessageResponseInterceptor,
  RequestClient,
} from '@vben/request';
import { useAccessStore } from '@vben/stores';

import { message } from 'ant-design-vue';

import { useAuthStore } from '#/store';

const { apiURL } = useAppConfig(import.meta.env, import.meta.env.PROD);

function createRequestClient(baseURL: string, options?: RequestClientOptions) {
  const client = new RequestClient({
    ...options,
    baseURL,
  });

  /**
   * 重新认证逻辑:清空会话状态;已生成过动态路由(在布局内)时按偏好跳登录页
   */
  async function doReAuthenticate() {
    console.warn('Access token is invalid or expired. ');
    const accessStore = useAccessStore();
    const authStore = useAuthStore();
    accessStore.setAccessToken(null);
    if (
      preferences.app.loginExpiredMode === 'modal' &&
      accessStore.isAccessChecked
    ) {
      accessStore.setLoginExpired(true);
    } else {
      await authStore.logout();
    }
  }

  function formatToken(token: null | string) {
    return token ? `Bearer ${token}` : null;
  }

  // 请求头处理
  client.addRequestInterceptor({
    fulfilled: async (config) => {
      const accessStore = useAccessStore();

      config.headers.Authorization = formatToken(accessStore.accessToken);
      config.headers['Accept-Language'] = preferences.app.locale;
      return config;
    },
  });

  // 处理返回的响应数据格式:'body' 分支直接返回响应体(裸载荷契约),
  // codeField/successCode 仅在 responseReturn 非 body/raw 时才参与。
  client.addResponseInterceptor(
    defaultResponseInterceptor({
      codeField: 'code',
      dataField: 'data',
      successCode: 0,
    }),
  );

  // token过期的处理
  client.addResponseInterceptor(
    authenticateResponseInterceptor({
      client,
      doReAuthenticate,
      doRefreshToken: async () => '',
      enableRefreshToken: false,
      formatToken,
    }),
  );

  // 通用的错误处理,如果没有进入上面的错误处理逻辑,就会进入这里
  client.addResponseInterceptor(
    errorMessageResponseInterceptor((msg: string, error) => {
      // 本仓管理 API 错误形状:{"error":{"code","message"}}
      const responseData = error?.response?.data ?? {};
      const errorMessage = responseData?.error?.message ?? '';
      // 如果没有错误信息,则会根据状态码进行提示
      message.error(errorMessage || msg);
    }),
  );

  return client;
}

export const requestClient = createRequestClient(apiURL, {
  responseReturn: 'body',
});

export const baseRequestClient = new RequestClient({ baseURL: apiURL });
