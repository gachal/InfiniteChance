/**
 * 35 号票后 auth.ts 不再持有会话:token 由 vben access store 持久化,
 * 登录/登出/401 全走 vben access 机制(store/auth.ts + 路由守卫)。
 * 本文件退化为 API 客户端访问器,给业务视图原样保留的
 * `useAuth().client / canvasClient` 提供同一形状;模型价格页走本应用的
 * requestClient(36 号票,@infinitechance/api 按票定案不扩展)。
 */
import { ApiClient, ApiError } from '@infinitechance/api'

import { useAccessStore } from '@vben/stores'

// token 统一读 vben access store(持久化由其接管)
const readToken = (): null | string => useAccessStore().accessToken ?? null

// 网关管理面客户端(dev 经 vite 代理 /api → :8080;部署为 nginx/desktop 同契约)
const client = new ApiClient({ base: '/api', getToken: readToken })

// 画布服务的客户端(/canvas-api → :8081):素材库等管理面走这里,
// 与网关的管理 API 同一套 JWT 会话。
const canvasClient = new ApiClient({ base: '/canvas-api', getToken: readToken })

/** 认证视图共用的错误文案:后端错误透传,其余按连接失败提示。 */
export function authErrorMessage(e: unknown): string {
  return e instanceof ApiError ? e.message : '无法连接服务,请确认后端已启动'
}

export function useAuth() {
  return {
    canvasClient,
    client,
  }
}
