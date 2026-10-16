import Keycloak from 'keycloak-js'
import { runtimeConfig } from './runtime'

const config = runtimeConfig()
if (!config.keycloakUrl) {
  console.error('Keycloak runtime configuration is missing')
}

export const keycloak = new Keycloak({
  url: config.keycloakUrl || window.location.origin,
  realm: config.realm,
  clientId: config.clientId,
})

// 会话保存在 sessionStorage（仅当前标签页）。
// 之前的实现每次加载都要和 Keycloak 做一次整页往返来判定登录态，一旦这次往返失败
// （HTTP/2 连接被重置、Keycloak 短暂不可用等），用户就被判定为未登录并跳回登录页。
// 现在同一标签页刷新/切回时直接用本地会话恢复，不再依赖那次往返。
const STORAGE_KEY = 'cmdb.session'
const INIT_TIMEOUT_MS = 12000

export class SessionExpiredError extends Error {
  constructor(message = 'session expired') {
    super(message)
    this.name = 'SessionExpiredError'
  }
}

type StoredSession = { token: string; refreshToken: string; idToken?: string }

function decodeExpiry(token?: string): number {
  if (!token) return 0
  try {
    const payload = token.split('.')[1]
    if (!payload) return 0
    const normalized = payload.replace(/-/g, '+').replace(/_/g, '/')
    const padded = normalized + '='.repeat((4 - (normalized.length % 4)) % 4)
    const bytes = Uint8Array.from(window.atob(padded), character => character.charCodeAt(0))
    const parsed = JSON.parse(new TextDecoder().decode(bytes)) as { exp?: number }
    return typeof parsed.exp === 'number' ? parsed.exp : 0
  } catch {
    return 0
  }
}

function readStoredSession(): StoredSession | undefined {
  try {
    const raw = window.sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return undefined
    const parsed = JSON.parse(raw) as StoredSession
    if (!parsed?.token || !parsed?.refreshToken) return undefined
    const refreshExpiry = decodeExpiry(parsed.refreshToken)
    // 刷新令牌本身都过期了就没有恢复的意义
    if (refreshExpiry && refreshExpiry * 1000 <= Date.now()) return undefined
    return parsed
  } catch {
    return undefined
  }
}

export const persistSession = () => {
  try {
    if (!keycloak.token || !keycloak.refreshToken) return
    window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify({
      token: keycloak.token,
      refreshToken: keycloak.refreshToken,
      idToken: keycloak.idToken,
    }))
  } catch {
    // 隐私模式或存储被禁用时忽略：退化成内存会话
  }
}

export const clearStoredSession = () => {
  try {
    window.sessionStorage.removeItem(STORAGE_KEY)
  } catch {
    // ignore
  }
}

// Keycloak 回跳时会把 state/session_state/code 放在 hash 里，处理完必须清掉，
// 否则地址栏会残留一个无效的授权回调，刷新后再次触发失败流程。
export const clearAuthFragment = () => {
  const hash = window.location.hash
  if (!hash) return
  if (!/(^|[#&])(state|code|error|session_state|iss)=/.test(hash)) return
  window.history.replaceState(null, '', window.location.pathname + window.location.search)
}

export const initializeKeycloak = async () => {
  const stored = readStoredSession()
  const options = {
    onLoad: 'check-sso' as const,
    pkceMethod: 'S256' as const,
    checkLoginIframe: false,
    // 无本地会话时用隐藏 iframe 做 SSO 校验，避免整页跳转后再回来
    // （页面白屏 + 失败的往返正是"莫名回到登录页"的来源）；iframe 被浏览器
    // 拦截时会自动回退到整页跳转。
    silentCheckSsoRedirectUri: window.location.origin + '/silent-check-sso.html',
    silentCheckSsoFallback: true,
    timeSkew: 30,
    token: stored?.token,
    refreshToken: stored?.refreshToken,
    idToken: stored?.idToken,
  }
  try {
    // 带上本地令牌时 keycloak-js 走一次静默的令牌刷新，不会做整页跳转；
    // 刷新失败才回退到 check-sso 的 SSO 校验流程。
    await Promise.race([
      keycloak.init(options),
      new Promise(resolve => window.setTimeout(resolve, INIT_TIMEOUT_MS)),
    ])
  } catch (error) {
    console.warn('[auth] Keycloak 初始化失败，按未登录处理', error)
  } finally {
    clearAuthFragment()
  }
  return Boolean(keycloak.authenticated)
}

export const refreshAccessToken = async (minValidity = 60) => {
  if (!keycloak.authenticated) throw new SessionExpiredError('not authenticated')
  try {
    await keycloak.updateToken(minValidity)
  } catch (error) {
    // keycloak-js 只在刷新令牌被服务端明确拒绝（invalid_grant）时才清空会话；
    // 其余情况（网络抖动、Keycloak 短暂不可用）保留会话，交给调用方重试。
    if (!keycloak.authenticated || !keycloak.refreshToken) {
      throw new SessionExpiredError(String(error))
    }
    throw error
  }
  persistSession()
  return keycloak.token || ''
}

export const configureTokenLifecycle = (onToken: (token: string) => void, onExpired: () => void) => {
  keycloak.onTokenExpired = () => {
    void refreshAccessToken(30)
      .then(onToken)
      .catch(error => {
        // 瞬时错误不登出，交给定时刷新继续重试
        if (error instanceof SessionExpiredError) onExpired()
      })
  }
  keycloak.onAuthRefreshSuccess = () => persistSession()
  keycloak.onAuthLogout = () => clearStoredSession()
}

export const signOutKeycloak = async () => {
  clearStoredSession()
  await clearAuthFragment()
  await keycloak.logout({ redirectUri: window.location.origin + '/login' })
}
