export interface UserProfile { id:string; username:string; displayName:string; roles:string[]; permissions:string[]; projectGroups:string[] }
export interface ModuleEntry { code:string; name:string; path:string; icon:string; permission:string }
export interface LoginSession { token:string; user:UserProfile }
