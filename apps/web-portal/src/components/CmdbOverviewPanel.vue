<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Activity, Boxes, Database, ExternalLink, Network, Plus, RefreshCw, Server, ShieldAlert } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'
import type { Asset, AssetSummary, CiModel } from '@/features/cmdb/types'

const props = defineProps<{ summary?: AssetSummary; models: CiModel[]; assets: Asset[]; loading?: boolean }>()
const emit = defineEmits<{ navigate:[section:'overview'|'k8s'|'middleware'|'database'|'network'|'servers'|'relations'|'monitor'|'rancher'|'config'|'assets']; add:[] }>()
const auth = useAuthStore()
const monitorCount = ref(0)
const monitorPending = ref(0)
const rancher = ref<{ configured:boolean; url?:string }>({ configured:false })
const modelCount = (code:string) => props.models.find(item => item.code === code)?.count || 0
const serverCount = computed(() => modelCount('physical-server') + modelCount('virtual-machine') + modelCount('k8s-node'))
const healthyRate = computed(() => props.summary?.total ? Math.round((props.summary.online / props.summary.total) * 100) : 0)
async function fetchJson<T>(path:string):Promise<T>{const response=await fetch(path,{headers:{Authorization:`Bearer ${auth.token}`}});if(!response.ok)throw new Error(`请求失败 (${response.status})`);return response.json() as Promise<T>}
async function loadContext(){try{const [targets,config]=await Promise.all([fetchJson<Array<{status:string}>>('/api/v1/cmdb/monitor-targets'),fetchJson<{configured:boolean;url?:string}>('/api/v1/cmdb/rancher/config')]);monitorCount.value=targets.length;monitorPending.value=targets.filter(item=>item.status==='pending').length;rancher.value=config}catch{}}
onMounted(loadContext)
</script>

<template>
  <section class="asset-overview">
    <div class="overview-hero">
      <div><span>ASSET CONTROL PLANE</span><h2>资产底账、关系拓扑与监控联动</h2><p>CMDB 只维护资产和关系事实，K8s 交给 Rancher，指标与告警计算交给 Prometheus。</p></div>
      <div class="hero-health" :style="{'--health': healthyRate+'%'}"><strong>{{healthyRate}}%</strong><span>资产健康率</span></div>
    </div>
    <div class="overview-metrics">
      <button @click="emit('navigate','k8s')"><Boxes/><span>K8s 集群<strong>{{modelCount('k8s-cluster')}}</strong></span></button>
      <button @click="emit('navigate','middleware')"><Activity/><span>中间件实例<strong>{{modelCount('middleware')}}</strong></span></button>
      <button @click="emit('navigate','database')"><Database/><span>数据库实例<strong>{{modelCount('database')}}</strong></span></button>
      <button @click="emit('navigate','network')"><Network/><span>网络设备<strong>{{modelCount('network-device')}}</strong></span></button>
      <button @click="emit('navigate','servers')"><Server/><span>服务器节点<strong>{{serverCount}}</strong></span></button>
    </div>
    <div class="overview-grid">
      <section class="overview-panel">
        <header><div><h3>资产健康分布</h3><span>按 CMDB 当前状态统计</span></div><ShieldAlert/></header>
        <div class="health-bars">
          <article><i class="online"></i><span>正常</span><strong>{{summary?.online||0}}</strong></article>
          <article><i class="warning"></i><span>异常</span><strong>{{summary?.warning||0}}</strong></article>
          <article><i class="offline"></i><span>未知/离线</span><strong>{{summary?.offline||0}}</strong></article>
        </div>
      </section>
      <section class="overview-panel">
        <header><div><h3>监控目标</h3><span>由 CMDB 动态下发给 Prometheus</span></div><Activity/></header>
        <div class="context-number"><strong>{{monitorCount}}</strong><span>已登记目标</span><b v-if="monitorPending">{{monitorPending}} 个待配置 Exporter</b><b v-else>全部目标已配置</b></div>
        <button class="panel-link" @click="emit('navigate','monitor')">查看监控目标 <ExternalLink/></button>
      </section>
      <section class="overview-panel">
        <header><div><h3>Rancher 集成</h3><span>K8s 集群与资源统一入口</span></div><Boxes/></header>
        <div class="rancher-state"><strong>{{rancher.configured?'已配置':'待配置'}}</strong><span>{{rancher.configured?rancher.url:'配置 RANCHER_URL 与 Token 后启用同步和跳转'}}</span></div>
        <button class="panel-link" @click="emit('navigate','rancher')">进入 Rancher 工作区 <ExternalLink/></button>
      </section>
      <section class="overview-panel recent-assets">
        <header><div><h3>最近资产</h3><span>当前读取模型中的最新资产</span></div><RefreshCw :class="{spin:loading}"/></header>
        <button v-for="asset in assets.slice(0,6)" :key="asset.id" @click="emit('navigate','assets')"><span><strong>{{asset.name}}</strong><small>{{asset.typeName}} · {{asset.ip||'无 IP'}}</small></span><b :data-status="asset.status">{{asset.status}}</b></button>
        <div v-if="!assets.length" class="empty-overview">暂无资产数据</div>
      </section>
    </div>
    <div class="overview-actions"><button class="primary" @click="emit('add')"><Plus/>新增资产</button><button @click="emit('navigate','config')">导入与模型配置</button></div>
  </section>
</template>

<style scoped>
.asset-overview{display:grid;gap:14px}.overview-hero{display:flex;justify-content:space-between;align-items:center;padding:20px;border:1px solid #16445c;border-radius:14px;background:linear-gradient(135deg,#061a29,#082638)}.overview-hero span{color:#22d3ee;font:10px/1.2 monospace;letter-spacing:.15em}.overview-hero h2{margin:7px 0;font-size:20px}.overview-hero p{margin:0;color:#7f9fb2}.hero-health{display:grid;place-items:center;width:92px;height:92px;border-radius:50%;background:radial-gradient(circle,#071b2f 56%,transparent 58%),conic-gradient(#22d3ee var(--health,0%),#14344a 0);color:#dffbff}.hero-health strong{font-size:20px}.hero-health span{color:#6f91a5}.overview-metrics{display:grid;grid-template-columns:repeat(5,1fr);gap:10px}.overview-metrics button{display:grid;grid-template-columns:auto 1fr;align-items:center;gap:11px;padding:15px;border:1px solid #173b55;border-radius:12px;background:#0b273c;color:#dcecff;text-align:left;cursor:pointer}.overview-metrics button:hover{border-color:#22d3ee}.overview-metrics svg{width:22px;color:#22d3ee}.overview-metrics span{display:grid;gap:5px;color:#7894a8;font-size:10px}.overview-metrics strong{color:white;font-size:22px}.overview-grid{display:grid;grid-template-columns:1fr 1fr;gap:12px}.overview-panel{padding:14px;border:1px solid #173b55;border-radius:12px;background:#081f31}.overview-panel>header{display:flex;justify-content:space-between;align-items:center;margin-bottom:10px}.overview-panel h3{margin:0;font-size:13px}.overview-panel header span{display:block;margin-top:3px;color:#6f91a5;font-size:9px}.overview-panel header svg{width:18px;color:#22d3ee}.health-bars{display:grid;grid-template-columns:repeat(3,1fr);gap:8px}.health-bars article{display:grid;grid-template-columns:auto 1fr auto;align-items:center;gap:7px;padding:10px;background:#061620;color:#8fb2c4;font-size:10px}.health-bars i{width:8px;height:8px;border-radius:50%}.health-bars i.online{background:#2dd4bf}.health-bars i.warning{background:#f59e0b}.health-bars i.offline{background:#64748b}.health-bars strong{color:white;font-size:16px}.context-number{display:flex;align-items:baseline;gap:8px}.context-number strong{font-size:30px;color:#67e8f9}.context-number span{color:#8fb2c4}.context-number b{margin-left:auto;color:#fbbf24;font-size:10px}.panel-link{display:flex;align-items:center;gap:6px;margin-top:10px;border:0;background:transparent;color:#67e8f9;cursor:pointer}.panel-link svg{width:14px}.rancher-state{display:grid;gap:5px}.rancher-state strong{color:#a5f3fc}.rancher-state span{color:#6f91a5;font-size:10px;line-height:1.6}.recent-assets button{display:flex;justify-content:space-between;width:100%;padding:8px 0;border:0;border-bottom:1px solid #123043;background:transparent;color:#dcecff;text-align:left;cursor:pointer}.recent-assets button span{display:grid;gap:3px}.recent-assets small{color:#6f91a5}.recent-assets b{color:#2dd4bf;font-size:10px}.recent-assets b[data-status=warning]{color:#fbbf24}.recent-assets b[data-status=offline]{color:#fb7185}.empty-overview{padding:24px;text-align:center;color:#64748b}.overview-actions{display:flex;gap:8px;justify-content:flex-end}.overview-actions button{display:flex;align-items:center;gap:6px;padding:9px 12px;border:1px solid #23455f;background:#0b263b;color:#bdd8e9;cursor:pointer}.overview-actions button.primary{background:#087b9e}@media(max-width:1200px){.overview-metrics{grid-template-columns:repeat(2,1fr)}.overview-grid{grid-template-columns:1fr}}
</style>