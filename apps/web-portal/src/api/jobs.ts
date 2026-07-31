import type { AssetPage } from '@/features/cmdb/types'

export type Template={id:string;name:string;category:string;description:string;command:string;risk:'low'|'medium'|'high';lastRun:string;enabled:boolean}
export type JobLog={time:string;level:string;message:string}
export type Job={id:string;templateId:string;name:string;targets:string[];status:string;progress:number;operator:string;startedAt:string;duration:string;logs:JobLog[];timeoutSeconds:number;attempt:number;approvedBy:string}
export type Schedule={id:string;name:string;cron:string;nextRun:string;enabled:boolean}
export type Summary={templates:number;running:number;successToday:number;failedToday:number;schedules:number}

async function request<T>(path:string,token:string,init?:RequestInit):Promise<T>{const response=await fetch(path,{...init,headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json',...(init?.headers||{})}});const data=await response.json().catch(()=>({message:'请求失败'}));if(!response.ok)throw new Error((data as {message?:string}).message||`请求失败 (${response.status})`);return data as T}
export const fetchJobData=(token:string)=>Promise.all([request<Summary>('/api/v1/jobs/summary',token),request<Template[]>('/api/v1/jobs/templates',token),request<Job[]>('/api/v1/jobs/executions',token),request<Schedule[]>('/api/v1/jobs/schedules',token)])
export const fetchJobAssets=(token:string)=>request<AssetPage>('/api/v1/cmdb/assets?page=1&pageSize=100',token)
export const createTemplate=(token:string,input:Omit<Template,'id'|'lastRun'|'enabled'>)=>request<Template>('/api/v1/jobs/templates',token,{method:'POST',body:JSON.stringify(input)})
export const toggleTemplate=(token:string,id:string)=>request<Template>(`/api/v1/jobs/templates/${id}/toggle`,token,{method:'POST'})
export const createExecution=(token:string,input:{templateId:string;targets:string[];timeoutSeconds:number})=>request<Job>('/api/v1/jobs/executions',token,{method:'POST',body:JSON.stringify(input)})
export const jobAction=(token:string,id:string,action:'run'|'retry'|'cancel'|'approve')=>request<Job>(`/api/v1/jobs/executions/${id}/${action}`,token,{method:'POST'})
