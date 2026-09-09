<script setup lang="ts">
import {onMounted,reactive,ref} from 'vue'
import {useRoute} from 'vue-router'
import {Boxes,ChevronLeft,ChevronRight,Database,Download,Filter,Plus,RefreshCw,Search,Server,Tag,X,History,Pencil} from 'lucide-vue-next'
import {useCmdbStore} from '@/stores/useCmdbStore'
import {statusLabel} from '@/features/cmdb/cmdbModel'
import AssetEditor from './AssetEditor.vue'
import AssetImport from './AssetImport.vue'
import ModelManager from './ModelManager.vue'
import CollectedAttributes from './CollectedAttributes.vue'
import HostMonitoring from './HostMonitoring.vue'
import type {AssetInput,ImportResult} from '@/features/cmdb/types'

const store=useCmdbStore()
const route=useRoute()
const filters=reactive({search:'',type:'',status:'',projectGroup:'',page:1,pageSize:20})
const editorMode=ref<'create'|'edit'|''>('')
const showImport=ref(false)
const showModels=ref(false)
const importResult=ref<ImportResult>()
const showAnalytics=ref(false)
async function openAnalytics(){await store.loadAnalytics();showAnalytics.value=true}
function apply(){filters.page=1;void store.search({...filters})}
function chooseType(type:string){filters.type=filters.type===type?'':type;apply()}
function page(delta:number){const next=filters.page+delta;if(next>0&&next<=store.meta.totalPages){filters.page=next;void store.search({...filters})}}
async function save(input:AssetInput){if(editorMode.value==='create')await store.create(input,{...filters});else if(store.selected)await store.update(store.selected.id,input,{...filters});editorMode.value=''}
async function importRows(rows:AssetInput[],upsert:boolean){importResult.value=await store.importRows(rows,upsert,{...filters})}
onMounted(async()=>{await store.initialize({...filters});const asset=typeof route.query.asset==='string'?route.query.asset:'';if(asset)await store.select(asset)})
</script>

<template>
  <section class="cmdb-page">
    <header class="cmdb-heading"><div><span>CONFIGURATION MANAGEMENT DATABASE</span><h1>CMDB 资产中心</h1><p>统一管理资产模型、生命周期、归属关系与数据来源</p></div><div><button @click="openAnalytics">数据报表</button><button @click="showModels=true"><Boxes/>模型管理</button><button @click="showImport=true"><Download/>批量导入</button><button class="primary" @click="editorMode='create'"><Plus/>新增资产</button></div></header>
    <div v-if="store.summary" class="cmdb-summary">
      <article><Database/><div><span>资产总数</span><strong>{{store.summary.total}}</strong></div></article><article><i class="online"></i><div><span>在线资产</span><strong>{{store.summary.online}}</strong></div></article><article><i class="warning"></i><div><span>异常资产</span><strong>{{store.summary.warning}}</strong></div></article><article><i class="offline"></i><div><span>离线资产</span><strong>{{store.summary.offline}}</strong></div></article><article><Boxes/><div><span>CI 模型</span><strong>{{store.summary.models}}</strong></div></article><article><Tag/><div><span>项目组</span><strong>{{store.summary.projectGroups}}</strong></div></article>
    </div>
    <div class="cmdb-workspace">
      <aside class="model-tree"><header><strong>CI 模型</strong><span>{{store.models.length}}</span></header><button :class="{active:!filters.type}" @click="chooseType('')"><Boxes/><span>全部资产</span><b>{{store.summary?.total}}</b></button><button v-for="model in store.models" :key="model.code" :class="{active:filters.type===model.code}" @click="chooseType(model.code)"><Server/><span>{{model.name}}</span><b>{{model.count}}</b></button></aside>
      <main class="asset-register">
        <div class="asset-toolbar"><div class="search-box"><Search/><input v-model="filters.search" placeholder="搜索资产名称、编号或 IP" @keyup.enter="apply"><button @click="apply">搜索</button></div><select v-model="filters.status" @change="apply"><option value="">全部状态</option><option value="online">在线</option><option value="warning">异常</option><option value="offline">离线</option></select><select v-model="filters.projectGroup" @change="apply"><option value="">全部项目组</option><option>核心系统组</option><option>电商业务组</option><option>容器平台组</option><option>基础设施组</option><option>数据平台组</option><option>研发效能组</option></select><button class="icon-button" aria-label="刷新" @click="store.search({...filters})"><RefreshCw/></button><button class="icon-button" aria-label="筛选"><Filter/></button></div>
        <div v-if="store.error" class="cmdb-error">{{store.error}}</div>
        <div class="asset-table"><div class="asset-row asset-head"><span>资产名称</span><span>类型</span><span>状态</span><span>IP 地址</span><span>环境</span><span>项目组</span><span>负责人</span><span>数据来源</span></div><button v-for="asset in store.assets" :key="asset.id" class="asset-row" @click="store.select(asset.id)"><span><Server/><i><strong>{{asset.name}}</strong><small>{{asset.id}}</small></i></span><span>{{asset.typeName}}</span><span><b class="status-pill" :data-status="asset.status">{{statusLabel(asset.status)}}</b></span><span class="mono">{{asset.ip}}</span><span>{{asset.environment}}</span><span>{{asset.projectGroup}}</span><span>{{asset.owner}}</span><span>{{asset.source}}</span></button><div v-if="store.isLoading" class="table-loading">正在读取资产数据…</div></div>
        <footer class="table-footer"><span>共 {{store.meta.total}} 条资产</span><div><button :disabled="store.meta.page<=1" @click="page(-1)"><ChevronLeft/></button><b>{{store.meta.page}} / {{store.meta.totalPages||1}}</b><button :disabled="store.meta.page>=store.meta.totalPages" @click="page(1)"><ChevronRight/></button></div></footer>
      </main>
    </div>
    <div v-if="store.selected" class="drawer-backdrop" @click.self="store.closeDetail"><aside class="asset-drawer"><header><div><span>资产详情</span><h2>{{store.selected.name}}</h2><small>{{store.selected.id}}</small></div><div class="drawer-actions"><button aria-label="编辑" @click="editorMode='edit'"><Pencil/></button><button aria-label="关闭" @click="store.closeDetail"><X/></button></div></header><section class="asset-identity"><Server/><div><strong>{{store.selected.typeName}}</strong><span><b class="status-pill" :data-status="store.selected.status">{{statusLabel(store.selected.status)}}</b> {{store.selected.environment}}</span></div></section><section><h3>基础信息</h3><dl><div><dt>IP 地址</dt><dd>{{store.selected.ip}}</dd></div><div><dt>负责人</dt><dd>{{store.selected.owner}}</dd></div><div><dt>项目组</dt><dd>{{store.selected.projectGroup}}</dd></div><div><dt>位置</dt><dd>{{store.selected.location}}</dd></div><div><dt>数据来源</dt><dd>{{store.selected.source}}</dd></div><div><dt>最后发现</dt><dd>{{store.selected.lastSeenAt}}</dd></div></dl></section><section><h3>标签</h3><div class="asset-tags"><span v-for="tag in store.selected.tags" :key="tag">{{tag}}</span></div></section><section><h3><History/>变更历史</h3><div class="history-list"><div v-if="!store.history.length">暂无人工变更</div><article v-for="entry in store.history" :key="entry.id"><strong>{{entry.action==='created'?'创建资产':'更新资产'}}</strong><span>{{entry.operator}} · {{entry.occurredAt}}</span><small v-for="change in entry.changes" :key="change.field">{{change.field}}：{{change.before}} → {{change.after}}</small></article></div></section></aside></div>
    <CollectedAttributes v-if="store.selected" :attributes="store.selected.attributes"/>
    <HostMonitoring v-if="store.selected" :asset-id="store.selected.id"/>
    <AssetEditor v-if="editorMode" :asset="editorMode==='edit'?store.selected:undefined" :models="store.models" @close="editorMode=''" @save="save"/>
    <AssetImport v-if="showImport" :result="importResult" :models="store.models" @close="showImport=false" @import="importRows"/>
    <div v-if="showAnalytics" class="modal-backdrop"><section class="analytics-panel"><header><div><span>REPORTS</span><h2>CMDB 数据报表</h2></div><button @click="showAnalytics=false">✕</button></header><div class="analytics-body"><article><h3>资产状态</h3><dl><dt>总资产</dt><dd>{{store.analytics?.status.total}}</dd><dt>在线</dt><dd>{{store.analytics?.status.online}}</dd><dt>异常</dt><dd>{{store.analytics?.status.warning}}</dd><dt>离线</dt><dd>{{store.analytics?.status.offline}}</dd></dl></article><article><h3>按类型</h3><ul><li v-for="t in store.analytics?.byType" :key="t.code">{{t.name}}（{{t.count}}）</li></ul></article><article><h3>按项目组</h3><ul><li v-for="g in store.analytics?.byGroup" :key="g.group">{{g.group}}（{{g.count}}）</li></ul></article><article><h3>按来源</h3><ul><li v-for="s in store.analytics?.bySource" :key="s.source">{{s.source}}（{{s.count}}）</li></ul></article><article class="wide"><h3>字段完整度</h3><ul><li v-for="f in store.analytics?.completeness" :key="f.field">{{f.label}}：缺失 {{f.missing}} / {{f.total}}（{{Math.round((f.rate)*100)}}%）</li></ul></article></div></section></div>
<ModelManager v-if="showModels" :models="store.models" @close="showModels=false" @changed="store.initialize({...filters})"/>
  </section>
</template>
