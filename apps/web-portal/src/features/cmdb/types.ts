export interface ModelField {name:string;label:string;type:'text'|'number'|'boolean'|'date'|'select';required:boolean;defaultValue:string;options?:string[]}
export interface CiModel { code:string;name:string;category:string;icon:string;count:number;description:string;enabled:boolean;fields:ModelField[] }
export interface Attribute { name:string;label:string;value:string }
export interface Relation { type:string;targetId:string;targetName:string }
export interface Asset { id:string;name:string;type:string;typeName:string;status:string;ip:string;environment:string;projectGroup:string;owner:string;location:string;source:string;lastSeenAt:string;tags:string[];attributes:Attribute[];relations:Relation[] }
export interface AssetSummary { total:number;online:number;warning:number;offline:number;models:number;projectGroups:number }
export interface AssetFilters { search:string;type:string;status:string;projectGroup:string;page:number;pageSize:number }
export interface AssetPage { data:Asset[];meta:{total:number;page:number;pageSize:number;totalPages:number} }
export interface AssetInput { id:string;name:string;type:string;status:string;ip:string;environment:string;projectGroup:string;owner:string;location:string;source:string;tags:string[];attributes?:Attribute[];relations?:Relation[] }
export interface HistoryEntry { id:string;assetId:string;action:string;operator:string;occurredAt:string;changes:Array<{field:string;before:string;after:string}> }
export interface ImportResult { total:number;created:number;updated:number;errors:Array<{row:number;id:string;message:string}> }

export interface Analytics { status:{total:number;online:number;warning:number;offline:number}; byType:Array<{code:string;name:string;count:number}>; byGroup:Array<{group:string;count:number}>; bySource:Array<{source:string;count:number}>; completeness:Array<{field:string;label:string;missing:number;total:number;rate:number}> }

export interface IdcRack { id:string;moduleId:string;name:string;uTotal:number;voltage:string;occupiedU?:string[] }
export interface IdcModule { id:string;roomId:string;name:string;racks:IdcRack[] }
export interface IdcRoom { id:string;name:string;location:string;modules:IdcModule[] }

export interface NetworkARPEntry { ip:string;macAddress?:string;vlan?:string;interfaceIndex?:string;assetId?:string;assetName?:string;state?:string;lastSeenAt?:string }
export interface NetworkPort { id:string;assetId:string;assetName:string;ifIndex:string;name:string;description?:string;alias?:string;adminStatus:string;operStatus:string;speedMbps?:string;macAddress?:string;macAddresses?:string[];arpEntries?:NetworkARPEntry[];vlan?:string;neighborName?:string;neighborPort?:string;neighborChassis?:string;lastSeenAt?:string }
export interface NetworkLink { id:string;sourceAssetId:string;sourceAssetName:string;sourceInterface:string;targetAssetId:string;targetAssetName:string;targetPort:string;status:string;source:string;verification:string;confidence:number;evidence?:string[];lastVerifiedAt?:string;lastSeenAt?:string }
export interface NetworkInterface { id:string;assetId:string;assetName:string;name:string;macAddress?:string;ipv4?:string;mtu?:string;speedMbps?:string;adminStatus:string;operStatus:string;type?:string;lastSeenAt?:string;neighbors?:NetworkARPEntry[];link?:NetworkLink }
export interface NetworkDeviceInventory { asset:Asset;ports:NetworkPort[];totalPorts:number;upPorts:number }
export interface PhysicalNode { id:string;name:string;kind:string;assetType:string;status:string;ip?:string;location?:string;owner?:string;portCount:number;upPortCount:number }
export interface PhysicalEdge { id:string;source:string;target:string;sourceInterface?:string;targetPort?:string;relation:string;status:string;sourceType:string;verification:string;confidence:number;evidence?:string[];lastSeenAt?:string }
export interface PhysicalGraph { nodes:PhysicalNode[];edges:PhysicalEdge[];updatedAt:string }

export interface TopologySnapshot { id:string;kind:string;environment:string;nodeCount:number;edgeCount:number;payload?:unknown;operator:string;createdAt:string }
export interface NetworkLinkHistoryEntry { id:string;assetId:string;assetName:string;action:string;operator:string;occurredAt:string;changes:Array<{field:string;before:string;after:string}> }
export interface VLANMember { deviceId:string;deviceName:string;portName:string;ifIndex:string;assetId?:string;assetName?:string;status:string;macAddresses?:string[];arpCount:number;neighborName?:string;lastSeenAt?:string }
export interface VLANInventory { vlan:string;memberCount:number;upMembers:number;conflictMembers:number;learnedMacCount:number;arpEntryCount:number;members:VLANMember[];updatedAt:string }
export interface NetworkAnomaly { id:string;type:string;severity:string;status:string;title:string;detail:string;assetId?:string;assetName?:string;deviceId?:string;deviceName?:string;portName?:string;vlan?:string;ip?:string;macAddress?:string;evidence?:string[];acknowledgedBy?:string;acknowledgedAt?:string;note?:string;detectedAt:string }
