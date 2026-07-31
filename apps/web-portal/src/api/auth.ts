import type { LoginSession, ModuleEntry, UserProfile } from '@/features/auth/types'

async function request<T>(path:string, options:RequestInit={}):Promise<T>{
  const response=await fetch(path,{...options,headers:{'Content-Type':'application/json',...options.headers}})
  if(!response.ok){ const body=await response.json().catch(()=>({message:'请求失败'})) as {message?:string}; throw new Error(body.message||`请求失败：${response.status}`) }
  if(response.status===204) return undefined as T
  return response.json() as Promise<T>
}
export const login=(username:string,password:string)=>request<LoginSession>('/api/v1/auth/login',{method:'POST',body:JSON.stringify({username,password})})
export const currentUser=(token:string)=>request<UserProfile>('/api/v1/auth/me',{headers:{Authorization:`Bearer ${token}`}})
export const fetchModules=(token:string)=>request<ModuleEntry[]>('/api/v1/auth/modules',{headers:{Authorization:`Bearer ${token}`}})
export const logout=(token:string)=>request<void>('/api/v1/auth/logout',{method:'POST',headers:{Authorization:`Bearer ${token}`}})
