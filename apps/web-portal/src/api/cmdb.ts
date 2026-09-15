import type { Analytics,Asset,AssetFilters,AssetInput,AssetPage,AssetSummary,CiModel,HistoryEntry,IdcRoom,ImportResult,NetworkDeviceInventory,NetworkInterface,NetworkLink,NetworkPort,PhysicalGraph,TopologySnapshot,NetworkLinkHistoryEntry,VLANInventory,NetworkAnomaly } from '@/features/cmdb/types'
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
export const fetchIdcRooms=(token:string)=>get<IdcRoom[]>('/api/v1/idc/rooms',token)

async function del<T>(path:string,token:string):Promise<T>{const response=await fetch(path,{method:'DELETE',headers:{Authorization:`Bearer ${token}`}});if(!response.ok)throw new Error('删除失败');return undefined as unknown as T}
export const deleteIdcRack=(token:string,rackId:string)=>del<void>(`/api/v1/idc/racks/${encodeURIComponent(rackId)}`,token)
export const deleteIdcModule=(token:string,moduleId:string)=>del<void>(`/api/v1/idc/modules/${encodeURIComponent(moduleId)}`,token)

export const updateIdcRack=(token:string,rackId:string,payload:{name:string;uTotal:number;voltage:string})=>send<{id:string;moduleId:string;name:string;uTotal:number;voltage:string}>(`/api/v1/idc/racks/${encodeURIComponent(rackId)}`,token,'PATCH',payload)
export const updateIdcModule=(token:string,moduleId:string,name:string)=>send<{id:string;roomId:string;name:string}>(`/api/v1/idc/modules/${encodeURIComponent(moduleId)}`,token,'PATCH',{name})
export const deleteIdcRoom=(token:string,roomId:string)=>del<void>(`/api/v1/idc/rooms/${encodeURIComponent(roomId)}`,token)
export const createIdcRoom=(token:string,name:string,location:string)=>send<IdcRoom>('/api/v1/idc/rooms',token,'POST',{name,location})
export const addIdcModule=(token:string,roomId:string,name:string)=>send<{id:string;roomId:string;name:string;racks:never[]}>(`/api/v1/idc/rooms/${encodeURIComponent(roomId)}/modules`,token,'POST',{name})
export const addIdcRack=(token:string,moduleId:string,payload:{name:string;uTotal:number;voltage:string})=>send<{id:string;moduleId:string;name:string;uTotal:number;voltage:string}>(`/api/v1/idc/racks`,token,'POST',{moduleId,...payload})

export const createModel=(token:string,input:Omit<CiModel,'count'|'enabled'>)=>send<CiModel>('/api/v1/cmdb/models',token,'POST',input)
export const updateModel=(token:string,code:string,input:Omit<CiModel,'count'|'enabled'>)=>send<CiModel>(`/api/v1/cmdb/models/${encodeURIComponent(code)}`,token,'PUT',input)
export const toggleModel=(token:string,code:string)=>send<CiModel>(`/api/v1/cmdb/models/${encodeURIComponent(code)}/toggle`,token,'POST',{})

export interface Credential { id:string;name:string;kind:string;username:string;group:string;description:string }
export const listCredentials=(token:string)=>get<Credential[]>('/api/v1/credentials',token)
export const createCredential=(token:string,input:{name:string;kind:string;username:string;secret:string;group?:string;description?:string})=>send<Credential>('/api/v1/credentials',token,'POST',input)
export const deleteCredential=(token:string,id:string)=>del<void>(`/api/v1/credentials/${encodeURIComponent(id)}`,token)



export const fetchNetworkDevices=(token:string)=>get<NetworkDeviceInventory[]>('/api/v1/cmdb/network-devices',token)
export const fetchNetworkPorts=(token:string,assetId:string)=>get<NetworkPort[]>(`/api/v1/cmdb/network-devices/${encodeURIComponent(assetId)}/ports`,token)
export const fetchNetworkInterfaces=(token:string,assetId:string)=>get<NetworkInterface[]>(`/api/v1/cmdb/assets/${encodeURIComponent(assetId)}/interfaces`,token)
export const fetchNetworkLinks=(token:string)=>get<NetworkLink[]>('/api/v1/cmdb/network-links',token)
export const createNetworkLink=(token:string,input:{sourceAssetId:string;sourceInterface:string;targetAssetId:string;targetPort:string})=>send<NetworkLink>('/api/v1/cmdb/network-links',token,'POST',input)
export const deleteNetworkLink=(token:string,id:string)=>del<void>(`/api/v1/cmdb/network-links/${encodeURIComponent(id)}`,token)
export const fetchPhysicalTopology=(token:string,environment='')=>get<PhysicalGraph>(`/api/v1/topology/physical${environment?`?environment=${encodeURIComponent(environment)}`:''}`,token)
export const fetchNetworkLinkHistory=(token:string,limit=20)=>get<NetworkLinkHistoryEntry[]>(`/api/v1/cmdb/network-links/history?limit=${limit}`,token)
export const fetchTopologySnapshots=(token:string,kind='physical',environment='',limit=20)=>get<TopologySnapshot[]>(`/api/v1/topology/snapshots?kind=${encodeURIComponent(kind)}&environment=${encodeURIComponent(environment)}&limit=${limit}`,token)
export const fetchTopologySnapshot=(token:string,id:string)=>get<TopologySnapshot>(`/api/v1/topology/snapshots/${encodeURIComponent(id)}`,token)
export const captureTopologySnapshot=(token:string,kind='physical',environment='')=>send<TopologySnapshot>('/api/v1/topology/snapshots',token,'POST',{kind,environment})
export const fetchVLANInventory=(token:string)=>get<VLANInventory[]>('/api/v1/cmdb/vlans',token)
export const fetchNetworkAnomalies=(token:string)=>get<NetworkAnomaly[]>('/api/v1/cmdb/network-anomalies',token)
export const acknowledgeNetworkAnomaly=(token:string,id:string,note='')=>send<NetworkAnomaly>(`/api/v1/cmdb/network-anomalies/${encodeURIComponent(id)}/acknowledge`,token,'POST',{note})
export const resolveNetworkAnomaly=(token:string,id:string,note='')=>send<NetworkAnomaly>(`/api/v1/cmdb/network-anomalies/${encodeURIComponent(id)}/resolve`,token,'POST',{note})
export const createNetworkAnomalyTicket=(token:string,id:string)=>send<{ticket:unknown;created:boolean}>(`/api/v1/cmdb/network-anomalies/${encodeURIComponent(id)}/ticket`,token,'POST',{})
export const syncNetworkAnomalies=(token:string)=>send<{synced:number}>('/api/v1/cmdb/network-anomalies/sync-alerts',token,'POST',{})