import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import * as authApi from '@/api/auth'
import type { ModuleEntry, UserProfile } from '@/features/auth/types'
import {
  SessionExpiredError,
  clearAuthFragment,
  clearStoredSession,
  configureTokenLifecycle,
  initializeKeycloak,
  keycloak,
  refreshAccessToken,
  signOutKeycloak,
} from '@/auth/keycloak'

const REFRESH_INTERVAL_MS = 30000
const MAX_TRANSIENT_RETRIES = 3

export const useAuthStore=defineStore('auth',()=>{
  const token=ref('')
  const user=ref<UserProfile>()
  const modules=ref<ModuleEntry[]>([])
  const isReady=ref(false)
  const sessionNotice=ref('')
  const isAuthenticated=computed(()=>Boolean(token.value&&user.value))
  let refreshTimer:number|undefined
  let refreshing=false
  let lifecycleBound=false

  async function loadProfile(nextToken:string){
    token.value=nextToken
    const [profile, entries]=await Promise.all([authApi.currentUser(nextToken),authApi.fetchModules(nextToken)])
    user.value=profile
    modules.value=entries
  }

  // 只有会话被服务端明确判定失效时才清空登录态。
  // 网络抖动、Keycloak 短暂不可用等瞬时问题一律重试，绝不把用户踢回登录页。
  function expire(notice:string){
    sessionNotice.value=notice
    clearStoredSession()
    clear()
  }

  async function refreshSession(attempt=0){
    if(refreshing) return
    refreshing=true
    try{
      const next=await refreshAccessToken(90)
      if(!next){expire('登录状态已过期，请重新登录');return}
      token.value=next
      if(!user.value||!modules.value.length) await loadProfile(next)
      sessionNotice.value=''
    }catch(error){
      if(error instanceof SessionExpiredError){expire('登录状态已过期，请重新登录');return}
      if(attempt<MAX_TRANSIENT_RETRIES){
        const delay=1500*(attempt+1)
        console.warn(`[auth] 会话刷新失败，${delay}ms 后重试（第 ${attempt+1} 次）`,error)
        window.setTimeout(()=>{refreshing=false;void refreshSession(attempt+1)},delay)
        return
      }
      console.warn('[auth] 会话刷新连续失败，保留当前会话并等待下次重试',error)
    }finally{refreshing=false}
  }

  function bindLifecycle(){
    if(lifecycleBound) return
    lifecycleBound=true
    configureTokenLifecycle(
      next=>{if(next)token.value=next},
      ()=>expire('登录状态已过期，请重新登录'),
    )
    document.addEventListener('visibilitychange',()=>{if(document.visibilityState==='visible')void refreshSession()})
    window.addEventListener('focus',()=>void refreshSession())
    refreshTimer=window.setInterval(()=>void refreshSession(),REFRESH_INTERVAL_MS)
  }

  async function signIn(){
    sessionNotice.value=''
    clearAuthFragment()
    const loginUrl=await keycloak.createLoginUrl({redirectUri:window.location.origin+'/'})
    window.location.assign(loginUrl)
  }

  async function initialize(){
    try{
      if(await initializeKeycloak()){
        const next=await refreshAccessToken(90)
        if(next){
          await loadProfile(next)
          clearAuthFragment()
          bindLifecycle()
        }else{
          expire('登录状态已过期，请重新登录')
        }
      }
    }catch(error){
      if(error instanceof SessionExpiredError) expire('登录状态已过期，请重新登录')
      else console.warn('[auth] 初始化会话失败',error)
    }finally{isReady.value=true}
  }

  async function signOut(){
    clear()
    sessionNotice.value=''
    if(refreshTimer){clearInterval(refreshTimer);refreshTimer=undefined}
    await signOutKeycloak()
  }

  function clear(){token.value='';user.value=undefined;modules.value=[]}
  return{token,user,modules,isReady,isAuthenticated,sessionNotice,signIn,signOut,initialize,refreshSession}
})
