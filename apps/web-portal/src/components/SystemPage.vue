<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  Blocks, Copy, FileClock, KeyRound, Pencil, Plus, RefreshCw, Save,
  Settings, ShieldCheck, ToggleLeft, Trash2, UserPlus, Users, Waypoints, X,
} from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'
import { auditTone, userStatusLabel } from '@/features/system/systemModel'
import { fetchProjectGroups } from '@/api/cmdb'
import type { ProjectGroup } from '@/features/cmdb/types'

type PermissionEffect = 'inherit' | 'allow' | 'deny'
type PermissionOverride = { code:string; effect:'allow'|'deny'; reason?:string }
type PermissionItem = { code:string; name:string; module:string; action:string; risk:string; description:string }
type User = {
  id:string;username:string;displayName:string;department:string;email?:string;roles:string[];
  status:string;lastLogin:string;dataScope:string;projectGroups:string[];scopeTags:string[];
  keycloakGroups?:string[];keycloakId?:string;syncedAt?:string;permissionOverrides?:PermissionOverride[];
}
type Role = {
  id:string;name:string;description:string;users:number;permissions:string[];
  dataScope:string;projectGroups:string[];scopeTags:string[];system?:boolean;
}
type Module = { code:string;name:string;enabled:boolean;order:number;permission:string }
type Audit = { id:string;time:string;actor:string;action:string;resource:string;ip:string;result:string;detail:string }
type Setting = { key:string;name:string;value:string;category:string;description:string }
type ReadinessCheck = { key:string;name:string;category:string;status:string;required:boolean;detail:string }
type ReadinessReport = { status:string;score:number;passed:number;warning:number;failed:number;generatedAt:string;checks:ReadinessCheck[] }
type Summary = { users:number;activeUsers:number;roles:number;modules:number;auditsToday:number }
type ProjectionStatus = { neo4jConfigured:boolean;openSearchConfigured:boolean;neo4jUrl?:string;openSearchUrl?:string;neo4jAssets:number;openSearchAssets:number;detail?:string }
type ProjectionResult = { assets:number;neo4j:number;openSearch:number }
type SyncReport = { users:number;created:number;updated:number;groups:number;errors?:string[] }
type CreateUserResponse = { user:User;initialPassword:string }

const auth = useAuthStore()
const summary = ref<Summary>()
const users = ref<User[]>([])
const roles = ref<Role[]>([])
const permissionCatalog = ref<PermissionItem[]>([])
const modules = ref<Module[]>([])
const audits = ref<Audit[]>([])
const settings = ref<Setting[]>([])
const readiness = ref<ReadinessReport|null>(null)
const projections = ref<ProjectionStatus|null>(null)
const scopeProjectGroups = ref<ProjectGroup[]>([])
const tab = ref<'users'|'roles'|'modules'|'audits'|'settings'|'ready'|'projection'>('users')
const selectedRole = ref<Role>()
const busy = ref(false)
const message = ref('')
const error = ref('')
const search = ref('')
const statusFilter = ref('')
const passwordDialog = reactive({ open:false, username:'', password:'' })

const userDialog = reactive({
  open:false, mode:'create' as 'create'|'edit', id:'', username:'', displayName:'',
  department:'', email:'', roles:[] as string[], status:'active', dataScope:'groups',
  projectGroups:'', scopeTags:'', overrides:{} as Record<string, PermissionEffect>,
})
const roleForm = reactive({
  name:'', description:'', permissions:[] as string[], dataScope:'groups',
  projectGroups:'', scopeTags:'',
})

const headers = computed(() => ({
  Authorization:`Bearer ${auth.token}`,
  'Content-Type':'application/json',
}))

const filteredUsers = computed(() => {
  const query = search.value.trim().toLowerCase()
  return users.value.filter((user) => {
    if (statusFilter.value && user.status !== statusFilter.value) return false
    if (!query) return true
    return [user.username, user.displayName, user.department, user.email || '']
      .some((value) => value.toLowerCase().includes(query))
  })
})

const permissionsByModule = computed(() => {
  const groups = new Map<string, PermissionItem[]>()
  for (const permission of permissionCatalog.value) {
    const items = groups.get(permission.module) || []
    items.push(permission)
    groups.set(permission.module, items)
  }
  return Array.from(groups, ([module, items]) => ({ module, items }))
})

const inheritedPermissions = computed(() => {
  const selected = new Set(userDialog.roles)
  const result = new Set<string>()
  for (const role of roles.value) {
    if (!selected.has(role.id)) continue
    for (const permission of role.permissions) result.add(permission)
  }
  return result
})

const effectivePermissions = computed(() => {
  const result = new Set<string>()
  const inherited = inheritedPermissions.value
  for (const permission of permissionCatalog.value) {
    if (inherited.has('*') || inherited.has(permission.code)) result.add(permission.code)
  }
  for (const [code, effect] of Object.entries(userDialog.overrides)) {
    if (effect === 'allow') result.add(code)
    if (effect === 'deny') result.delete(code)
  }
  return Array.from(result)
})

function splitValues(value:string) {
  return value.split(',').map((item) => item.trim()).filter(Boolean)
}

function scopeLabel(value:string) {
  return ({ all:'全部资产', groups:'项目组范围', tags:'标签范围', none:'无资产权限' } as Record<string,string>)[value] || value
}

function roleName(id:string) {
  return roles.value.find((role) => role.id === id)?.name || id
}

function overrideCount(user:User) {
  return user.permissionOverrides?.length || 0
}

async function request<T>(path:string, init?:RequestInit):Promise<T> {
  const response = await fetch(path, { ...init, headers:headers.value })
  const data = response.status === 204 ? null : await response.json().catch(() => ({}))
  if (!response.ok) throw new Error((data as {message?:string})?.message || '系统请求失败')
  return data as T
}

async function loadScopeProjectGroups() {
  try {
    scopeProjectGroups.value = await fetchProjectGroups(auth.token)
  } catch {
    scopeProjectGroups.value = []
  }
}

async function load() {
  busy.value = true
  error.value = ''
  try {
    const [summaryData,userData,roleData,permissionData,moduleData,auditData,settingData,readinessData,projectionData] = await Promise.all([
      request<Summary>('/api/v1/system/summary'),
      request<User[]>('/api/v1/system/users'),
      request<Role[]>('/api/v1/system/roles'),
      request<PermissionItem[]>('/api/v1/system/permissions'),
      request<Module[]>('/api/v1/system/modules'),
      request<Audit[]>('/api/v1/system/audits'),
      request<Setting[]>('/api/v1/system/settings'),
      request<ReadinessReport>('/api/v1/system/production-readiness'),
      request<ProjectionStatus>('/api/v1/system/projections'),
    ])
    summary.value = summaryData
    users.value = userData
    roles.value = roleData
    permissionCatalog.value = permissionData
    modules.value = moduleData
    audits.value = auditData
    settings.value = settingData
    readiness.value = readinessData
    projections.value = projectionData
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '系统数据加载失败'
  } finally {
    busy.value = false
  }
}

function resetUserDialog() {
  const overrides:Record<string,PermissionEffect> = {}
  for (const permission of permissionCatalog.value) overrides[permission.code] = 'inherit'
  Object.assign(userDialog, {
    open:true, mode:'create', id:'', username:'', displayName:'', department:'',
    email:'', roles:[], status:'active', dataScope:'groups', projectGroups:'',
    scopeTags:'', overrides,
  })
}

function openCreateUser() {
  resetUserDialog()
  message.value = ''
  error.value = ''
}

function editUser(user:User) {
  const overrides:Record<string,PermissionEffect> = {}
  for (const permission of permissionCatalog.value) overrides[permission.code] = 'inherit'
  for (const item of user.permissionOverrides || []) overrides[item.code] = item.effect
  Object.assign(userDialog, {
    open:true, mode:'edit', id:user.id, username:user.username,
    displayName:user.displayName, department:user.department, email:user.email || '',
    roles:[...user.roles], status:user.status, dataScope:user.dataScope || 'groups',
    projectGroups:(user.projectGroups || []).join(', '),
    scopeTags:(user.scopeTags || []).join(', '), overrides,
  })
  message.value = ''
  error.value = ''
}

function permissionOverrides() {
  return Object.entries(userDialog.overrides)
    .filter(([, effect]) => effect !== 'inherit')
    .map(([code, effect]) => ({ code, effect, reason:'用户级权限调整' }))
}

async function saveUser() {
  if (!userDialog.username.trim() || !userDialog.displayName.trim() || !userDialog.roles.length) {
    error.value = '请填写用户名、显示名称并至少选择一个角色'
    return
  }
  busy.value = true
  error.value = ''
  try {
    const payload = {
      username:userDialog.username.trim(),
      displayName:userDialog.displayName.trim(),
      department:userDialog.department.trim(),
      email:userDialog.email.trim(),
      roles:userDialog.roles,
      status:userDialog.status,
      dataScope:userDialog.dataScope,
      projectGroups:splitValues(userDialog.projectGroups),
      scopeTags:splitValues(userDialog.scopeTags),
      permissionOverrides:permissionOverrides(),
    }
    if (userDialog.mode === 'create') {
      const result = await request<CreateUserResponse>('/api/v1/system/users', {
        method:'POST',
        body:JSON.stringify(payload),
      })
      if (result.initialPassword) {
        Object.assign(passwordDialog, {
          open:true, username:result.user.username, password:result.initialPassword,
        })
      }
      message.value = '用户已创建'
    } else {
      await request(`/api/v1/system/users/${encodeURIComponent(userDialog.id)}`, {
        method:'PUT',
        body:JSON.stringify(payload),
      })
      message.value = '用户角色、权限和数据范围已保存'
    }
    userDialog.open = false
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '用户保存失败'
  } finally {
    busy.value = false
  }
}

async function toggleUser(user:User) {
  busy.value = true
  try {
    await request(`/api/v1/system/users/${encodeURIComponent(user.id)}/toggle`, { method:'POST' })
    message.value = user.status === 'active' ? `已停用 ${user.displayName}` : `已启用 ${user.displayName}`
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '用户状态切换失败'
  } finally {
    busy.value = false
  }
}

async function resetPassword(user:User) {
  if (!window.confirm(`确认为 ${user.displayName} 生成一次性新密码并撤销现有会话？`)) return
  busy.value = true
  try {
    const result = await request<{password:string}>(`/api/v1/system/users/${encodeURIComponent(user.id)}/reset-password`, { method:'POST' })
    Object.assign(passwordDialog, { open:true, username:user.username, password:result.password })
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '密码重置失败'
  } finally {
    busy.value = false
  }
}

async function deleteUser(user:User) {
  if (!window.confirm(`确认删除用户“${user.displayName}”？账号将被禁用并进入回收站。`)) return
  busy.value = true
  try {
    await request(`/api/v1/system/users/${encodeURIComponent(user.id)}`, { method:'DELETE' })
    message.value = `用户 ${user.displayName} 已删除`
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '用户删除失败'
  } finally {
    busy.value = false
  }
}

async function copyPassword() {
  try {
    await navigator.clipboard.writeText(passwordDialog.password)
    message.value = '初始密码已复制'
  } catch {
    error.value = '浏览器不允许自动复制，请手动选择密码'
  }
}

function selectRole(role:Role) {
  selectedRole.value = {
    ...role,
    permissions:[...role.permissions],
    projectGroups:[...role.projectGroups],
    scopeTags:[...role.scopeTags],
  }
  Object.assign(roleForm, {
    name:role.name, description:role.description, permissions:[...role.permissions],
    dataScope:role.dataScope || 'groups', projectGroups:(role.projectGroups || []).join(', '),
    scopeTags:(role.scopeTags || []).join(', '),
  })
}

function createRole() {
  selectedRole.value = {
    id:'', name:'', description:'', users:0, permissions:[], dataScope:'groups',
    projectGroups:[], scopeTags:[], system:false,
  }
  Object.assign(roleForm, {
    name:'', description:'', permissions:[], dataScope:'groups',
    projectGroups:'', scopeTags:'',
  })
}

async function saveRole() {
  if (!roleForm.name.trim()) {
    error.value = '请填写角色名称'
    return
  }
  busy.value = true
  error.value = ''
  const payload = {
    name:roleForm.name.trim(),
    description:roleForm.description.trim(),
    permissions:roleForm.permissions,
    dataScope:roleForm.dataScope,
    projectGroups:splitValues(roleForm.projectGroups),
    scopeTags:splitValues(roleForm.scopeTags),
  }
  try {
    if (selectedRole.value?.id) {
      await request(`/api/v1/system/roles/${encodeURIComponent(selectedRole.value.id)}/permissions`, {
        method:'PUT',
        body:JSON.stringify(payload),
      })
      message.value = '角色权限已保存'
    } else {
      await request('/api/v1/system/roles', { method:'POST', body:JSON.stringify(payload) })
      message.value = '角色已创建'
    }
    selectedRole.value = undefined
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '角色保存失败'
  } finally {
    busy.value = false
  }
}

async function deleteRole() {
  if (!selectedRole.value?.id || selectedRole.value.system) return
  if (!window.confirm(`确认删除角色“${selectedRole.value.name}”？`)) return
  busy.value = true
  try {
    await request(`/api/v1/system/roles/${encodeURIComponent(selectedRole.value.id)}`, { method:'DELETE' })
    selectedRole.value = undefined
    message.value = '角色已删除'
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '角色删除失败'
  } finally {
    busy.value = false
  }
}

async function rebuildProjections() {
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const result = await request<ProjectionResult>('/api/v1/system/projections/rebuild', { method:'POST' })
    message.value = `拓扑投影已重建：资产 ${result.assets} 条，Neo4j ${result.neo4j} 条，OpenSearch ${result.openSearch} 条`
    projections.value = await request<ProjectionStatus>('/api/v1/system/projections')
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '拓扑投影重建失败'
  } finally {
    busy.value = false
  }
}

async function syncKeycloak() {
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const report = await request<SyncReport>('/api/v1/system/sync/keycloak', { method:'POST' })
    message.value = `Keycloak 同步完成：用户 ${report.users}，新增 ${report.created}，更新 ${report.updated}，用户组 ${report.groups}`
    if (report.errors?.length) error.value = report.errors.slice(0, 5).join('；')
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : 'Keycloak 同步失败'
  } finally {
    busy.value = false
  }
}

onMounted(load)
onMounted(loadScopeProjectGroups)
</script>

<template>
  <section class="system-page">
    <header class="system-heading">
      <div>
        <span>PLATFORM GOVERNANCE</span>
        <h1>系统管理中心</h1>
        <p>用户、角色、权限、项目组数据范围、模块和审计统一治理</p>
      </div>
      <div>
        <button :disabled="busy" @click="syncKeycloak"><RefreshCw :class="{spin:busy}"/>同步 Keycloak</button>
        <button :disabled="busy" @click="load"><RefreshCw/>刷新配置</button>
      </div>
    </header>

    <div v-if="message" class="system-message">{{ message }}</div>
    <div v-if="error" class="system-error">{{ error }}</div>

    <div class="system-summary">
      <article><Users/><span>平台用户<strong>{{summary?.users}}</strong><small>{{summary?.activeUsers}} 个正常</small></span></article>
      <article><ShieldCheck/><span>角色数量<strong>{{summary?.roles}}</strong></span></article>
      <article><Blocks/><span>启用模块<strong>{{summary?.modules}}</strong></span></article>
      <article><FileClock/><span>今日审计<strong>{{summary?.auditsToday}}</strong></span></article>
    </div>

    <div class="system-workspace">
      <aside>
        <button v-for="item in [
          {id:'users',n:'用户与组织',i:Users},
          {id:'roles',n:'角色与数据权限',i:KeyRound},
          {id:'modules',n:'模块配置',i:Blocks},
          {id:'audits',n:'审计日志',i:FileClock},
          {id:'settings',n:'平台参数',i:Settings},
          {id:'ready',n:'生产就绪',i:ShieldCheck},
          {id:'projection',n:'拓扑投影',i:Waypoints},
        ]" :key="item.id" :class="{active:tab===item.id}" @click="tab=item.id as typeof tab">
          <component :is="item.i"/>{{item.n}}
        </button>
      </aside>

      <main>
        <template v-if="tab==='users'">
          <header class="section-heading">
            <div><h2>用户与组织</h2><span>角色决定基础权限，用户级允许/拒绝可做精细调整</span></div>
            <button class="primary" @click="openCreateUser"><UserPlus/>新增用户</button>
          </header>
          <div class="user-filters">
            <input v-model="search" placeholder="搜索姓名、用户名、部门或邮箱">
            <select v-model="statusFilter">
              <option value="">全部状态</option>
              <option value="active">正常</option>
              <option value="disabled">已停用</option>
              <option value="locked">已锁定</option>
            </select>
          </div>
          <div class="system-table user-table">
            <div class="table-head"><span>用户</span><span>部门</span><span>角色</span><span>权限覆盖</span><span>数据范围</span><span>状态</span><span>操作</span></div>
            <div v-for="u in filteredUsers" :key="u.id">
              <span><strong>{{u.displayName}}</strong><small>{{u.username}}<template v-if="u.keycloakId"> · Keycloak</template></small></span>
              <span>{{u.department||'-'}}</span>
              <span><b v-for="r in u.roles" :key="r">{{roleName(r)}}</b></span>
              <span><em class="scope-pill">{{overrideCount(u)}} 项</em></span>
              <span><small>{{(u.projectGroups||[]).join('、')||'未设置'}}<template v-if="u.scopeTags?.length"><br/>标签：{{u.scopeTags.join('、')}}</template></small></span>
              <span><i :data-status="u.status">{{userStatusLabel(u.status)}}</i></span>
              <span class="row-actions">
                <button @click="editUser(u)"><Pencil/>编辑</button>
                <button @click="resetPassword(u)"><KeyRound/>密码</button>
                <button @click="toggleUser(u)"><ToggleLeft/>{{u.status==='active'?'停用':'启用'}}</button>
                <button class="danger" @click="deleteUser(u)"><Trash2/>删除</button>
              </span>
            </div>
            <p v-if="!filteredUsers.length" class="empty-row">没有匹配的用户</p>
          </div>
        </template>

        <template v-else-if="tab==='roles'">
          <header class="section-heading">
            <div><h2>角色与数据权限</h2><span>系统角色受保护；自定义角色可新建、复制权限和删除</span></div>
            <button class="primary" @click="createRole"><Plus/>新建角色</button>
          </header>
          <div class="role-grid">
            <button v-for="r in roles" :key="r.id" :class="{active:selectedRole?.id===r.id}" @click="selectRole(r)">
              <ShieldCheck/>
              <div>
                <strong>{{r.name}}<em v-if="r.system">系统</em></strong>
                <span>{{r.description}}</span>
                <small>{{r.users}} 个用户 · {{r.permissions.length}} 项权限 · {{scopeLabel(r.dataScope)}}</small>
              </div>
            </button>
          </div>
          <section v-if="selectedRole" class="permission-editor">
            <header>
              <div><h3>{{selectedRole.id ? '编辑角色' : '新建角色'}}</h3><span>{{selectedRole.system ? '系统角色不能删除' : '自定义角色'}}</span></div>
              <div class="editor-actions">
                <button v-if="selectedRole.id && !selectedRole.system" class="danger" @click="deleteRole"><Trash2/>删除</button>
                <button class="primary" :disabled="busy" @click="saveRole"><Save/>{{selectedRole.id ? '保存角色' : '创建角色'}}</button>
              </div>
            </header>
            <div class="role-meta">
              <label><span>角色名称</span><input v-model="roleForm.name" placeholder="例如：项目组发布者"></label>
              <label><span>角色说明</span><input v-model="roleForm.description" placeholder="说明角色职责"></label>
            </div>
            <div v-if="selectedRole.id==='platform-admin'" class="admin-notice">平台超级管理员固定拥有全部权限，不允许移除。</div>
            <div v-else class="permission-groups">
              <section v-for="group in permissionsByModule" :key="group.module">
                <header><b>{{group.module}}</b><small>{{group.items.length}} 项</small></header>
                <label v-for="permission in group.items" :key="permission.code">
                  <input v-model="roleForm.permissions" type="checkbox" :value="permission.code">
                  <span>{{permission.name}}<small>{{permission.code}} · {{permission.risk}}</small></span>
                </label>
              </section>
            </div>
            <div class="scope-form">
              <label><span>数据范围</span><select v-model="roleForm.dataScope"><option value="all">全部资产</option><option value="groups">项目组范围</option><option value="tags">标签范围</option><option value="none">无资产权限</option></select></label>
              <label><span>项目组（逗号分隔）</span><input v-model="roleForm.projectGroups" list="scope-project-groups" placeholder="选择项目组编码"></label>
              <label><span>标签范围（逗号分隔）</span><input v-model="roleForm.scopeTags" placeholder="生产, 核心"></label>
            </div>
          </section>
        </template>

        <template v-else-if="tab==='modules'">
          <header><div><h2>模块配置</h2><span>控制平台模块启用状态与菜单顺序</span></div></header>
          <div class="module-grid">
            <article v-for="m in modules" :key="m.code"><Blocks/><div><strong>{{m.name}}</strong><span>{{m.code}} · {{m.permission}}</span></div><b>顺序 {{m.order}}</b><em>{{m.enabled?'已启用':'已停用'}}</em></article>
          </div>
        </template>

        <template v-else-if="tab==='audits'">
          <header><div><h2>审计日志</h2><span>记录用户、角色、权限和关键运维操作</span></div></header>
          <div class="audit-list">
            <article v-for="a in audits" :key="a.id"><time>{{a.time}}</time><i :data-tone="auditTone(a.result)"></i><div><strong>{{a.actor}} · {{a.action}}</strong><span>{{a.resource}} · {{a.detail}}</span></div><code>{{a.ip}}</code><b>{{a.result}}</b></article>
          </div>
        </template>

        <template v-else-if="tab==='projection'">
          <header><div><h2>拓扑投影</h2><span>PostgreSQL 是唯一底账，Neo4j 与 OpenSearch 是可随时重建的副本</span></div><button class="primary" :disabled="busy" @click="rebuildProjections"><RefreshCw :class="{spin:busy}"/>重建投影</button></header>
          <div class="projection-grid">
            <article :data-status="projections?.neo4jConfigured?'passed':'warning'"><i></i><div><strong>Neo4j 图数据库</strong><span>资产关系图 · {{projections?.neo4jUrl||'未配置'}}</span><small>已投影 {{projections?.neo4jAssets??0}} 个节点</small></div></article>
            <article :data-status="projections?.openSearchConfigured?'passed':'warning'"><i></i><div><strong>OpenSearch 检索</strong><span>索引 cmdb-assets · {{projections?.openSearchUrl||'未配置'}}</span><small>已投影 {{projections?.openSearchAssets??0}} 个文档</small></div></article>
          </div>
          <p v-if="projections?.detail" class="readiness-time">探测异常：{{projections.detail}}</p>
        </template>

        <template v-else-if="tab==='ready'">
          <header><div><h2>生产就绪检查</h2><span>安全、数据、监控、发布和 AI 依赖统一体检</span></div><b>{{readiness?.status==='passed'?'可以上线':readiness?.status==='warning'?'存在提示':'存在阻断'}} · {{readiness?.score}} 分</b></header>
          <div class="readiness-grid">
            <article v-for="item in readiness?.checks" :key="item.key" :data-status="item.status"><i></i><div><strong>{{item.name}}</strong><span>{{item.category}} · {{item.required?'必需':'可选'}}</span><small>{{item.detail}}</small></div></article>
          </div>
          <p class="readiness-time">检查时间：{{readiness?.generatedAt}} · 通过 {{readiness?.passed}} · 提示 {{readiness?.warning}} · 失败 {{readiness?.failed}}</p>
        </template>

        <template v-else>
          <header><div><h2>平台参数</h2><span>系统运行、安全与数据保留策略</span></div></header>
          <div class="setting-list">
            <label v-for="s in settings" :key="s.key"><div><b>{{s.category}}</b><strong>{{s.name}}</strong><span>{{s.description}}</span></div><input v-model="s.value"><code>{{s.key}}</code></label>
          </div>
        </template>
      </main>
    </div>

    <div v-if="userDialog.open" class="system-modal" @click.self="userDialog.open=false">
      <form class="user-dialog" @submit.prevent="saveUser">
        <header>
          <div><span>{{userDialog.mode==='create'?'CREATE USER':'EDIT USER'}}</span><h3>{{userDialog.mode==='create'?'新增用户':'编辑用户权限'}}</h3></div>
          <button type="button" @click="userDialog.open=false"><X/></button>
        </header>
        <div class="user-form">
          <label><span>用户名</span><input v-model="userDialog.username" :disabled="userDialog.mode==='edit'" placeholder="登录用户名"></label>
          <label><span>显示名称</span><input v-model="userDialog.displayName" placeholder="姓名或显示名"></label>
          <label><span>部门</span><input v-model="userDialog.department"></label>
          <label><span>邮箱</span><input v-model="userDialog.email" placeholder="name@example.com"></label>
          <label><span>账号状态</span><select v-model="userDialog.status"><option value="active">正常</option><option value="disabled">停用</option><option value="locked">锁定</option></select></label>
          <label><span>数据范围</span><select v-model="userDialog.dataScope"><option value="all">全部资产</option><option value="groups">项目组范围</option><option value="tags">标签范围</option><option value="none">无资产权限</option></select></label>
          <label class="wide"><span>选择角色</span>
            <div class="role-checks">
              <label v-for="role in roles" :key="role.id"><input v-model="userDialog.roles" type="checkbox" :value="role.id"><span>{{role.name}}<small>{{role.description}}</small></span></label>
            </div>
          </label>
          <div class="wide effective-box">
            <div><b>有效权限预览</b><span>{{effectivePermissions.length}} 项</span></div>
            <div class="permission-chips"><span v-for="code in effectivePermissions.slice(0, 18)" :key="code">{{code}}</span><span v-if="effectivePermissions.length>18">+{{effectivePermissions.length-18}}</span></div>
          </div>
          <div class="wide override-box">
            <header><b>用户级权限调整</b><span>“跟随角色”使用角色权限；“拒绝”优先于角色和 Keycloak 权限</span></header>
            <div class="override-grid">
              <label v-for="permission in permissionCatalog" :key="permission.code">
                <span>{{permission.name}}<small>{{permission.code}}</small></span>
                <select v-model="userDialog.overrides[permission.code]">
                  <option value="inherit">跟随角色</option>
                  <option value="allow">额外允许</option>
                  <option value="deny">明确拒绝</option>
                </select>
              </label>
            </div>
          </div>
          <label><span>项目组（逗号分隔）</span><input v-model="userDialog.projectGroups" list="scope-project-groups" placeholder="选择项目组编码"></label>
          <label><span>标签范围（逗号分隔）</span><input v-model="userDialog.scopeTags" placeholder="生产, 核心"></label>
        </div>
        <footer>
          <button type="button" @click="userDialog.open=false">取消</button>
          <button class="primary" :disabled="busy" type="submit">{{busy?'保存中':'保存用户'}}</button>
        </footer>
      </form>
    </div>

    <div v-if="passwordDialog.open" class="system-modal" @click.self="passwordDialog.open=false">
      <section class="password-dialog">
        <header><div><span>ONE-TIME PASSWORD</span><h3>{{passwordDialog.username}}</h3></div><button @click="passwordDialog.open=false"><X/></button></header>
        <p>该密码仅显示一次。用户首次登录后需要修改密码。</p>
        <code>{{passwordDialog.password}}</code>
        <footer><button @click="passwordDialog.open=false">关闭</button><button class="primary" @click="copyPassword"><Copy/>复制密码</button></footer>
      </section>
    </div>

    <datalist id="scope-project-groups"><option v-for="item in scopeProjectGroups" :key="item.code" :value="item.code">{{item.name}}</option></datalist>
  </section>
</template>

<style scoped>
.system-page{display:grid;gap:14px}
.system-heading{display:flex;justify-content:space-between;align-items:end}
.system-heading>div:first-child{display:grid;gap:3px}
.system-heading>div:first-child>span{color:#31c9eb;font-size:10px;letter-spacing:2px}
.system-heading h1{margin:0;font-size:23px}
.system-heading p{margin:0;color:#7890a8;font-size:11px}
.system-heading>div:last-child,.editor-actions,.row-actions{display:flex;gap:6px}
.system-page button{display:inline-flex;align-items:center;gap:5px;border:1px solid #28536c;background:#0d2235;color:#b9d4e3;border-radius:6px;padding:7px 10px;font-size:11px;cursor:pointer}
.system-page button.primary{background:#0b6da5;border-color:#1a93c8;color:#fff}
.system-page button.danger{border-color:#783e49;color:#ff9aa7}
.system-page button:disabled{opacity:.45;cursor:not-allowed}
.system-page button svg{width:13px;height:13px}
.system-message,.system-error{padding:9px 11px;border-radius:6px;font-size:11px}
.system-message{background:rgba(72,216,154,.1);color:#5bdfa1;border:1px solid rgba(72,216,154,.2)}
.system-error{background:rgba(255,82,104,.1);color:#ff8998;border:1px solid rgba(255,82,104,.2)}
.system-summary{display:grid;grid-template-columns:repeat(4,1fr);gap:9px}
.system-summary article{display:flex;align-items:center;gap:10px;padding:12px;border:1px solid #173b55;background:#081a2b;border-radius:8px}
.system-summary svg{width:20px;color:#31c9eb}
.system-summary span{display:grid;color:#7890a8;font-size:10px}
.system-summary strong{font-size:20px;color:#e3f3fb}
.system-summary small{font-size:9px}
.system-workspace{display:grid;grid-template-columns:210px minmax(0,1fr);gap:12px}
.system-workspace>aside{display:grid;align-content:start;gap:4px;padding:8px;border:1px solid #173b55;background:#081a2b;border-radius:8px}
.system-workspace>aside button{justify-content:flex-start;background:transparent;border:0;color:#8099ae}
.system-workspace>aside button.active{background:#0b3049;color:#67d8f2}
.system-workspace main{min-width:0;border:1px solid #173b55;background:#081a2b;border-radius:8px;padding:14px}
.system-workspace main>header,.section-heading{display:flex;justify-content:space-between;align-items:flex-start;gap:10px;margin-bottom:12px}
.system-workspace h2{margin:0;font-size:15px}
.system-workspace header span{display:block;margin-top:3px;color:#7890a8;font-size:10px}
.user-filters{display:flex;gap:8px;margin-bottom:10px}
.user-filters input,.user-filters select{height:34px;border:1px solid #28536c;background:#031525;color:#d5edf8;border-radius:5px;padding:0 8px}
.user-filters input{flex:1}
.system-table{overflow:auto}
.table-head,.system-table>div{display:grid;gap:9px;align-items:center;min-width:1120px;padding:9px;border-bottom:1px solid #17324a;font-size:11px}
.user-table .table-head,.user-table>div{grid-template-columns:1.15fr .75fr 1fr .65fr 1.15fr .55fr 1.45fr}
.table-head{background:#0b2235;color:#7890a8}
.system-table strong,.system-table small{display:block}
.system-table small{margin-top:2px;color:#69889d;font-size:9px}
.system-table b{display:inline-block;margin:1px;padding:2px 5px;background:#102f46;color:#7dd9f3;border-radius:4px;font-size:9px;font-weight:500}
.system-table i{font-style:normal;font-size:10px;color:#5bdfa1}
.system-table i[data-status=disabled],.system-table i[data-status=locked]{color:#ff9aa8}
.scope-pill{padding:3px 6px;border-radius:4px;background:rgba(72,200,232,.12);color:#48c8e8;font-style:normal;font-size:9px}
.row-actions{flex-wrap:wrap}
.row-actions button{padding:4px 6px;font-size:9px}
.empty-row{padding:20px;color:#69889d;font-size:11px}
.role-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(230px,1fr));gap:8px}
.role-grid>button{justify-content:flex-start;text-align:left;background:#0b2235}
.role-grid>button.active{border-color:#31c9eb;background:#10334b}
.role-grid svg{width:19px;color:#31c9eb}
.role-grid strong,.role-grid span,.role-grid small{display:block}
.role-grid strong em{padding:2px 5px;margin-left:6px;background:#593b12;color:#fbbf24;font-size:8px;font-style:normal}
.role-grid span,.role-grid small{color:#7890a8;font-size:9px}
.permission-editor{margin-top:12px;padding:12px;border:1px solid #28536c;background:#10273a;border-radius:8px}
.permission-editor>header{display:flex;justify-content:space-between;align-items:center;gap:10px;margin-bottom:10px}
.permission-editor h3{margin:0;font-size:14px}
.role-meta{display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-bottom:10px}
.role-meta label,.scope-form label{display:grid;gap:4px}
.role-meta span,.scope-form span{font-size:10px;color:#7890a8}
.role-meta input,.scope-form input,.scope-form select{height:32px;border:1px solid #28536c;background:#031525;color:#d5edf8;border-radius:5px;padding:0 7px}
.admin-notice{padding:9px;border:1px solid #7c5c19;background:#2e250f;color:#fbbf24;font-size:10px}
.permission-groups{display:grid;gap:8px;max-height:360px;overflow:auto;padding-right:3px}
.permission-groups section{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:5px;padding:7px;border:1px solid #1b405a;background:#061a2c}
.permission-groups section>header{grid-column:1/-1;display:flex;justify-content:space-between}
.permission-groups section>header small{color:#69889d}
.permission-groups label{display:flex;gap:6px;align-items:flex-start;font-size:10px}
.permission-groups label span{display:grid}
.permission-groups label small{color:#69889d;font-size:8px}
.scope-form{display:grid;grid-template-columns:1fr 1fr 1fr;gap:8px;margin-top:10px;padding-top:10px;border-top:1px solid #1b405a}
.module-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:8px}
.module-grid article{display:grid;grid-template-columns:auto 1fr auto;gap:9px;align-items:center;padding:10px;border:1px solid #17415e;background:#061a2c}
.module-grid strong,.module-grid span{display:block}
.module-grid span{font-size:9px;color:#7890a8}
.module-grid em{grid-column:3;font-style:normal;font-size:9px;color:#5bdfa1}
.audit-list{display:grid;gap:5px}
.audit-list article{display:grid;grid-template-columns:60px 8px 1fr 100px 60px;gap:8px;align-items:center;padding:8px;background:#061a2c;border-bottom:1px solid #17324a;font-size:10px}
.audit-list time,.audit-list span{color:#7890a8}
.audit-list i{width:7px;height:7px;border-radius:50%;background:#5bdfa1}
.audit-list div strong,.audit-list div span{display:block}
.setting-list{display:grid;gap:7px}
.setting-list label{display:grid;grid-template-columns:1fr 220px 160px;gap:10px;align-items:center;padding:9px;background:#061a2c;border:1px solid #17415e}
.setting-list strong,.setting-list span,.setting-list b{display:block}
.setting-list span{font-size:9px;color:#7890a8}
.setting-list b{font-size:9px;color:#31c9eb}
.setting-list input{height:31px;border:1px solid #28536c;background:#031525;color:#d5edf8;padding:0 7px}
.readiness-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:8px}
.readiness-grid article{display:flex;gap:9px;padding:10px;border:1px solid #17415e;background:#061a2c}
.readiness-grid i{width:8px;height:8px;border-radius:50%;margin-top:4px;background:#5bdfa1}
.readiness-grid article[data-status=warning] i{background:#fbbf24}
.readiness-grid article[data-status=failed] i{background:#fb7185}
.readiness-grid strong,.readiness-grid span,.readiness-grid small{display:block}
.readiness-grid span,.readiness-grid small{color:#7890a8;font-size:9px}
.readiness-time{color:#7890a8;font-size:10px}
.projection-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:8px}
.projection-grid article{display:flex;gap:9px;padding:12px;border:1px solid #17415e;background:#061a2c}
.projection-grid i{width:8px;height:8px;border-radius:50%;margin-top:4px;background:#5bdfa1}
.projection-grid article[data-status=warning] i{background:#fbbf24}
.projection-grid strong,.projection-grid span,.projection-grid small{display:block}
.projection-grid span,.projection-grid small{color:#7890a8;font-size:9px}
.system-modal{position:fixed;inset:0;z-index:90;display:grid;place-items:center;padding:20px;background:rgba(0,8,16,.8)}
.user-dialog{width:min(1100px,96vw);max-height:92vh;overflow:auto;background:#081a2b;border:1px solid #246c98;border-radius:10px}
.user-dialog>header,.password-dialog>header{display:flex;justify-content:space-between;padding:14px 18px;border-bottom:1px solid #174d78}
.user-dialog>header span,.password-dialog span{color:#31c9eb;font-size:9px}
.user-dialog h3,.password-dialog h3{margin:3px 0}
.user-dialog header button,.password-dialog header button{border:0;background:transparent;padding:0}
.user-form{display:grid;grid-template-columns:1fr 1fr;gap:10px;padding:14px 18px}
.user-form>label{display:grid;gap:4px}
.user-form>label>span{color:#7890a8;font-size:10px}
.user-form input,.user-form select{height:32px;border:1px solid #28536c;background:#031525;color:#d5edf8;padding:0 7px;border-radius:5px}
.user-form .wide{grid-column:1/-1}
.role-checks{display:grid;grid-template-columns:repeat(3,1fr);gap:5px}
.role-checks label{display:flex;gap:6px;align-items:flex-start;padding:7px;border:1px solid #1b405a;background:#061a2c;font-size:10px}
.role-checks label span{display:grid}
.role-checks small{color:#69889d;font-size:8px}
.effective-box,.override-box{padding:10px;border:1px solid #1b405a;background:#061a2c}
.effective-box>div:first-child,.override-box>header{display:flex;justify-content:space-between;align-items:center;margin-bottom:7px}
.effective-box span,.override-box header span{color:#7890a8;font-size:9px}
.permission-chips{display:flex;flex-wrap:wrap;gap:4px}
.permission-chips span{padding:2px 5px;background:#10334a;color:#7dd3fc;font-size:8px}
.override-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:5px;max-height:280px;overflow:auto}
.override-grid label{display:flex;justify-content:space-between;align-items:center;gap:6px;padding:5px;border-bottom:1px solid #17324a}
.override-grid label>span{display:grid;color:#b9d4e3;font-size:9px}
.override-grid small{color:#69889d;font-size:8px}
.override-grid select{width:105px;height:27px;border:1px solid #28536c;background:#031525;color:#d5edf8;font-size:9px}
.user-dialog>footer,.password-dialog>footer{display:flex;justify-content:flex-end;gap:7px;padding:12px 18px;border-top:1px solid #174d78}
.password-dialog{width:min(520px,94vw);background:#081a2b;border:1px solid #246c98;border-radius:10px}
.password-dialog p{margin:14px 18px 0;color:#7890a8;font-size:10px}
.password-dialog>code{display:block;margin:12px 18px;padding:12px;background:#031525;border:1px solid #28536c;color:#7dd3fc;font-size:16px;letter-spacing:1px;word-break:break-all}
.spin{animation:spin .9s linear infinite}
@keyframes spin{to{transform:rotate(360deg)}}
@media(max-width:1000px){
  .system-summary{grid-template-columns:1fr 1fr}
  .system-workspace{grid-template-columns:1fr}
  .user-form,.role-meta,.scope-form,.permission-groups section,.override-grid{grid-template-columns:1fr}
  .user-form .wide,.permission-groups section>header{grid-column:auto}
}
</style>
