import type { ModuleEntry, UserProfile } from './types'
export function canAccessRoute(permission: string|undefined, user:UserProfile|undefined): boolean { return !permission || Boolean(user?.permissions.includes(permission)) }
export function filterModules(modules:ModuleEntry[],user:UserProfile|undefined):ModuleEntry[]{ return user ? modules.filter(module=>user.permissions.includes(module.permission)) : [] }
