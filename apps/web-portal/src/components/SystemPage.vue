<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Blocks, FileClock, KeyRound, RefreshCw, Save, Settings, ShieldCheck, ToggleLeft, Users, Waypoints, X } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'
import { auditTone, userStatusLabel } from '@/features/system/systemModel'

type User = { id:string;username:string;displayName:string;department:string;email?:string;roles:string[];status:string;lastLogin:string;dataScope:string;projectGroups:string[];scopeTags:string[];keycloakGroups?:string[];keycloakId?:string;syncedAt?:string }
type Role = { id:string;name:string;description:string;users:number;permissions:string[];dataScope:string;projectGroups:string[];scopeTags:string[] }
type Module = { code:string;name:string;enabled:boolean;order:number;permission:string }
type Audit = { id:string;time:string;actor:string;action:string;resource:string;ip:string;result:string;detail:string }
type Setting = { key:string;name:string;value:string;category:string;description:string }
type ReadinessCheck = { key:string;name:string;category:string;status:string;required:boolean;detail:string }
type ReadinessReport = { status:string;score:number;passed:number;warning:number;failed:number;generatedAt:string;checks:ReadinessCheck[] }
type Summary = { users:number;activeUsers:number;roles:number;modules:number;auditsToday:number }
type ProjectionStatus = { neo4jConfigured:boolean;openSearchConfigured:boolean;neo4jUrl?:string;openSearchUrl?:string;neo4jAssets:number;openSearchAssets:number;detail?:string }
type ProjectionResult = { assets:number;neo4j:number;openSearch:number }
type SyncReport = { users:number;created:number;updated:number;groups:number;errors?:string[] }

const auth = useAuthStore()
const summary = ref<Summary>()
const users = ref<User[]>([])
const roles = ref<Role[]>([])
const modules = ref<Module[]>([])
const audits = ref<Audit[]>([])
const settings = ref<Setting[]>([]);const readiness = ref<ReadinessReport|null>(null);const projections = ref<ProjectionStatus|null>(null)
const tab = ref<'users'|'roles'|'modules'|'audits'|'settings'|'ready'|'projection'>('users')
const selectedRole = ref<Role>()
const editingUser = ref<User>()
const busy = ref(false)
const message = ref('')
const error = ref('')
const userForm = reactive({ displayName:'', department:'', email:'', roles:[] as string[], status:'active', dataScope:'groups', projectGroups:'', scopeTags:'' })
const roleForm = reactive({ permissions:[] as string[], dataScope:'groups', projectGroups:'', scopeTags:'' })
const permissions = ['dashboard:view','cmdb:view','cmdb:manage','discovery:view','discovery:manage','topology:view','monitor:view','monitor:manage','job:view','job:manage','ticket:view','ticket:manage','release:view','release:manage','toolbox:use','aiops:view','aiops:execute','system:manage']
const headers = computed(() => ({ Authorization:`Bearer ${auth.token}`, 'Content-Type':'application/json' }))

function splitValues(value:string) { return value.split(',').map(item => item.trim()).filter(Boolean) }
function scopeLabel(value:string) { return ({ all:'全部资产', groups:'项目组范围', tags:'标签范围', none:'无资产权限' } as Record<string,string>)[value] || value }
async function request<T>(path:string, init?:RequestInit):Promise<T> { const response=await fetch(path,{...init,headers:headers.value}); const data=await response.json().catch(()=>({})); if(!response.ok) throw new Error((data as {message?:string}).message || '系统请求失败'); return data as T }
async function load() {
  busy.value=true; error.value=''
  try {
    const [summaryData,userData,roleData,moduleData,auditData,settingData,readinessData,projectionData] = await Promise.all([
      request<Summary>('/api/v1/system/summary'),
      request<User[]>('/api/v1/system/users'),
      request<Role[]>('/api/v1/system/roles'),
      request<Module[]>('/api/v1/system/modules'),
      request<Audit[]>('/api/v1/system/audits'),
      request<Setting[]>('/api/v1/system/settings'),
      request<ReadinessReport>('/api/v1/system/production-readiness'),
      request<ProjectionStatus>('/api/v1/system/projections'),
    ])
    summary.value=summaryData; users.value=userData; roles.value=roleData; modules.value=moduleData; audits.value=auditData; settings.value=settingData; readiness.value=readinessData; projections.value=projectionData
  }
  catch(reason){ error.value=reason instanceof Error?reason.message:'系统数据加载失败' } finally { busy.value=false }
}
function editUser(user:User) {
  editingUser.value=user
  Object.assign(userForm,{displayName:user.displayName,department:user.department,email:user.email||'',roles:[...user.roles],status:user.status,dataScope:user.dataScope||'groups',projectGroups:(user.projectGroups||[]).join(', '),scopeTags:(user.scopeTags||[]).join(', ')})
}
async function saveUser() {
  if(!editingUser.value) return
  busy.value=true; error.value=''
  try {
    await request(`/api/v1/system/users/${editingUser.value.id}`,{method:'PUT',body:JSON.stringify({...userForm,roles:userForm.roles,projectGroups:splitValues(userForm.projectGroups),scopeTags:splitValues(userForm.scopeTags)})})
    editingUser.value=undefined; message.value='用户角色和数据范围已保存'; await load()
  } catch(reason){ error.value=reason instanceof Error?reason.message:'用户保存失败' } finally { busy.value=false }
}
function selectRole(role:Role) {
  selectedRole.value={...role,permissions:[...role.permissions],projectGroups:[...role.projectGroups],scopeTags:[...role.scopeTags]}
  Object.assign(roleForm,{permissions:[...role.permissions],dataScope:role.dataScope||'groups',projectGroups:(role.projectGroups||[]).join(', '),scopeTags:(role.scopeTags||[]).join(', ')})
}
async function saveRole() {
  if(!selectedRole.value) return
  busy.value=true; error.value=''
  try {
    await request(`/api/v1/system/roles/${selectedRole.value.id}/permissions`,{method:'PUT',body:JSON.stringify({permissions:roleForm.permissions,dataScope:roleForm.dataScope,projectGroups:splitValues(roleForm.projectGroups),scopeTags:splitValues(roleForm.scopeTags)})})
    message.value='角色权限与数据范围已保存'; await load()
  } catch(reason){ error.value=reason instanceof Error?reason.message:'角色保存失败' } finally { busy.value=false }
}
async function toggle(user:User) {
  busy.value=true
  try { await request(`/api/v1/system/users/${user.id}/toggle`,{method:'POST'}); await load() } catch(reason){ error.value=reason instanceof Error?reason.message:'用户状态切换失败' } finally { busy.value=false }
}
async function rebuildProjections() {
  busy.value=true; error.value=''; message.value=''
  try {
    const result=await request<ProjectionResult>('/api/v1/system/projections/rebuild',{method:'POST'})
    message.value=`拓扑投影已重建：资产 ${result.assets} 条，Neo4j ${result.neo4j} 条，OpenSearch ${result.openSearch} 条`
    projections.value=await request<ProjectionStatus>('/api/v1/system/projections')
  } catch(reason){ error.value=reason instanceof Error?reason.message:'拓扑投影重建失败' } finally { busy.value=false }
}
async function syncKeycloak() {
  busy.value=true; error.value=''; message.value=''
  try { const report=await request<SyncReport>('/api/v1/system/sync/keycloak',{method:'POST'}); message.value=`Keycloak 同步完成：用户 ${report.users}，新增 ${report.created}，更新 ${report.updated}，用户组 ${report.groups}`; if(report.errors?.length) error.value=report.errors.slice(0,5).join('；'); await load() }
  catch(reason){ error.value=reason instanceof Error?reason.message:'Keycloak 同步失败' } finally { busy.value=false }
}
onMounted(load)
</script>

<template>
  <section class="system-page">
    <header class="system-heading"><div><span>PLATFORM GOVERNANCE</span><h1>系统管理中心</h1><p>身份、权限、项目组数据范围、模块和审计统一治理</p></div><div><button :disabled="busy" @click="syncKeycloak"><RefreshCw :class="{spin:busy}"/>同步 Keycloak</button><button :disabled="busy" @click="load"><RefreshCw/>刷新配置</button></div></header>
    <div v-if="message" class="system-message">{{ message }}</div><div v-if="error" class="system-error">{{ error }}</div>
    <div class="system-summary"><article><Users/><span>平台用户<strong>{{summary?.users}}</strong><small>{{summary?.activeUsers}} 个正常</small></span></article><article><ShieldCheck/><span>角色数量<strong>{{summary?.roles}}</strong></span></article><article><Blocks/><span>启用模块<strong>{{summary?.modules}}</strong></span></article><article><FileClock/><span>今日审计<strong>{{summary?.auditsToday}}</strong></span></article></div>
    <div class="system-workspace"><aside><button v-for="item in [{id:'users',n:'用户与组织',i:Users},{id:'roles',n:'角色与数据权限',i:KeyRound},{id:'modules',n:'模块配置',i:Blocks},{id:'audits',n:'审计日志',i:FileClock},{id:'settings',n:'平台参数',i:Settings},{id:'ready',n:'生产就绪',i:ShieldCheck},{id:'projection',n:'拓扑投影',i:Waypoints}]" :key="item.id" :class="{active:tab===item.id}" @click="tab=item.id as typeof tab"><component :is="item.i"/>{{item.n}}</button></aside>
      <main>
        <template v-if="tab==='users'"><header><div><h2>用户与组织</h2><span>平台角色决定功能权限，项目组和标签决定资产可见范围</span></div></header><div class="system-table user-table"><div class="table-head"><span>用户</span><span>部门</span><span>角色</span><span>数据范围</span><span>项目组 / 标签</span><span>状态</span><span>操作</span></div><div v-for="u in users" :key="u.id"><span><strong>{{u.displayName}}</strong><small>{{u.username}}<template v-if="u.keycloakId"> · Keycloak</template></small></span><span>{{u.department||'-'}}</span><span><b v-for="r in u.roles" :key="r">{{r}}</b></span><span><em class="scope-pill">{{scopeLabel(u.dataScope)}}</em></span><span><small>{{(u.projectGroups||[]).join('、')||'未设置'}}<template v-if="u.scopeTags?.length"><br/>标签：{{u.scopeTags.join('、')}}</template></small></span><span><i :data-status="u.status">{{userStatusLabel(u.status)}}</i></span><span class="row-actions"><button @click="editUser(u)">编辑范围</button><button @click="toggle(u)"><ToggleLeft/>切换</button></span></div></div></template>

        <template v-else-if="tab==='roles'"><header><div><h2>角色与数据权限</h2><span>功能权限和数据范围可同时配置，用户的数据范围取角色与个人配置的并集</span></div></header><div class="role-grid"><button v-for="r in roles" :key="r.id" :class="{active:selectedRole?.id===r.id}" @click="selectRole(r)"><ShieldCheck/><div><strong>{{r.name}}</strong><span>{{r.description}}</span><small>{{r.users}} 个用户 · {{r.permissions.length}} 项权限 · {{scopeLabel(r.dataScope)}}</small></div></button></div><section v-if="selectedRole" class="permission-editor"><header><h3>{{selectedRole.name}}</h3><button class="primary" :disabled="busy" @click="saveRole"><Save/>保存角色</button></header><label v-for="permission in permissions" :key="permission"><input v-model="roleForm.permissions" type="checkbox" :value="permission">{{permission}}</label><div class="scope-form"><label><span>数据范围</span><select v-model="roleForm.dataScope"><option value="all">全部资产</option><option value="groups">项目组范围</option><option value="tags">标签范围</option><option value="none">无资产权限</option></select></label><label><span>项目组（逗号分隔）</span><input v-model="roleForm.projectGroups" placeholder="核心系统组, 电商业务组"></label><label><span>标签范围（逗号分隔）</span><input v-model="roleForm.scopeTags" placeholder="生产, 核心"></label></div></section></template>

        <template v-else-if="tab==='modules'"><header><div><h2>模块配置</h2><span>控制平台模块启用状态与菜单顺序</span></div></header><div class="module-grid"><article v-for="m in modules" :key="m.code"><Blocks/><div><strong>{{m.name}}</strong><span>{{m.code}} · {{m.permission}}</span></div><b>顺序 {{m.order}}</b><em>{{m.enabled?'已启用':'已停用'}}</em></article></div></template>
        <template v-else-if="tab==='audits'"><header><div><h2>审计日志</h2><span>记录关键管理与运维操作</span></div></header><div class="audit-list"><article v-for="a in audits" :key="a.id"><time>{{a.time}}</time><i :data-tone="auditTone(a.result)"></i><div><strong>{{a.actor}} · {{a.action}}</strong><span>{{a.resource}} · {{a.detail}}</span></div><code>{{a.ip}}</code><b>{{a.result}}</b></article></div></template>
        <template v-else-if="tab==='projection'"><header><div><h2>拓扑投影</h2><span>PostgreSQL 是唯一底账，Neo4j 与 OpenSearch 是可随时重建的副本</span></div><button class="primary" :disabled="busy" @click="rebuildProjections"><RefreshCw :class="{spin:busy}"/>重建投影</button></header><div class="projection-grid"><article :data-status="projections?.neo4jConfigured?'passed':'warning'"><i></i><div><strong>Neo4j 图数据库</strong><span>资产关系图 · {{projections?.neo4jUrl||'未配置'}}</span><small>已投影 {{projections?.neo4jAssets??0}} 个节点</small></div></article><article :data-status="projections?.openSearchConfigured?'passed':'warning'"><i></i><div><strong>OpenSearch 检索</strong><span>索引 cmdb-assets · {{projections?.openSearchUrl||'未配置'}}</span><small>已投影 {{projections?.openSearchAssets??0}} 个文档</small></div></article></div><p v-if="projections?.detail" class="readiness-time">探测异常：{{projections.detail}}</p></template><template v-else-if="tab==='ready'"><header><div><h2>生产就绪检查</h2><span>安全、数据、监控、发布和 AI 依赖统一体检</span></div><b>{{readiness?.status==='passed'?'可以上线':readiness?.status==='warning'?'存在提示':'存在阻断'}} · {{readiness?.score}} 分</b></header><div class="readiness-grid"><article v-for="item in readiness?.checks" :key="item.key" :data-status="item.status"><i></i><div><strong>{{item.name}}</strong><span>{{item.category}} · {{item.required?'必需':'可选'}}</span><small>{{item.detail}}</small></div></article></div><p class="readiness-time">检查时间：{{readiness?.generatedAt}} · 通过 {{readiness?.passed}} · 提示 {{readiness?.warning}} · 失败 {{readiness?.failed}}</p></template><template v-else><header><div><h2>平台参数</h2><span>系统运行、安全与数据保留策略</span></div></header><div class="setting-list"><label v-for="s in settings" :key="s.key"><div><b>{{s.category}}</b><strong>{{s.name}}</strong><span>{{s.description}}</span></div><input v-model="s.value"><code>{{s.key}}</code></label></div></template>
      </main>
    </div>

    <div v-if="editingUser" class="user-modal" @click.self="editingUser=undefined"><form @submit.prevent="saveUser"><header><div><span>USER DATA SCOPE</span><h3>{{editingUser.displayName}}</h3><p>{{editingUser.username}}</p></div><button type="button" @click="editingUser=undefined"><X/></button></header><div class="user-form"><label><span>显示名称</span><input v-model="userForm.displayName"></label><label><span>部门</span><input v-model="userForm.department"></label><label class="wide"><span>邮箱</span><input v-model="userForm.email"></label><label class="wide"><span>平台角色</span><div class="checks"><label v-for="role in roles" :key="role.id"><input v-model="userForm.roles" type="checkbox" :value="role.id">{{role.name}}</label></div></label><label><span>账号状态</span><select v-model="userForm.status"><option value="active">正常</option><option value="disabled">停用</option><option value="locked">锁定</option></select></label><label><span>数据范围</span><select v-model="userForm.dataScope"><option value="all">全部资产</option><option value="groups">项目组范围</option><option value="tags">标签范围</option><option value="none">无资产权限</option></select></label><label><span>项目组（逗号分隔）</span><input v-model="userForm.projectGroups" placeholder="核心系统组, 电商业务组"></label><label><span>标签范围（逗号分隔）</span><input v-model="userForm.scopeTags" placeholder="生产, 核心"></label></div><footer><button type="button" @click="editingUser=undefined">取消</button><button class="primary" :disabled="busy" type="submit">保存用户范围</button></footer></form></div>
  </section>
</template>

<style scoped>
.system-page{display:grid;gap:14px}.system-heading{display:flex;justify-content:space-between;align-items:end}.system-heading>div:first-child{display:grid;gap:3px}.system-heading span{color:#31c9eb;font-size:10px;letter-spacing:2px}.system-heading h1{margin:0;font-size:23px}.system-heading p{margin:0;color:#7890a8;font-size:11px}.system-heading>div:last-child{display:flex;gap:7px}.system-page button{display:inline-flex;align-items:center;gap:5px;border:1px solid #28536c;background:#0d2235;color:#b9d4e3;border-radius:6px;padding:7px 10px;font-size:11px;cursor:pointer}.system-page button.primary{background:#0b6da5;border-color:#1a93c8;color:#fff}.system-page button:disabled{opacity:.5}.system-page button svg{width:13px}.system-message,.system-error{padding:9px 11px;border-radius:6px;font-size:11px}.system-message{background:rgba(72,216,154,.1);color:#5bdfa1;border:1px solid rgba(72,216,154,.2)}.system-error{background:rgba(255,82,104,.1);color:#ff8998;border:1px solid rgba(255,82,104,.2)}.system-summary{display:grid;grid-template-columns:repeat(4,1fr);gap:9px}.system-summary article{display:flex;align-items:center;gap:10px;padding:12px;border:1px solid #173b55;background:#081a2b;border-radius:8px}.system-summary svg{width:20px;color:#31c9eb}.system-summary span{display:grid;color:#7890a8;font-size:10px}.system-summary strong{font-size:20px;color:#e3f3fb}.system-summary small{font-size:9px}.system-workspace{display:grid;grid-template-columns:210px 1fr;gap:12px}.system-workspace>aside{display:grid;align-content:start;gap:4px;padding:8px;border:1px solid #173b55;background:#081a2b;border-radius:8px}.system-workspace>aside button{justify-content:flex-start;background:transparent;border:0;color:#8099ae}.system-workspace>aside button.active{background:#0b3049;color:#67d8f2}.system-workspace main{min-width:0;border:1px solid #173b55;background:#081a2b;border-radius:8px;padding:14px}.system-workspace main>header{display:flex;justify-content:space-between;margin-bottom:12px}.system-workspace h2{margin:0;font-size:15px}.system-workspace header span{display:block;margin-top:3px;color:#7890a8;font-size:10px}.system-table{overflow:auto}.table-head,.system-table>div{display:grid;gap:9px;align-items:center;min-width:980px;padding:9px;border-bottom:1px solid #17324a;font-size:11px}.user-table .table-head,.user-table>div{grid-template-columns:1.2fr .8fr 1fr .7fr 1.2fr .6fr 1fr}.table-head{background:#0b2235;color:#7890a8}.system-table strong,.system-table small{display:block}.system-table small{margin-top:2px;color:#69889d;font-size:9px}.system-table b{display:inline-block;margin:1px;padding:2px 5px;background:#102f46;color:#7dd9f3;border-radius:4px;font-size:9px;font-weight:500}.system-table i{font-style:normal;font-size:10px;color:#5bdfa1}.system-table i[data-status=disabled],.system-table i[data-status=locked]{color:#ff9aa8}.scope-pill{padding:3px 6px;border-radius:4px;background:rgba(72,200,232,.12);color:#48c8e8}.row-actions{display:flex;gap:4px}.row-actions button{padding:4px 6px;font-size:9px}.role-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(230px,1fr));gap:8px}.role-grid>button{justify-content:flex-start;text-align:left;background:#0b2235}.role-grid>button.active{border-color:#31c9eb;background:#10334b}.role-grid svg{width:19px;color:#31c9eb}.role-grid strong,.role-grid span,.role-grid small{display:block}.role-grid span,.role-grid small{color:#7890a8;font-size:9px}.permission-editor{display:grid;grid-template-columns:repeat(3,1fr);gap:7px;margin-top:12px;padding:12px;border:1px solid #28536c;background:#10273a;border-radius:8px}.permission-editor header{grid-column:1/-1;display:flex;justify-content:space-between}.permission-editor>label{display:flex;align-items:center;gap:5px;font-size:10px}.scope-form{grid-column:1/-1;display:grid;grid-template-columns:1fr 1fr 1fr;gap:8px;padding-top:8px;border-top:1px solid #1b405a}.scope-form label{display:grid;gap:4px}.scope-form span{font-size:10px;color:#7890a8}.scope-form input,.scope-form select{height:32px;border:1px solid #28536c;background:#031525;color:#d5edf8;border-radius:5px;padding:0 7px}.module-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:8px}.module-grid article{display:grid;grid-template-columns:auto 1fr auto;gap:9px;align-items:center;padding:10px;border:1px solid #17415e;background:#061a2c}.module-grid strong,.module-grid span{display:block}.module-grid span{font-size:9px;color:#7890a8}.module-grid em{grid-column:3;font-style:normal;font-size:9px;color:#5bdfa1}.audit-list{display:grid;gap:5px}.audit-list article{display:grid;grid-template-columns:60px 8px 1fr 100px 60px;gap:8px;align-items:center;padding:8px;background:#061a2c;border-bottom:1px solid #17324a;font-size:10px}.audit-list time,.audit-list span{color:#7890a8}.audit-list i{width:7px;height:7px;border-radius:50%;background:#5bdfa1}.audit-list div strong,.audit-list div span{display:block}.setting-list{display:grid;gap:7px}.setting-list label{display:grid;grid-template-columns:1fr 220px 160px;gap:10px;align-items:center;padding:9px;background:#061a2c;border:1px solid #17415e}.setting-list strong,.setting-list span,.setting-list b{display:block}.setting-list span{font-size:9px;color:#7890a8}.setting-list b{font-size:9px;color:#31c9eb}.setting-list input{height:31px;border:1px solid #28536c;background:#031525;color:#d5edf8;padding:0 7px}.readiness-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:8px}.readiness-grid article{display:flex;gap:9px;padding:10px;border:1px solid #17415e;background:#061a2c}.readiness-grid i{width:8px;height:8px;border-radius:50%;margin-top:4px;background:#5bdfa1}.readiness-grid article[data-status=warning] i{background:#fbbf24}.readiness-grid article[data-status=failed] i{background:#fb7185}.readiness-grid strong,.readiness-grid span,.readiness-grid small{display:block}.readiness-grid span,.readiness-grid small{color:#7890a8;font-size:9px}.readiness-time{color:#7890a8;font-size:10px}.projection-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:8px}.projection-grid article{display:flex;gap:9px;padding:12px;border:1px solid #17415e;background:#061a2c}.projection-grid i{width:8px;height:8px;border-radius:50%;margin-top:4px;background:#5bdfa1}.projection-grid article[data-status=warning] i{background:#fbbf24}.projection-grid strong,.projection-grid span,.projection-grid small{display:block}.projection-grid span,.projection-grid small{color:#7890a8;font-size:9px}.setting-list code{color:#7890a8;font-size:9px}.user-modal{position:fixed;inset:0;z-index:90;display:grid;place-items:center;background:rgba(0,8,16,.78)}.user-modal form{width:min(720px,94vw);max-height:90vh;overflow:auto;background:#081a2b;border:1px solid #246c98;border-radius:10px}.user-modal form>header{display:flex;justify-content:space-between;padding:14px 18px;border-bottom:1px solid #174d78}.user-modal span{color:#31c9eb;font-size:9px}.user-modal h3{margin:3px 0}.user-modal p{margin:0;color:#7890a8;font-size:10px}.user-form{display:grid;grid-template-columns:1fr 1fr;gap:10px;padding:14px 18px}.user-form>label,.user-form label.wide{display:grid;gap:4px}.user-form label.wide{grid-column:1/-1}.user-form label>span{color:#7890a8}.user-form input,.user-form select{height:32px;border:1px solid #28536c;background:#031525;color:#d5edf8;padding:0 7px;border-radius:5px}.checks{display:grid;grid-template-columns:1fr 1fr;gap:5px}.checks label{display:flex;align-items:center;gap:5px}.user-modal footer{display:flex;justify-content:flex-end;gap:7px;padding:12px 18px;border-top:1px solid #174d78}.spin{animation:spin .9s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}@media(max-width:900px){.system-summary{grid-template-columns:1fr 1fr}.system-workspace{grid-template-columns:1fr}.permission-editor,.scope-form,.user-form{grid-template-columns:1fr}.scope-form,.user-form label.wide{grid-column:auto}}
</style>
