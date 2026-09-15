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

export interface NetworkAutomationPolicy { id:string;name:string;enabled:boolean;priority:number;severity:string;anomalyType:string;environment:string;projectGroup:string;location:string;autoTicket:boolean;assignee:string;escalationMinutes:number;escalateTo:string;verificationTemplate:string;createdAt:string;updatedAt:string }
export interface NetworkAutomationLifecycle { anomalyId:string;policyId:string;status:string;title?:string;severity?:string;target?:string;firstSeenAt:string;lastSeenAt:string;resolvedAt?:string;ticketId?:string;escalatedAt?:string;verificationJobId?:string }
export interface NetworkAutomationReport { evaluated:number;policies:number;autoTickets:number;escalated:number;resolved:number;verificationJobs:number;alertsSynced:number;errors?:string[] }

export interface NetworkDiffSummary { added:number;removed:number;changed:number;unknownAccess:number }
export interface NetworkDiffChange { kind:string;action:string;id:string;name:string;detail:string;before?:string;after?:string }
export interface NetworkDiff { from:string;to:string;generatedAt:string;summary:NetworkDiffSummary;changes:NetworkDiffChange[] }

export interface ComplianceRule { id:string;name:string;action:string;type:string;value:string;environment:string;projectGroup:string;location:string;vlan:string;status:string;description:string;expiresAt?:string;reminderHours:number;lastRemindedAt?:string;createdBy:string;approvedBy?:string;createdAt:string;updatedAt:string }
export interface ComplianceDecision { id:string;anomalyId:string;ruleId?:string;ruleName?:string;status:string;reason:string;title:string;ip?:string;macAddress?:string;vlan?:string;environment?:string;projectGroup?:string;location?:string;firstSeenAt:string;expiresAt?:string }

export interface OnboardingRequest { id:string;anomalyId:string;name:string;ip:string;macAddress:string;vlan:string;assetType:string;environment:string;projectGroup:string;owner:string;location:string;status:string;note:string;ticketId?:string;assetId?:string;ruleId?:string;createdBy:string;approvedBy?:string;createdAt:string;updatedAt:string }
export interface AssetGovernanceIssue { assetId:string;assetName:string;assetType:string;assetTypeName:string;ip:string;status:string;projectGroup:string;owner:string;location:string;issueType:string;severity:string;detail:string;lastSeenAt:string;staleDays:number;reviewStatus:string;reviewedAt?:string;reviewedBy?:string;note?:string;ticketId?:string;updatedAt:string }
export interface AssetGovernanceReview { assetId:string;status:string;owner:string;projectGroup:string;location:string;reviewedAt?:string;reviewedBy?:string;note?:string;ticketId?:string;updatedAt:string }
export interface AssetGovernanceReport { evaluated:number;issues:number;critical:number;warning:number;tickets:number;alerts:number;ruleEvaluated:number;ruleMatched:number;ruleAssigned:number;errors?:string[] }

export interface AssetGovernanceRule { id:string;name:string;enabled:boolean;priority:number;matchAssetType:string;matchSource:string;matchIpCidr:string;matchProjectGroup:string;matchTag:string;owner:string;targetProjectGroup:string;location:string;note:string;createdBy:string;createdAt:string;updatedAt:string }
export interface AssetGovernanceRuleInput { name:string;enabled:boolean;priority:number;matchAssetType:string;matchSource:string;matchIpCidr:string;matchProjectGroup:string;matchTag:string;owner:string;targetProjectGroup:string;location:string;note:string }
export interface AssetGovernanceRuleReport { evaluated:number;matched:number;assigned:number;skipped:number;errors?:string[] }
export interface AssetGovernanceBulkResult { total:number;succeeded:number;failed:number;items:Array<{assetId:string;assetName:string;success:boolean;error?:string}> }
