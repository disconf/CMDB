import type { Analytics,Asset,AssetFilters,AssetInput,AssetPage,AssetSummary,CiModel,HistoryEntry,ImportResult } from '@/features/cmdb/types'
import { buildAssetQuery } from '@/features/cmdb/cmdbModel'
async function get<T>(path:string,token:string):Promise<T>{const response=await fetch(path,{headers:{Authorization:`Bearer ${token}`}});if(!response.ok)throw new Error(response.status===401?'登录状态已失效':'CMDB 数据请求失败');return response.json() as Promise<T>}
export const fetchModels=(token:string)=>get<CiModel[]>('/api/v1/cmdb/models',token)
export const fetchSummary=(token:string)=>get<AssetSummary>('/api/v1/cmdb/summary',token)
export const fetchAssets=(token:string,filters:AssetFilters)=>get<AssetPage>(`/api/v1/cmdb/assets?${buildAssetQuery(filters)}`,token)
export const fetchAsset=(token:string,id:string)=>get<Asset>(`/api/v1/cmdb/assets/${encodeURIComponent(id)}`,token)
async function send<T>(path:string,token:string,method:string,body:unknown):Promise<T>{const response=await fetch(path,{method,headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},body:JSON.stringify(body)});const data=await response.json().catch(()=>({message:'请求失败'}));if(!response.ok)throw new Error((data as {message?:string}).message||'保存失败');return data as T}
export const createAsset=(token:string,input:AssetInput)=>send<Asset>('/api/v1/cmdb/assets',token,'POST',input)
export const updateAsset=(token:string,id:string,input:Omit<AssetInput,'id'|'type'|'source'>)=>send<Asset>(`/api/v1/cmdb/assets/${encodeURIComponent(id)}`,token,'PATCH',input)
export const fetchHistory=(token:string,id:string)=>get<HistoryEntry[]>(`/api/v1/cmdb/assets/${encodeURIComponent(id)}/history`,token)
export const importAssets=(token:string,rows:AssetInput[],upsert=false)=>send<ImportResult>(`/api/v1/cmdb/imports?mode=${upsert?'upsert':'create'}`,token,'POST',rows)
export const fetchAnalytics=(token:string)=>get<Analytics>('/api/v1/cmdb/analytics',token)
export const fetchModelTemplate=(token:string,code:string)=>fetch(`/api/v1/cmdb/models/${encodeURIComponent(code)}/template`,{headers:{Authorization:`Bearer ${token}`}}).then(async(r)=>{if(!r.ok)throw new Error('模板获取失败');return r.text()})
export const createModel=(token:string,input:Omit<CiModel,'count'|'enabled'>)=>send<CiModel>('/api/v1/cmdb/models',token,'POST',input)
export const updateModel=(token:string,code:string,input:Omit<CiModel,'count'|'enabled'>)=>send<CiModel>(`/api/v1/cmdb/models/${encodeURIComponent(code)}`,token,'PUT',input)
export const toggleModel=(token:string,code:string)=>send<CiModel>(`/api/v1/cmdb/models/${encodeURIComponent(code)}/toggle`,token,'POST',{})
