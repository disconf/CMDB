<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { CheckSquare, RefreshCw, Server, Settings2, X } from 'lucide-vue-next'
import { batchUpdateAssets, fetchAssetOptions } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'
import type { Asset, BatchUpdateAssetResult } from '@/features/cmdb/types'

const emit = defineEmits<{ close: []; changed: [] }>()
const auth = useAuthStore()
const assets = ref<Asset[]>([])
const hosts = ref<Asset[]>([])
const selectedIDs = ref<string[]>([])
const query = ref('')
const typeFilter = ref('')
const busy = ref(false)
const error = ref('')
const message = ref('')
const result = ref<BatchUpdateAssetResult>()
const form = reactive({ status:'', environment:'', projectGroup:'', owner:'', location:'', hostId:'', tagsText:'', tagsMode:'replace' as 'replace'|'append' })

const filtered = computed(() => assets.value.filter(asset => {
  const text = `${asset.name} ${asset.id} ${asset.ip} ${asset.owner} ${asset.projectGroup}`.toLowerCase()
  return (!query.value || text.includes(query.value.toLowerCase())) && (!typeFilter.value || asset.type === typeFilter.value)
}))
const selectedAssets = computed(() => assets.value.filter(asset => selectedIDs.value.includes(asset.id)))
const vmCount = computed(() => selectedAssets.value.filter(asset => asset.type === 'virtual-machine').length)
const allVisibleSelected = computed(() => filtered.value.length > 0 && filtered.value.every(asset => selectedIDs.value.includes(asset.id)))

function toggleAll(){ if(allVisibleSelected.value){ const visible=new Set(filtered.value.map(a=>a.id)); selectedIDs.value=selectedIDs.value.filter(id=>!visible.has(id)) } else { selectedIDs.value=Array.from(new Set([...selectedIDs.value,...filtered.value.map(a=>a.id)])) } }
function toggle(id:string){ selectedIDs.value=selectedIDs.value.includes(id)?selectedIDs.value.filter(item=>item!==id):[...selectedIDs.value,id] }
function typeLabel(type:string){ return ({'physical-server':'物理机','virtual-machine':'虚拟机','k8s-node':'K8s 节点'} as Record<string,string>)[type]||type }
async function load(){busy.value=true;error.value='';try{[assets.value,hosts.value]=await Promise.all([fetchAssetOptions(auth.token,['physical-server','virtual-machine','k8s-node']),fetchAssetOptions(auth.token,['physical-server','virtualization-cluster'])])}catch(reason){error.value=reason instanceof Error?reason.message:'服务器列表加载失败'}finally{busy.value=false}}
async function submit(){
  if(!selectedIDs.value.length){error.value='请至少选择一台服务器';return}
  if(form.hostId && vmCount.value===0){error.value='宿主机只适用于虚拟机，请选择虚拟机后再指定宿主机';return}
  busy.value=true;error.value='';message.value='';result.value=undefined
  try{
    result.value=await batchUpdateAssets(auth.token,{assetIds:selectedIDs.value,status:form.status,environment:form.environment,projectGroup:form.projectGroup,owner:form.owner,location:form.location,hostId:form.hostId,tags:form.tagsText.split('|').map(v=>v.trim()).filter(Boolean),tagsMode:form.tagsMode})
    message.value=`批量配置完成：成功 ${result.value.succeeded} 台，失败 ${result.value.failed} 台`
    if(result.value.failed) error.value=result.value.items.filter(i=>!i.success).slice(0,6).map(i=>`${i.assetName||i.assetId}: ${i.error}`).join('；')
    emit('changed')
  }catch(reason){error.value=reason instanceof Error?reason.message:'批量配置失败'}finally{busy.value=false}
}
onMounted(load)
</script>

<template>
  <div class="batch-backdrop" @click.self="emit('close')">
    <section class="batch-panel">
      <header><div><span>SERVER BATCH CONFIGURATION</span><h2>服务器批量配置</h2><p>未填写的字段保持不变；宿主机关系只应用到选中的虚拟机。</p></div><button aria-label="关闭" @click="emit('close')"><X/></button></header>
      <div v-if="message" class="batch-message">{{message}}</div><div v-if="error" class="batch-error">{{error}}</div>
      <div class="batch-layout">
        <aside class="server-picker">
          <div class="picker-tools"><label><Server/><input v-model="query" placeholder="搜索服务器"><select v-model="typeFilter"><option value="">全部类型</option><option value="physical-server">物理机</option><option value="virtual-machine">虚拟机</option><option value="k8s-node">K8s 节点</option></select></label><button :disabled="busy" @click="load"><RefreshCw :class="{spin:busy}"/></button></div>
          <div class="picker-head"><label><input type="checkbox" :checked="allVisibleSelected" @change="toggleAll"><span>全选当前 {{filtered.length}} 台</span></label><b>已选 {{selectedIDs.length}} 台</b></div>
          <div class="server-list"><button v-for="asset in filtered" :key="asset.id" :class="{active:selectedIDs.includes(asset.id)}" @click="toggle(asset.id)"><input type="checkbox" :checked="selectedIDs.includes(asset.id)" @click.stop="toggle(asset.id)"><span><strong>{{asset.name}}</strong><small>{{asset.ip}} · {{typeLabel(asset.type)}} · {{asset.environment}}</small></span></button><div v-if="!filtered.length" class="batch-empty">暂无匹配服务器</div></div>
        </aside>
        <main class="batch-form">
          <div class="form-title"><Settings2/><div><strong>公共配置</strong><span>批量更新服务器公用字段</span></div></div>
          <div class="form-grid">
            <label><span>环境</span><select v-model="form.environment"><option value="">保持不变</option><option>生产</option><option>测试</option><option>开发</option><option>预生产</option></select></label>
            <label><span>状态</span><select v-model="form.status"><option value="">保持不变</option><option value="online">在线</option><option value="warning">异常</option><option value="offline">离线</option></select></label>
            <label><span>项目组</span><input v-model="form.projectGroup" placeholder="保持原项目组"></label>
            <label><span>负责人</span><input v-model="form.owner" placeholder="保持原负责人"></label>
            <label class="wide"><span>位置</span><input v-model="form.location" placeholder="物理机填写机房位置；虚拟机可留空并继承宿主机"></label>
            <label class="wide"><span>宿主机（仅虚拟机）</span><select v-model="form.hostId"><option value="">不修改宿主机关系</option><option v-for="host in hosts" :key="host.id" :value="host.id">{{host.name}} · {{host.typeName}} · {{host.ip||host.location}}</option></select></label>
            <label class="wide"><span>标签（使用 | 分隔）</span><input v-model="form.tagsText" placeholder="例如：核心|生产"></label>
            <label><span>标签处理方式</span><select v-model="form.tagsMode"><option value="replace">覆盖原标签</option><option value="append">追加到原标签</option></select></label>
            <div class="selection-note">已选择 {{selectedIDs.length}} 台，其中虚拟机 {{vmCount}} 台。</div>
          </div>
          <footer><button class="ghost" @click="emit('close')">取消</button><button class="primary" :disabled="busy||!selectedIDs.length" @click="submit"><CheckSquare/>{{busy?'配置中…':'应用批量配置'}}</button></footer>
        </main>
      </div>
    </section>
  </div>
</template>

<style scoped>
.batch-backdrop{position:fixed;inset:0;z-index:90;background:rgba(0,8,16,.8);display:grid;place-items:center}.batch-panel{width:min(1120px,96vw);height:min(760px,92vh);display:grid;grid-template-rows:auto auto auto 1fr;background:#061a2c;border:1px solid #246c98;border-radius:12px;overflow:hidden}.batch-panel>header{display:flex;justify-content:space-between;padding:14px 18px;border-bottom:1px solid #174d78}.batch-panel header span{font-size:9px;letter-spacing:1.5px;color:#31c9eb}.batch-panel h2{margin:4px 0;font-size:18px}.batch-panel p{margin:0;color:#7890a8;font-size:10px}.batch-panel header button{border:0;background:transparent;color:#8aa6b9;cursor:pointer}.batch-message,.batch-error{margin:8px 18px 0;padding:8px 10px;border-radius:6px;font-size:10px}.batch-message{background:rgba(72,216,154,.1);color:#5bdfa1}.batch-error{background:rgba(255,82,104,.1);color:#ff8998}.batch-layout{min-height:0;display:grid;grid-template-columns:420px 1fr}.server-picker{min-height:0;display:grid;grid-template-rows:auto auto 1fr;border-right:1px solid #174d78}.picker-tools{display:grid;grid-template-columns:1fr 36px;gap:7px;padding:10px}.picker-tools label{display:grid;grid-template-columns:16px 1fr 100px;align-items:center;gap:6px;height:34px;border:1px solid #28536c;background:#031525;padding:0 8px}.picker-tools svg{width:14px;color:#648397}.picker-tools input{min-width:0;border:0;outline:0;background:transparent;color:#d5edf8}.picker-tools select{border:0;background:#071e32;color:#91afc1}.picker-tools>button{display:grid;place-items:center;border:1px solid #28536c;background:#0b263b;color:#a9c6d9;cursor:pointer}.picker-head{display:flex;justify-content:space-between;align-items:center;padding:8px 12px;border-top:1px solid #173d58;border-bottom:1px solid #173d58;color:#7890a8;font-size:9px}.picker-head label{display:flex;align-items:center;gap:5px}.picker-head b{font-weight:500;color:#48c8e8}.server-list{min-height:0;overflow:auto;padding:6px}.server-list>button{width:100%;display:grid;grid-template-columns:18px 1fr;gap:7px;align-items:center;text-align:left;border:1px solid transparent;background:transparent;color:#9eb9c9;padding:8px;cursor:pointer}.server-list>button:hover,.server-list>button.active{border-color:#1a6389;background:#082841}.server-list span{display:grid;gap:3px}.server-list strong{color:#d9eef8;font-weight:500;font-size:10px}.server-list small{color:#648397;font-size:9px}.batch-form{min-height:0;display:grid;grid-template-rows:auto 1fr auto;padding:14px 18px}.form-title{display:flex;align-items:center;gap:8px}.form-title svg{color:#31c9eb}.form-title strong,.form-title span{display:block}.form-title span{color:#7890a8;font-size:9px;margin-top:3px}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px;align-content:start;margin-top:14px}.form-grid label{display:grid;gap:5px}.form-grid label.wide{grid-column:1/-1}.form-grid span{font-size:10px;color:#7890a8}.form-grid input,.form-grid select{height:34px;border:1px solid #28536c;background:#031525;color:#d5edf8;padding:0 8px;border-radius:6px}.selection-note{display:flex;align-items:center;color:#7f9aae;font-size:10px;border:1px dashed #28536c;padding:9px}.batch-form footer{display:flex;justify-content:flex-end;gap:8px;padding-top:12px;border-top:1px solid #173d58}.batch-form button{display:flex;align-items:center;gap:5px;border:1px solid #28536c;background:#0b263b;color:#bdd8e9;padding:8px 11px;border-radius:6px;cursor:pointer}.batch-form button.primary{background:#087b9e}.batch-form button.ghost{background:transparent}.batch-form button:disabled{opacity:.45}.batch-empty{padding:28px;text-align:center;color:#648397;font-size:10px}.spin{animation:spin 1s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}@media(max-width:1000px){.batch-layout{grid-template-columns:1fr}.server-picker{max-height:300px;border-right:0;border-bottom:1px solid #174d78}.form-grid{grid-template-columns:1fr}.form-grid label.wide{grid-column:auto}}
</style>
