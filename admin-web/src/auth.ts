/**
 * admin-web 的会话状态:本地保存的 JWT、当前身份、初始化进度。
 * 路由守卫与视图都从这里取状态,模块级单例保证全局一致。
 */
import { computed, ref } from 'vue'

import { ApiClient, ApiError, UnauthorizedError, type SessionInfo } from '@infinitechance/api'

const TOKEN_KEY = 'infinitechance.admin.token'

function loadToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY)
  } catch {
    return null
  }
}

function persistToken(token: string | null): void {
  try {
    if (token === null) {
      localStorage.removeItem(TOKEN_KEY)
    } else {
      localStorage.setItem(TOKEN_KEY, token)
    }
  } catch {
    // 隐私模式下 localStorage 可能不可用;会话退化为仅存内存。
  }
}

const token = ref<string | null>(loadToken())
const username = ref<string | null>(null)
const initialized = ref<boolean | null>(null) // null = 尚未确认

const client = new ApiClient({ base: '/api', getToken: () => token.value })

// 画布服务的客户端(dev 经 vite 代理 /canvas-api → :8081):素材库等
// 挂在 canvas/server 的管理面走这里,与网关的管理 API 同一套 JWT 会话。
const canvasClient = new ApiClient({ base: '/canvas-api', getToken: () => token.value })

const isAuthenticated = computed(() => token.value !== null && username.value !== null)

let bootPromise: Promise<void> | null = null

/**
 * 校验本地令牌并确认管理员是否已初始化;只跑一次,后续调用复用。
 * 令牌失效只清理会话,网络故障向上抛给调用方处理。
 * 引导失败时清空缓存,后端恢复后的下一次导航可以重试。
 */
function ensureBooted(): Promise<void> {
  bootPromise ??= Promise.all([
    token.value
      ? client
          .me()
          .then((me) => {
            username.value = me.username
          })
          .catch((e: unknown) => {
            if (e instanceof UnauthorizedError) {
              clearSession()
            } else {
              throw e
            }
          })
      : Promise.resolve(),
    client
      .authStatus()
      .then((status) => {
        initialized.value = status.initialized
      })
      .catch((e: unknown) => {
        // 状态未知时守卫按「未知」处理,登录页引导重试。
        initialized.value = null
        throw e
      }),
  ]).then(() => undefined)

  // 失败的引导不缓存:允许重试,避免一次后端故障永久卡死路由守卫。
  bootPromise.catch(() => {
    bootPromise = null
  })
  return bootPromise
}

function saveSession(session: SessionInfo): void {
  persistToken(session.token)
  token.value = session.token
  username.value = session.username
}

function clearSession(): void {
  persistToken(null)
  token.value = null
  username.value = null
}

async function login(user: string, password: string): Promise<void> {
  saveSession(await client.login(user, password))
}

async function initAdmin(user: string, password: string): Promise<void> {
  saveSession(await client.initAdmin(user, password))
}

/** 认证视图共用的错误文案:后端错误透传,其余按连接失败提示。 */
export function authErrorMessage(e: unknown): string {
  return e instanceof ApiError ? e.message : '无法连接服务,请确认后端已启动'
}

export function useAuth() {
  return {
    client,
    canvasClient,
    token,
    username,
    initialized,
    isAuthenticated,
    ensureBooted,
    login,
    initAdmin,
    clearSession,
  }
}
