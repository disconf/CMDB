<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Activity, BellRing, BookOpen, Bot, Building2, ChevronDown, ClipboardCheck, Database, FileUp, Gauge, LayoutDashboard, LogOut, Maximize2, Network, Play, Radar, RadioTower, Rocket, Search, Settings, ShieldAlert, ShieldCheck, Terminal, TicketCheck, Wrench } from 'lucide-vue-next'
import { useRoute, useRouter } from 'vue-router'
import GlobalCommandCenter from '@/components/GlobalCommandCenter.vue'
import LoginPage from '@/components/LoginPage.vue'
import ModulePage from '@/components/ModulePage.vue'
import CmdbPage from '@/components/CmdbPage.vue'
import DiscoveryPage from '@/components/DiscoveryPage.vue'
import RemoteAccessPage from '@/components/RemoteAccessPage.vue'
import IdcManager from '@/components/IdcManager.vue'
import TopologyPage from '@/components/TopologyPage.vue'
import MonitorPage from '@/components/MonitorPage.vue'
import JobsPage from '@/components/JobsPage.vue'
import TicketsPage from '@/components/TicketsPage.vue'
import ReleasesPage from '@/components/ReleasesPage.vue'
import ToolboxPage from '@/components/ToolboxPage.vue'
import AiopsPage from '@/components/AiopsPage.vue'
import SystemPage from '@/components/SystemPage.vue'
import KnowledgeCenter from '@/components/KnowledgeCenter.vue'
import { useDashboardStore } from '@/stores/useDashboardStore'
import { useAuthStore } from '@/stores/useAuthStore'
import { formatUpdateTime } from '@/features/dashboard/dashboardModel'
import { remoteSections, resolveRemoteSection } from '@/features/remote/remoteNavigation'
import { clearAuthFragment } from '@/auth/keycloak'

const store = useDashboardStore(); const auth=useAuthStore(); const route=useRoute(); const router=useRouter()
const navigationIcons={LayoutDashboard,Database,Building2,Network,Radar,BellRing,ClipboardCheck,TicketCheck,Rocket,Wrench,Bot,Settings,Terminal,BookOpen}
const remoteSectionIcons={overview:LayoutDashboard,sessions:Terminal,files:FileUp,execution:Play,approvals:ShieldAlert,security:ShieldCheck}
const remoteExpanded=ref(route.path.startsWith('/remote'))
const remoteSection=computed(()=>resolveRemoteSection(route.path))
watch([()=>auth.isReady,()=>auth.isAuthenticated],([ready,authenticated])=>{if(ready&&authenticated)refresh();else if(ready&&!authenticated&&route.path!=='/login')void router.replace('/login')})
function openModule(item:{code:string;path:string}){if(item.code==='remote'){if(route.path.startsWith(item.path)){remoteExpanded.value=!remoteExpanded.value;return}remoteExpanded.value=true;void router.push('/remote/overview');return}void router.push(item.path)}
const currentTime = computed(()=>store.overview ? formatUpdateTime(store.overview.updatedAt) : '--')
const sourceOnline = computed(()=>store.overview?.sources.filter(item=>item.status==='live').length ?? 0)
const platformHealthy = computed(()=>store.overview?.command.platformStatus !== 'attention')
let timer: ReturnType<typeof setInterval> | undefined; let controller: AbortController | undefined
function refresh(){ controller?.abort(); controller=new AbortController(); void store.load(controller.signal) }
async function fullscreen(){ if(!document.fullscreenElement) await document.documentElement.requestFullscreen(); else await document.exitFullscreen() }
onMounted(()=>{ clearAuthFragment(); if(auth.isReady&&auth.isAuthenticated) refresh(); timer=setInterval(()=>{if(auth.isReady&&auth.isAuthenticated)refresh()},30000) }); onBeforeUnmount(()=>{controller?.abort(); if(timer) clearInterval(timer)})
async function signOut(){await auth.signOut();await router.replace('/login')}
</script>

<template>
  <LoginPage v-if="route.path==='/login'" />
  <main v-else-if="!auth.isReady" class="auth-loading"><Activity class="spin"/><span>正在验证登录状态…</span></main>
  <div v-else-if="auth.isAuthenticated" class="app-shell">
    <aside class="sidebar"><div class="brand"><span class="brand-mark"><Gauge /></span><strong>智能运维中心</strong></div><nav aria-label="主导航"><template v-for="item in auth.modules" :key="item.code"><button :class="{active:item.code==='remote'?route.path.startsWith(item.path):route.path===item.path}" @click="openModule(item)"><component :is="navigationIcons[item.icon as keyof typeof navigationIcons]" /><span>{{item.name}}</span><ChevronDown v-if="item.code==='remote'" class="nav-chevron" :class="{expanded:remoteExpanded}"/></button><div v-if="item.code==='remote' && remoteExpanded" class="sidebar-subnav"><button v-for="child in remoteSections" :key="child.id" :class="{active:remoteSection===child.id}" @click="router.push(child.path)"><component :is="remoteSectionIcons[child.id]"/><span>{{child.shortName}}</span></button></div></template></nav><div class="system-status" :data-status="platformHealthy?'healthy':'warning'"><i></i><div><strong>{{store.overview ? (platformHealthy?'系统运行稳定':'存在关注事件') : '正在读取平台状态'}}</strong><span>{{store.overview ? sourceOnline+' / '+store.overview.sources.length+' 个数据源在线' : '等待真实数据源响应'}}</span></div></div></aside>
    <main>
      <header class="topbar"><div class="scope"><span class="status" :data-status="platformHealthy?'healthy':'warning'"><i></i>{{store.overview ? (platformHealthy?'运行稳定':'需关注') : '状态同步中'}}</span><span>权限数据域</span><button>{{store.overview?.assets.projectGroups ?? 0}} 个项目组 <ChevronDown /></button></div><div class="top-actions"><span class="clock">{{currentTime}}</span><button aria-label="搜索"><Search /></button><button aria-label="全屏" @click="fullscreen"><Maximize2 /></button><button class="avatar">{{auth.user?.displayName.slice(0,1)}}</button><span>{{auth.user?.displayName}}</span><button class="logout-action" aria-label="退出登录" @click="signOut"><LogOut /></button></div></header>
      <div v-if="store.error" class="error-banner">{{store.error}} <button @click="refresh">重新加载</button></div>
      <GlobalCommandCenter v-if="route.path==='/' && store.overview" :overview="store.overview" :loading="store.isLoading" @refresh="refresh" />
      <section v-else-if="route.path==='/'" class="dashboard command-loading"><div class="loading-state"><Activity class="spin"/><span>{{store.error ? '全局态势暂时无法加载' : '正在汇聚真实运维数据…'}}</span></div></section>
      <CmdbPage v-else-if="route.path==='/cmdb'" />
      <IdcManager v-else-if="route.path==='/idc'" />
      <DiscoveryPage v-else-if="route.path==='/discovery'" />
      <RemoteAccessPage v-else-if="route.path.startsWith('/remote')" :section="remoteSection" />
      <TopologyPage v-else-if="route.path==='/topology'" />
      <MonitorPage v-else-if="route.path==='/monitor'" />
      <JobsPage v-else-if="route.path==='/jobs'" />
      <TicketsPage v-else-if="route.path==='/tickets'" />
      <ReleasesPage v-else-if="route.path==='/releases'" />
      <ToolboxPage v-else-if="route.path==='/toolbox'" />
      <AiopsPage v-else-if="route.path==='/aiops'" /><KnowledgeCenter v-else-if="route.path==='/knowledge'" />
      <SystemPage v-else-if="route.path==='/system'" />
      <ModulePage v-else />
    </main>
  </div>
</template>
