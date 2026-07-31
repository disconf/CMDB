import { describe, expect, it } from 'vitest'
import { canAccessRoute, filterModules } from './permissions'
import type { ModuleEntry, UserProfile } from './types'

const viewer: UserProfile = { id:'u2', username:'viewer', displayName:'只读用户', roles:['viewer'], permissions:['dashboard:view','cmdb:view'], projectGroups:['华东业务组'] }
const modules: ModuleEntry[] = [
  { code:'dashboard', name:'全局态势', path:'/', icon:'LayoutDashboard', permission:'dashboard:view' },
  { code:'cmdb', name:'CMDB 资产', path:'/cmdb', icon:'Database', permission:'cmdb:view' },
  { code:'system', name:'系统管理', path:'/system', icon:'Settings', permission:'system:manage' },
]

describe('permissions', () => {
  it('filters modules by user permission', () => { expect(filterModules(modules, viewer).map(item=>item.code)).toEqual(['dashboard','cmdb']) })
  it('allows public routes without a user', () => { expect(canAccessRoute(undefined, undefined)).toBe(true) })
  it('rejects protected routes without a user', () => { expect(canAccessRoute('cmdb:view', undefined)).toBe(false) })
  it('checks the required permission', () => { expect(canAccessRoute('cmdb:view', viewer)).toBe(true); expect(canAccessRoute('system:manage', viewer)).toBe(false) })
})
