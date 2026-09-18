import type { AssetPage } from '@/features/cmdb/types'

export type Template={id:string;name:string;category:string;description:string;command:string;playbookId?:string;parameters?:Record<string,string>;version:number;risk:'low'|'medium'|'high';lastRun:string;enabled:boolean;updatedAt?:string;updatedBy?:string}
export type Playbook={id:string;name:string;description:string;content:string;constants:Record<string,string>;variables:Record<string,string>;enabled:boolean;version:number;createdBy:string;updatedAt?:string}
export type PlaybookInput=Omit<Playbook,'id'|'createdBy'|'version'|'updatedAt'>
export type JobLog={time:string;level:string;message:string}
export type JobTargetResult={target:string;assetId?:string;assetName?:string;status:string;rc:number;changed:boolean;stdout?:string;stderr?:string;error?:string;durationMs:number}
export type Job={id:string;templateId:string;templateVersion:number;name:string;targets:string[];status:string;progress:number;operator:string;startedAt:string;duration:string;logs:JobLog[];timeoutSeconds:number;attempt:number;approvedBy:string;playbookId:string;playbookVersion:number;variables:Record<string,string>;parameters?:Record<string,string>;targetResults?:JobTargetResult[];artifactKeys?:string[];credentialId:string}
export type Schedule={id:string;name:string;templateId:string;templateName:string;targets:string[];cron:string;timeoutSeconds:number;enabled:boolean;nextRun:string;lastRun:string;lastStatus:string;lastJobId:string;createdBy:string;credentialId:string}
export type ScheduleInput={name:string;templateId:string;targets:string[];cron:string;timeoutSeconds:number;enabled:boolean;credentialId:string}
export type Summary={templates:number;running:number;successToday:number;failedToday:number;schedules:number;playbooks:number}
export type ExecutorInfo={mode:string;ready:boolean;endpoint:string}
export type JobTargetOptions={projectGroups:string[];tags:string[]}
export type JobTargetResolve={targets:string[];assets:AssetPage['data']}

async function request<T>(path:string,token:string,init?:RequestInit):Promise<T>{const response=await fetch(path,{...init,headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json',...(init?.headers||{})}});const data=await response.json().catch(()=>({message:'请求失败'}));if(!response.ok)throw new Error((data as {message?:string}).message||`请求失败 (${response.status})`);return data as T}
export const fetchJobData=(token:string)=>Promise.all([request<Summary>('/api/v1/jobs/summary',token),request<Template[]>('/api/v1/jobs/templates',token),request<Job[]>('/api/v1/jobs/executions',token),request<Schedule[]>('/api/v1/jobs/schedules',token),request<Playbook[]>('/api/v1/jobs/playbooks',token),request<ExecutorInfo>('/api/v1/jobs/executor',token)])
export const fetchJobAssets=(token:string)=>request<AssetPage>('/api/v1/cmdb/assets?page=1&pageSize=100',token)
export type TemplateInput=Omit<Template,'id'|'lastRun'|'enabled'|'version'|'updatedAt'|'updatedBy'>
export const createTemplate=(token:string,input:TemplateInput)=>request<Template>('/api/v1/jobs/templates',token,{method:'POST',body:JSON.stringify(input)})
export const updateTemplate=(token:string,id:string,input:TemplateInput)=>request<Template>(`/api/v1/jobs/templates/${id}`,token,{method:'PUT',body:JSON.stringify(input)})
export const duplicateTemplate=(token:string,id:string)=>request<Template>(`/api/v1/jobs/templates/${id}/duplicate`,token,{method:'POST'})
export const deleteTemplate=(token:string,id:string)=>request<void>(`/api/v1/jobs/templates/${id}`,token,{method:'DELETE'})
export const toggleTemplate=(token:string,id:string)=>request<Template>(`/api/v1/jobs/templates/${id}/toggle`,token,{method:'POST'})
export const createExecution=(token:string,input:{templateId:string;targets:string[];parameters?:Record<string,string>;timeoutSeconds:number;credentialId:string})=>request<Job>('/api/v1/jobs/executions',token,{method:'POST',body:JSON.stringify(input)})
export const createPlaybookExecution=(token:string,input:{playbookId:string;targets:string[];variables:Record<string,string>;parameters?:Record<string,string>;timeoutSeconds:number;credentialId:string})=>request<Job>('/api/v1/jobs/executions',token,{method:'POST',body:JSON.stringify(input)})
export const jobAction=(token:string,id:string,action:'run'|'retry'|'cancel'|'approve')=>request<Job>(`/api/v1/jobs/executions/${id}/${action}`,token,{method:'POST'})
export const createPlaybook=(token:string,input:PlaybookInput)=>request<Playbook>('/api/v1/jobs/playbooks',token,{method:'POST',body:JSON.stringify(input)})
export const updatePlaybook=(token:string,id:string,input:PlaybookInput)=>request<Playbook>(`/api/v1/jobs/playbooks/${id}`,token,{method:'PUT',body:JSON.stringify(input)})
export const togglePlaybook=(token:string,id:string)=>request<Playbook>(`/api/v1/jobs/playbooks/${id}/toggle`,token,{method:'POST'})
export const deletePlaybook=(token:string,id:string)=>request<void>(`/api/v1/jobs/playbooks/${id}`,token,{method:'DELETE'})
export const createSchedule=(token:string,input:ScheduleInput)=>request<Schedule>('/api/v1/jobs/schedules',token,{method:'POST',body:JSON.stringify(input)})
export const updateSchedule=(token:string,id:string,input:ScheduleInput)=>request<Schedule>(`/api/v1/jobs/schedules/${id}`,token,{method:'PUT',body:JSON.stringify(input)})
export const toggleSchedule=(token:string,id:string)=>request<Schedule>(`/api/v1/jobs/schedules/${id}/toggle`,token,{method:'POST'})
export const runSchedule=(token:string,id:string)=>request<Job>(`/api/v1/jobs/schedules/${id}/run`,token,{method:'POST'})
export const deleteSchedule=(token:string,id:string)=>request<void>(`/api/v1/jobs/schedules/${id}`,token,{method:'DELETE'})
export const fetchJobTargetOptions=(token:string)=>request<JobTargetOptions>('/api/v1/jobs/target-options',token)
export const resolveJobTargets=(token:string,input:{projectGroups:string[];tags:string[];assetIds:string[];ips:string[]})=>request<JobTargetResolve>('/api/v1/jobs/targets/resolve',token,{method:'POST',body:JSON.stringify(input)})
