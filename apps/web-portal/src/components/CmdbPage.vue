<script setup lang="ts">
import {computed,onMounted,reactive,ref} from 'vue'
import {useRoute,useRouter} from 'vue-router'
import {Activity,Boxes,Cable,ChevronLeft,ChevronRight,Database,Download,ExternalLink,Filter,GitFork,HardDrive,LayoutDashboard,Layers3,Network,Plus,RefreshCw,Search,Server,Settings,ShieldCheck,Tag,Waypoints,X,History,Pencil} from 'lucide-vue-next'
import {useCmdbStore} from '@/stores/useCmdbStore'
import {statusLabel} from '@/features/cmdb/cmdbModel'
import AssetEditor from './AssetEditor.vue'
import ServerBatchConfig from './ServerBatchConfig.vue'
import AssetImport from './AssetImport.vue'
import AssetGovernancePanel from './AssetGovernancePanel.vue'
import ModelManager from './ModelManager.vue'
import CollectedAttributes from './CollectedAttributes.vue'
import HostMonitoring from './HostMonitoring.vue'
import K8sAssetBrowser from './K8sAssetBrowser.vue'
import CmdbOverviewPanel from './CmdbOverviewPanel.vue'
import MonitorTargetsPanel from './MonitorTargetsPanel.vue'
import RancherPanel from './RancherPanel.vue'
import VMwarePanel from './VMwarePanel.vue'
import CloudPanel from './CloudPanel.vue'
import NetworkInventoryPanel from './NetworkInventoryPanel.vue'
import NetworkInterfacesPanel from './NetworkInterfacesPanel.vue'
import ServiceDiscoveryPanel from './ServiceDiscoveryPanel.vue'
import type {AssetInput,ImportResult} from '@/features/cmdb/types'

const store=useCmdbStore()
const route=useRoute()
const router=useRouter()
const filters=reactive({search:'',type:'',status:'',projectGroup:'',page:1,pageSize:20})
const editorMode=ref<'create'|'edit'|''>('')
const view=ref<'assets'|'k8s'>('assets')
const section=ref<'overview'|'assets'|'k8s'|'middleware'|'database'|'network'|'governance'|'servers'|'storage'|'security'|'rancher'|'vmware'|'cloud'|'relations'|'monitor'|'config'>('overview')
const resourceSections:string[]=['assets','k8s','middleware','database','network','servers','storage','security']
const regularModels=computed(()=>store.models.filter(model=>!['physical-server','virtual-machine','k8s-node','k8s-cluster','k8s-namespace','k8s-workload','k8s-pod','k8s-service','k8s-ingress','k8s-pvc','k8s-pv','k8s-storageclass','network-device','database','middleware','load-balancer','storage-device','security-device'].includes(model.code)))
const editorSaving=ref(false)
const editorError=ref('')
const showImport=ref(false)
const showServerBatch=ref(false)
const showNetworkLinkEditor=ref(false)
const showModels=ref(false)
const importResult=ref<ImportResult>()
const showAnalytics=ref(false)
async function openAnalytics(){await store.loadAnalytics();showAnalytics.value=true}
function apply(){filters.page=1;void store.search({...filters})}
function chooseType(type:string){section.value=type==='network-device'?'network':'assets';view.value='assets';filters.type=filters.type===type?'':type;apply()}
function chooseServers(){section.value='assets';view.value='assets';filters.type='server';apply()}
function chooseK8s(){section.value='k8s';view.value='k8s'}
function openSection(value:typeof section.value){section.value=value;if(value==='k8s'){view.value='k8s';return}if(value==='assets'||value==='middleware'||value==='database'||value==='network'||value==='servers'||value==='storage'||value==='security'){view.value='assets';filters.type=value==='assets'?'':value==='network'?'network-device':value==='servers'?'server':value==='storage'?'storage-device':value==='security'?'security-device':value;apply()}}
function page(delta:number){const next=filters.page+delta;if(next>0&&next<=store.meta.totalPages){filters.page=next;void store.search({...filters})}}
async function save(input:AssetInput){editorSaving.value=true;editorError.value='';try{if(editorMode.value==='create')await store.create(input,{...filters});else if(store.selected)await store.update(store.selected.id,input,{...filters});editorMode.value=''}catch(reason){editorError.value=reason instanceof Error?reason.message:'保存失败，请检查字段或机房U位占用'}finally{editorSaving.value=false}}
async function importRows(rows:AssetInput[],upsert:boolean){importResult.value=await store.importRows(rows,upsert,{...filters})}
onMounted(async()=>{await store.initialize({...filters});await store.loadIdcRooms();const asset=typeof route.query.asset==='string'?route.query.asset:'';if(asset)await store.select(asset)})
</script>

<template>
  <section class="cmdb-page">
    <header class="cmdb-heading"><div><span>CONFIGURATION MANAGEMENT DATABASE</span><h1>CMDB 资产中心</h1><p>统一管理资产模型、生命周期、归属关系与数据来源</p></div><div><button @click="openAnalytics">数据报表</button>
          <button @click="showModels=true"><Boxes/>模型管理</button><button @click="showImport=true"><Download/>批量导入</button><button class="primary" @click="editorMode='create';editorError=''"><Plus/>新增资产</button></div></header>
    <nav class="cmdb-domain-nav"><button :class="{active:section==='overview'}" @click="openSection('overview')"><LayoutDashboard/>资产总览</button><button :class="{active:resourceSections.includes(section)}" @click="openSection('assets')"><Boxes/>资产清单</button><button :class="{active:section==='governance'}" @click="openSection('governance')"><ShieldCheck/>责任治理</button><button :class="{active:section==='relations'}" @click="openSection('relations')"><Waypoints/>关系拓扑</button><button :class="{active:section==='monitor'}" @click="openSection('monitor')"><Activity/>监控联动</button><button :class="{active:section==='vmware'}" @click="openSection('vmware')"><Server/>虚拟化平台</button><button :class="{active:section==='cloud'}" @click="openSection('cloud')"><Server/>云资源</button><button :class="{active:section==='config'}" @click="openSection('config')"><Settings/>系统配置</button></nav>
    <CmdbOverviewPanel v-if="section==='overview'" :summary="store.summary" :models="store.models" :assets="store.assets" :loading="store.isLoading" @navigate="openSection" @add="editorMode='create';editorError=''"/>
    <MonitorTargetsPanel v-if="section==='monitor'"/>
    <RancherPanel v-if="section==='rancher'"/>
    <VMwarePanel v-if="section==='vmware'"/>
    <CloudPanel v-if="section==='cloud'"/>
    <section v-if="section==='relations'" class="cmdb-domain-panel"><header><Waypoints/><div><h2>资产关系与物理拓扑</h2><p>关系骨架来自 CMDB，状态着色与影响分析结合监控数据。</p></div></header><div class="domain-action-grid"><article><strong>应用与服务依赖</strong><span>应用 → 中间件 → 数据库 → 主机 → 交换机</span><button @click="router.push('/topology')">打开拓扑中心 <ExternalLink/></button></article><article><strong>故障影响范围</strong><span>按项目组、环境、机房筛选并查看上下游影响路径</span><button @click="router.push('/topology')">查询影响范围 <ExternalLink/></button></article><article><strong>物理链路</strong><span>物理机网卡 → 交换机端口 → LLDP 邻居关系</span><button @click="openSection('network')">查看网络设备</button></article></div></section>
    <section v-if="section==='config'" class="cmdb-domain-panel"><header><Settings/><div><h2>CMDB 系统配置</h2><p>管理资产模型、数据源、导入导出和机房模型。</p></div></header><div class="domain-action-grid"><article><strong>资产模型</strong><span>自定义资产类型、属性和关系定义</span><button @click="showModels=true">打开模型管理</button></article><article><strong>机房建模</strong><span>机房、模块、机柜、U位与电压信息</span><button @click="router.push('/idc')">打开机房管理</button></article><article><strong>批量导入</strong><span>CSV 模板导入、更新和错误反馈</span><button @click="showImport=true">打开导入</button></article><article><strong>数据源</strong><span>Rancher、VMware、SNMP、Agent、SSH 和 Exporter 配置</span><button @click="openSection('rancher')">Rancher 集成</button><button @click="openSection('vmware')">VMware 集成</button></article></div><ServiceDiscoveryPanel/></section>
    <div v-if="resourceSections.includes(section)&&store.summary" class="cmdb-summary">
      <article><Database/><div><span>资产总数</span><strong>{{store.summary.total}}</strong></div></article><article><i class="online"></i><div><span>在线资产</span><strong>{{store.summary.online}}</strong></div></article><article><i class="warning"></i><div><span>异常资产</span><strong>{{store.summary.warning}}</strong></div></article><article><i class="offline"></i><div><span>离线资产</span><strong>{{store.summary.offline}}</strong></div></article><article><Boxes/><div><span>CI 模型</span><strong>{{store.summary.models}}</strong></div></article><article><Tag/><div><span>项目组</span><strong>{{store.summary.projectGroups}}</strong></div></article>
    </div>

    <AssetGovernancePanel v-if="section==='governance'" @select="store.select" @changed="store.search({...filters})" />
    <div v-if="resourceSections.includes(section)" class="cmdb-workspace">
      <aside class="model-tree"><header><strong>CI 模型 / 资源视图</strong><span>{{store.models.length}}</span></header><button :class="{active:view==='assets'&&!filters.type}" @click="chooseType('')"><Boxes/><span>全部资产</span><b>{{store.summary?.total}}</b></button><button :class="{active:view==='assets'&&filters.type==='server'}" @click="chooseServers"><Server/><span>服务器</span></button><button :class="{active:view==='k8s'}" @click="chooseK8s"><Boxes/><span>K8s 集群</span></button><button :class="{active:view==='assets'&&filters.type==='middleware'}" @click="chooseType('middleware')"><Layers3/><span>中间件</span></button><button :class="{active:view==='assets'&&filters.type==='database'}" @click="chooseType('database')"><Database/><span>数据库</span></button><button :class="{active:view==='assets'&&filters.type==='network-device'}" @click="chooseType('network-device')"><Network/><span>网络设备</span></button><button :class="{active:view==='assets'&&filters.type==='storage-device'}" @click="chooseType('storage-device')"><HardDrive/><span>存储设备</span></button><button :class="{active:view==='assets'&&filters.type==='security-device'}" @click="chooseType('security-device')"><ShieldCheck/><span>安全设备</span></button><button :class="{active:view==='assets'&&filters.type==='load-balancer'}" @click="chooseType('load-balancer')"><GitFork/><span>负载均衡</span></button><button v-for="model in regularModels" :key="model.code" :class="{active:view==='assets'&&filters.type===model.code}" @click="chooseType(model.code)"><Server/><span>{{model.name}}</span><b>{{model.count}}</b></button></aside>
      <main class="asset-register"><NetworkInventoryPanel v-if="section==='network'"/><K8sAssetBrowser v-else-if="view==='k8s'" @select="store.select"/><template v-else>
        <div class="asset-toolbar"><div class="search-box"><Search/><input v-model="filters.search" placeholder="搜索资产名称、编号或 IP" @keyup.enter="apply"><button @click="apply">搜索</button></div><select v-model="filters.status" @change="apply"><option value="">全部状态</option><option value="online">在线</option><option value="warning">异常</option><option value="offline">离线</option></select><select v-model="filters.projectGroup" @change="apply"><option value="">全部项目组</option><option>核心系统组</option><option>电商业务组</option><option>容器平台组</option><option>基础设施组</option><option>数据平台组</option><option>研发效能组</option></select><button class="icon-button" aria-label="刷新" @click="store.search({...filters})"><RefreshCw/></button><button class="icon-button" aria-label="筛选"><Filter/></button><button v-if="filters.type==='server'" class="batch-config-button" @click="showServerBatch=true">批量配置</button></div>
        <div v-if="store.error" class="cmdb-error">{{store.error}}</div>
        <div class="asset-table"><div class="asset-row asset-head"><span>资产名称</span><span>类型</span><span>状态</span><span>IP 地址</span><span>环境</span><span>项目组</span><span>负责人</span><span>数据来源</span></div><button v-for="asset in store.assets" :key="asset.id" class="asset-row" @click="store.select(asset.id)"><span><Server/><i><strong>{{asset.name}}</strong><small>{{asset.id}}</small></i></span><span>{{asset.typeName}}</span><span><b class="status-pill" :data-status="asset.status">{{statusLabel(asset.status)}}</b></span><span class="mono">{{asset.ip}}</span><span>{{asset.environment}}</span><span>{{asset.projectGroup}}</span><span>{{asset.owner}}</span><span>{{asset.source}}</span></button><div v-if="store.isLoading" class="table-loading">正在读取资产数据…</div></div>
        <footer class="table-footer"><span>共 {{store.meta.total}} 条资产</span><div><button :disabled="store.meta.page<=1" @click="page(-1)"><ChevronLeft/></button><b>{{store.meta.page}} / {{store.meta.totalPages||1}}</b><button :disabled="store.meta.page>=store.meta.totalPages" @click="page(1)"><ChevronRight/></button></div></footer></template>
      </main>
    </div>
    <div v-if="store.selected" class="drawer-backdrop" @click.self="store.closeDetail"><aside class="asset-drawer"><header><div><span>资产详情</span><h2>{{store.selected.name}}</h2><small>{{store.selected.id}}</small></div><div class="drawer-actions"><button v-if="['physical-server','network-device','load-balancer'].includes(store.selected.type)" class="link-action" @click="showNetworkLinkEditor=true"><Cable/>{{store.selected.type==='physical-server'?'关联交换机端口':'交换机互联绑定'}}</button><button aria-label="编辑" @click="editorMode='edit';editorError=''"><Pencil/></button><button aria-label="关闭" @click="showNetworkLinkEditor=false;store.closeDetail()"><X/></button></div></header><section class="asset-identity"><Server/><div><strong>{{store.selected.typeName}}</strong><span><b class="status-pill" :data-status="store.selected.status">{{statusLabel(store.selected.status)}}</b> {{store.selected.environment}}</span></div></section><section><h3>基础信息</h3><dl><div><dt>IP 地址</dt><dd>{{store.selected.ip}}</dd></div><div><dt>负责人</dt><dd>{{store.selected.owner}}</dd></div><div><dt>项目组</dt><dd>{{store.selected.projectGroup}}</dd></div><div><dt>位置</dt><dd>{{store.selected.location}}</dd></div><div><dt>数据来源</dt><dd>{{store.selected.source}}</dd></div><div><dt>最后发现</dt><dd>{{store.selected.lastSeenAt}}</dd></div></dl></section><section><h3>标签</h3><div class="asset-tags"><span v-for="tag in store.selected.tags" :key="tag">{{tag}}</span></div></section><section><h3><History/>变更历史</h3><div class="history-list"><div v-if="!store.history.length">暂无人工变更</div><article v-for="entry in store.history" :key="entry.id"><strong>{{entry.action==='created'?'创建资产':'更新资产'}}</strong><span>{{entry.operator}} · {{entry.occurredAt}}</span><small v-for="change in entry.changes" :key="change.field">{{change.field}}：{{change.before}} → {{change.after}}</small></article></div></section></aside></div>
    <div v-if="showNetworkLinkEditor && store.selected" class="network-link-modal" @click.self="showNetworkLinkEditor=false"><section class="network-link-dialog"><header><div><Cable/><span><strong>关联交换机端口</strong><small>{{store.selected.name}} · {{store.selected.ip}}</small></span></div><button aria-label="关闭" @click="showNetworkLinkEditor=false"><X/></button></header><NetworkInterfacesPanel :asset-id="store.selected.id"/></section></div>
    <CollectedAttributes v-if="store.selected" :attributes="store.selected.attributes"/>
    <HostMonitoring v-if="store.selected" :asset-id="store.selected.id"/>
    <ServerBatchConfig v-if="showServerBatch" @close="showServerBatch=false" @changed="store.search({...filters})" />
    <AssetEditor v-if="editorMode" :asset="editorMode==='edit'?store.selected:undefined" :models="store.models" :idc-rooms="store.idcRooms" @close="editorMode='';editorError=''" @save="save" :saving="editorSaving" :server-error="editorError"/>
    <AssetImport v-if="showImport" :result="importResult" :models="store.models" @close="showImport=false" @import="importRows"/>
    <div v-if="showAnalytics" class="modal-backdrop"><section class="analytics-panel"><header><div><span>REPORTS</span><h2>CMDB 数据报表</h2></div><button @click="showAnalytics=false">✕</button></header><div class="analytics-body"><article><h3>资产状态</h3><dl><dt>总资产</dt><dd>{{store.analytics?.status.total}}</dd><dt>在线</dt><dd>{{store.analytics?.status.online}}</dd><dt>异常</dt><dd>{{store.analytics?.status.warning}}</dd><dt>离线</dt><dd>{{store.analytics?.status.offline}}</dd></dl></article><article><h3>按类型</h3><ul><li v-for="t in store.analytics?.byType" :key="t.code">{{t.name}}（{{t.count}}）</li></ul></article><article><h3>按项目组</h3><ul><li v-for="g in store.analytics?.byGroup" :key="g.group">{{g.group}}（{{g.count}}）</li></ul></article><article><h3>按来源</h3><ul><li v-for="s in store.analytics?.bySource" :key="s.source">{{s.source}}（{{s.count}}）</li></ul></article><article class="wide"><h3>字段完整度</h3><ul><li v-for="f in store.analytics?.completeness" :key="f.field">{{f.label}}：缺失 {{f.missing}} / {{f.total}}（{{Math.round((f.rate)*100)}}%）</li></ul></article></div></section></div>

<ModelManager v-if="showModels" :models="store.models" @close="showModels=false" @changed="store.initialize({...filters})"/>
  </section>
</template>

<style scoped>
.analytics-body { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px,1fr)); gap: 14px; max-height: 60vh; overflow:auto; }
.analytics-body article { background: var(--panel, #0d1526); border:1px solid var(--line,#24314d); border-radius:10px; padding:12px 14px; }
.analytics-body article.wide { grid-column: 1 / -1; }
.analytics-body h3 { margin: 0 0 8px; font-size: 13px; letter-spacing:.5px; color: var(--muted,#7f92b0); }
.analytics-body dl { display:grid; grid-template-columns: repeat(2,1fr); gap:6px 12px; margin:0; }
.analytics-body dt { color:#7f92b0; font-size:12px; }
.analytics-body dd { margin:0; font-weight:600; font-size:14px; }
.analytics-body ul { margin:0; padding:0; list-style:none; }
.analytics-body li { padding:4px 0; border-bottom:1px dashed var(--line,#24314d); font-size:13px; }
.analytics-panel { width: min(920px, 94vw); }
.analytics-panel header { display:flex; align-items:center; justify-content:space-between; }
</style>


<style scoped>
.batch-config-button { height:34px; padding:0 12px; border:1px solid #1a93c8; background:#0b6da5; color:#fff; cursor:pointer; border-radius:4px; }
</style>



<style scoped>
.link-action { height:30px; padding:0 8px; border:1px solid #1a93c8; background:#0b6da5; color:#fff; cursor:pointer; display:flex; align-items:center; gap:5px; font-size:10px; }
.link-action svg { width:13px; }
.network-link-modal { position:fixed; inset:0; z-index:60; background:rgba(0,8,16,.78); display:grid; place-items:center; }
.network-link-dialog { width:min(860px,94vw); max-height:88vh; overflow:auto; background:#061a2c; border:1px solid #246c98; border-radius:12px; box-shadow:0 25px 80px rgba(0,0,0,.45); }
.network-link-dialog > header { display:flex; justify-content:space-between; align-items:center; padding:14px 18px; border-bottom:1px solid #174d78; }
.network-link-dialog > header > div { display:flex; align-items:center; gap:9px; }
.network-link-dialog > header svg { width:18px; color:#31c9eb; }
.network-link-dialog > header span { display:grid; gap:3px; }
.network-link-dialog > header strong { font-size:13px; }
.network-link-dialog > header small { font-size:9px; color:#7890a8; }
.network-link-dialog > header button { border:0; background:transparent; color:#8aa6b9; cursor:pointer; }
</style>
