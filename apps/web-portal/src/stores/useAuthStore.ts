import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import * as authApi from '@/api/auth'
import type { ModuleEntry, UserProfile } from '@/features/auth/types'
import { configureTokenLifecycle, initializeKeycloak, keycloak, refreshAccessToken } from '@/auth/keycloak'

export const useAuthStore=defineStore('auth',()=>{
  const token=ref('')
  const user=ref<UserProfile>()
  const modules=ref<ModuleEntry[]>([])
  const isReady=ref(false)
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

  async function refreshSession(){
    if(refreshing) return
    refreshing=true
    try{
      const next=await refreshAccessToken(90)
      if(!next){clear();return}
      token.value=next
      if(!user.value||!modules.value.length) await loadProfile(next)
    }catch{clear()}finally{refreshing=false}
  }

  function bindLifecycle(){
    if(lifecycleBound) return
    lifecycleBound=true
    configureTokenLifecycle(
      next=>{if(next)token.value=next},
      ()=>clear(),
    )
    document.addEventListener('visibilitychange',()=>{if(document.visibilityState==='visible')void refreshSession()})
    window.addEventListener('focus',()=>void refreshSession())
    refreshTimer=window.setInterval(()=>void refreshSession(),30000)
  }

  async function signIn(){
    if(window.location.hash) window.history.replaceState(null,'',window.location.pathname+window.location.search)
    const loginUrl=await keycloak.createLoginUrl({redirectUri:window.location.origin+'/'})
    window.location.assign(loginUrl)
  }
  async function initialize(){
    try{
      if(await initializeKeycloak()){
        const next=await refreshAccessToken(90)
        if(next){await loadProfile(next);bindLifecycle()}
      }
    }catch{clear()}finally{isReady.value=true}
  }
  async function signOut(){
    clear()
    if(refreshTimer){clearInterval(refreshTimer);refreshTimer=undefined}
    await keycloak.logout({redirectUri:window.location.origin+'/login'})
  }
  function clear(){token.value='';user.value=undefined;modules.value=[]}
  return{token,user,modules,isReady,isAuthenticated,signIn,signOut,initialize,refreshSession}
})