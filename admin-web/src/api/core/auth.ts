import type { SessionInfo } from '@infinitechance/api';

import { baseRequestClient, requestClient } from '#/api/request';

/** 登录/初始化参数 */
export interface LoginParams {
  password?: string;
  username?: string;
}

/** 后端会话载荷:token + 用户名(SessionInfo) */
export type LoginResult = SessionInfo;

/** GET /auth/status 响应 */
export interface AuthStatus {
  initialized: boolean;
}

/**
 * 登录(POST /auth/login)
 */
export async function loginApi(data: LoginParams) {
  return requestClient.post<LoginResult>('/auth/login', data);
}

/**
 * 首管理员初始化(POST /auth/init,成功即登录)
 */
export async function initAdminApi(data: LoginParams) {
  return requestClient.post<LoginResult>('/auth/init', data);
}

/**
 * 初始化状态(GET /auth/status):守卫引导用,不经 token 拦截。
 * baseRequestClient 是裸 axios(无响应拦截器),responseReturn 不生效,
 * 运行时拿到 AxiosResponse,这里手动解包。
 */
export async function getAuthStatusApi() {
  const resp = await baseRequestClient.get<unknown>('/auth/status');
  return (resp as { data: AuthStatus }).data;
}

/**
 * 权限码:单管理员无角色体系,固定空表
 */
export async function getAccessCodesApi() {
  return [] as string[];
}
