import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import * as authApi from '@/api/auth'
import type { ModuleEntry, UserProfile } from '@/features/auth/types'
import { initializeKeycloak, keycloak, refreshAccessToken } from '@/auth/keycloak'

export const useAuthStore=defineStore('auth',()=>{
  const token=ref(''); const user=ref<UserProfile>(); const modules=ref<ModuleEntry[]>([]); const isReady=ref(false); const isAuthenticated=computed(()=>Boolean(token.value&&user.value))
  async function signIn(){ await keycloak.login({redirectUri:window.location.origin+'/'}) }
  async function initialize(){try{if(await initializeKeycloak()){token.value=await refreshAccessToken();[user.value,modules.value]=await Promise.all([authApi.currentUser(token.value),authApi.fetchModules(token.value)]);window.setInterval(async()=>{token.value=await refreshAccessToken()},20000)}}catch{clear()}finally{isReady.value=true}}
  async function signOut(){clear();await keycloak.logout({redirectUri:window.location.origin+'/login'})}
  function clear(){token.value='';user.value=undefined;modules.value=[]}
  return{token,user,modules,isReady,isAuthenticated,signIn,signOut,initialize}
})
