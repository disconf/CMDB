<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted } from 'vue'
import { Activity, BellRing, Bot, Box, ChevronDown, ClipboardCheck, Database, Gauge, LayoutDashboard, LogOut, Maximize2, Network, Radar, RadioTower, Rocket, Search, Server, Settings, Terminal, TicketCheck, Wrench } from 'lucide-vue-next'
import { useRoute, useRouter } from 'vue-router'
import DashboardPanel from '@/components/DashboardPanel.vue'
import LoginPage from '@/components/LoginPage.vue'
import ModulePage from '@/components/ModulePage.vue'
import CmdbPage from '@/components/CmdbPage.vue'
import DiscoveryPage from '@/components/DiscoveryPage.vue'
import RemoteAccessPage from '@/components/RemoteAccessPage.vue'
import TopologyPage from '@/components/TopologyPage.vue'
import MonitorPage from '@/components/MonitorPage.vue'
import JobsPage from '@/components/JobsPage.vue'
import TicketsPage from '@/components/TicketsPage.vue'
import ReleasesPage from '@/components/ReleasesPage.vue'
import ToolboxPage from '@/components/ToolboxPage.vue'
import AiopsPage from '@/components/AiopsPage.vue'
import SystemPage from '@/components/SystemPage.vue'
import TrendChart from '@/components/TrendChart.vue'
import { useDashboardStore } from '@/stores/useDashboardStore'
import { useAuthStore } from '@/stores/useAuthStore'
import { formatUpdateTime } from '@/features/dashboard/dashboardModel'

const store = useDashboardStore(); const auth=useAuthStore(); const route=useRoute(); const router=useRouter()
const iconMap = { Database, RadioTower, BellRing, ClipboardCheck }
const navigationIcons={LayoutDashboard,Database,Network,Radar,BellRing,ClipboardCheck,TicketCheck,Rocket,Wrench,Bot,Settings,Terminal}
const currentTime = computed(()=>store.overview ? formatUpdateTime(store.overview.updatedAt) : '--')
let timer: ReturnType<typeof setInterval> | undefined; let controller: AbortController | undefined
function refresh(){ controller?.abort(); controller=new AbortController(); void store.load(controller.signal) }
async function fullscreen(){ if(!document.fullscreenElement) await document.documentElement.requestFullscreen(); else await document.exitFullscreen() }
onMounted(()=>{ refresh(); timer=setInterval(refresh,30000) }); onBeforeUnmount(()=>{controller?.abort(); if(timer) clearInterval(timer)})
async function signOut(){await auth.signOut();await router.replace('/login')}
</script>

<template>
  <LoginPage v-if="route.path==='/login'" />
  <div v-else class="app-shell">
    <aside class="sidebar"><div class="brand"><span class="brand-mark"><Gauge /></span><strong>智能运维中心</strong></div><nav aria-label="主导航"><button v-for="item in auth.modules" :key="item.code" :class="{active:route.path===item.path}" @click="router.push(item.path)"><component :is="navigationIcons[item.icon as keyof typeof navigationIcons]" /><span>{{item.name}}</span></button></nav><div class="system-status"><i></i><div><strong>系统运行正常</strong><span>12 个核心服务在线</span></div></div></aside>
    <main>
      <header class="topbar"><div class="scope"><span class="status"><i></i>运行正常</span><span>数据中心</span><button>华东 · 上海 <ChevronDown /></button></div><div class="top-actions"><span class="clock">{{currentTime}}</span><button aria-label="搜索"><Search /></button><button aria-label="全屏" @click="fullscreen"><Maximize2 /></button><button class="avatar">{{auth.user?.displayName.slice(0,1)}}</button><span>{{auth.user?.displayName}}</span><button class="logout-action" aria-label="退出登录" @click="signOut"><LogOut /></button></div></header>
      <div v-if="store.error" class="error-banner">{{store.error}} <button @click="refresh">重新加载</button></div>
      <section v-if="route.path==='/'" class="dashboard" :class="{loading:store.isLoading && !store.overview}">
        <div class="metrics"><article v-for="card in store.metricCards" :key="card.label" class="metric-card" :data-tone="card.tone"><div class="metric-icon"><component :is="iconMap[card.icon as keyof typeof iconMap]" /></div><div><span>{{card.label}}</span><strong>{{card.value}}</strong><small>{{card.note}}</small></div></article></div>
        <template v-if="store.overview">
          <div class="dashboard-grid">
            <div class="left-stack">
              <DashboardPanel title="数据中心容量"><div class="capacity-list"><div v-for="item in store.overview.capacity" :key="item.name" class="capacity-item"><div><span>{{item.name}}</span><strong>{{item.value}}%</strong></div><div class="progress"><i :style="{width:item.value+'%'}"></i></div><small>{{item.detail}}</small></div></div></DashboardPanel>
              <DashboardPanel title="资源健康度"><div class="health-grid"><div v-for="item in store.overview.health" :key="item.name" class="health-ring" :style="{'--value':item.value+'%'}"><div><strong>{{item.value}}%</strong><small>健康</small></div><span>{{item.name}}</span></div></div></DashboardPanel>
            </div>
            <DashboardPanel title="基础设施拓扑总览" action="拓扑详情"><div class="topology"><div class="topology-title">项目 <b>→</b> 应用 <b>→</b> 服务 <b>→</b> 实例 <b>→</b> 基础设施</div><div v-for="(layer,index) in store.overview.topology.layers" :key="layer.name" class="topology-layer"><strong>{{layer.name}}</strong><div class="topology-nodes"><div v-for="node in layer.nodes" :key="node.id" class="topology-node" :data-status="node.status"><Box v-if="index<4"/><Server v-else/><span>{{node.name}}</span></div></div></div><div class="topology-legend"><span><i class="healthy"></i>健康</span><span><i class="warning"></i>关注</span><span><i class="critical"></i>严重</span></div></div></DashboardPanel>
            <div class="right-stack">
              <DashboardPanel title="告警趋势（近24小时）" action="详情"><TrendChart :points="store.overview.alertTrend" /></DashboardPanel>
              <DashboardPanel title="高优先级告警" action="全部告警"><div class="alert-table"><div class="table-head"><span>级别</span><span>告警名称</span><span>对象</span><span>时间</span></div><div v-for="alert in store.overview.alerts" :key="alert.id" class="table-row"><span><b :class="alert.level">{{alert.level==='critical'?'严重':'高危'}}</b></span><span>{{alert.title}}</span><span>{{alert.target}}</span><span>{{alert.occurredAt}}</span></div></div></DashboardPanel>
            </div>
            <DashboardPanel title="活动任务" action="任务中心"><div class="job-table"><div v-for="job in store.overview.jobs" :key="job.id" class="job-row"><div><strong>{{job.name}}</strong><span>{{job.type}} · {{job.owner}}</span></div><div class="job-progress"><i :style="{width:job.progress+'%'}"></i></div><b>{{job.progress}}%</b><span :class="job.status">{{job.status==='running'?'执行中':'排队中'}}</span></div></div></DashboardPanel>
            <DashboardPanel title="运维操作时间线" action="查看更多"><div class="timeline"><div v-for="event in store.overview.timeline" :key="event.id" class="timeline-row"><time>{{event.time}}</time><i :data-type="event.type"></i><span>{{event.message}}</span><small>{{event.source}}</small></div></div></DashboardPanel>
            <DashboardPanel title="发布状态" action="发布中心"><div class="deploy-summary"><div class="deploy-ring"><strong>{{store.overview.deployments.length}}</strong><span>今日发布</span></div><div class="deploy-stats"><span><i class="success"></i>发布成功 <b>{{store.overview.deployments.filter(x=>x.status==='success').length}}</b></span><span><i class="running"></i>发布中 <b>{{store.overview.deployments.filter(x=>x.status==='running').length}}</b></span><span><i class="failed"></i>发布失败 <b>{{store.overview.deployments.filter(x=>x.status==='failed').length}}</b></span></div></div><div class="deploy-list"><div v-for="item in store.overview.deployments" :key="item.name"><span>{{item.name}} {{item.version}}</span><time>{{item.updatedAt}}</time><b :class="item.status">{{item.status==='success'?'发布成功':item.status==='running'?'发布中':'发布失败'}}</b></div></div></DashboardPanel>
          </div>
        </template>
        <div v-else class="loading-state"><Activity class="spin"/>正在汇聚全局运维数据…</div>
      </section>
      <CmdbPage v-else-if="route.path==='/cmdb'" />
      <DiscoveryPage v-else-if="route.path==='/discovery'" />
      <RemoteAccessPage v-else-if="route.path==='/remote'" />
      <TopologyPage v-else-if="route.path==='/topology'" />
      <MonitorPage v-else-if="route.path==='/monitor'" />
      <JobsPage v-else-if="route.path==='/jobs'" />
      <TicketsPage v-else-if="route.path==='/tickets'" />
      <ReleasesPage v-else-if="route.path==='/releases'" />
      <ToolboxPage v-else-if="route.path==='/toolbox'" />
      <AiopsPage v-else-if="route.path==='/aiops'" />
      <SystemPage v-else-if="route.path==='/system'" />
      <ModulePage v-else />
    </main>
  </div>
</template>
