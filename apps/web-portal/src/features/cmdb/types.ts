export interface ModelField {name:string;label:string;type:'text'|'number'|'boolean'|'date'|'select';required:boolean;defaultValue:string}
export interface CiModel { code:string;name:string;category:string;icon:string;count:number;description:string;enabled:boolean;fields:ModelField[] }
export interface Attribute { name:string;label:string;value:string }
export interface Relation { type:string;targetId:string;targetName:string }
export interface Asset { id:string;name:string;type:string;typeName:string;status:string;ip:string;environment:string;projectGroup:string;owner:string;location:string;source:string;lastSeenAt:string;tags:string[];attributes:Attribute[];relations:Relation[] }
export interface AssetSummary { total:number;online:number;warning:number;offline:number;models:number;projectGroups:number }
export interface AssetFilters { search:string;type:string;status:string;projectGroup:string;page:number;pageSize:number }
export interface AssetPage { data:Asset[];meta:{total:number;page:number;pageSize:number;totalPages:number} }
export interface AssetInput { id:string;name:string;type:string;status:string;ip:string;environment:string;projectGroup:string;owner:string;location:string;source:string;tags:string[];attributes?:Attribute[] }
export interface HistoryEntry { id:string;assetId:string;action:string;operator:string;occurredAt:string;changes:Array<{field:string;before:string;after:string}> }
export interface ImportResult { total:number;created:number;errors:Array<{row:number;id:string;message:string}> }
