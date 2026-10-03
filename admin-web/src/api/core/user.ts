import type { UserInfo } from '@vben/types';

import type { AdminIdentity } from '@infinitechance/api';

import { requestClient } from '#/api/request';

/**
 * 获取用户信息(GET /auth/me),映射成 vben UserInfo
 */
export async function getUserInfoApi() {
  const identity = await requestClient.get<AdminIdentity>('/auth/me');
  const userInfo: UserInfo = {
    avatar: '',
    desc: '单管理员',
    homePath: '/dashboard',
    realName: identity.username,
    roles: ['admin'],
    token: '',
    userId: identity.username,
    username: identity.username,
  };
  return userInfo;
}
