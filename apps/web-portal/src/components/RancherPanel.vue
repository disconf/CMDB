<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Boxes, ExternalLink, RefreshCw, Settings } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'

type Cluster = { id:string; name:string; state:string; kubernetesVersion:string; nodeCount:number; dashboardUrl:string }
const auth = useAuthStore()
const config = ref<{ configured:boolean; url?:string }>({ configured:false })
const clusters = ref<Cluster[]>([])
const loading = ref(false)
const syncing = ref(false)
const error = ref('')
const message = ref('')
const headers = () => ({ Authorization:`Bearer ${auth.token}`, 'Content-Type':'application/json' })
async function request<T>(path:string, init?:RequestInit){const response=await fetch(path,{...init,headers:headers()});const body=await response.json().catch(()=>({message:'请求失败'}));if(!response.ok)throw new Error((body as {message?:string}).message||`请求失败 (${response.status})`);return body as T}
async function load(){loading.value=true;error.value='';try{config.value=await request<{configured:boolean;url?:string}>('/api/v1/cmdb/rancher/config');if(config.value.configured)clusters.value=await request<Cluster[]>('/api/v1/cmdb/rancher/clusters')}catch(reason){error.value=reason instanceof Error?reason.message:'Rancher 数据加载失败'}finally{loading.value=false}}
async function sync(){syncing.value=true;error.value='';message.value='';try{const result=await request<{clusters:number;nodes:number;namespaces:number;workloads:number}>('/api/v1/cmdb/rancher/sync',{method:'POST'});message.value=`已同步集群 ${result.clusters} 个、节点 ${result.nodes} 个、命名空间 ${result.namespaces} 个、工作负载 ${result.workloads} 个`;await load()}catch(reason){error.value=reason instanceof Error?reason.message:'Rancher 同步失败'}finally{syncing.value=false}}
function open(url:string){if(url)window.open(url,'_blank','noopener,noreferrer')}
onMounted(load)
</script>

<template>
  <section class="rancher-panel">
    <header><div><span>RANCHER KUBERNETES CONTROL PLANE</span><h2>K8s 集群入口</h2><p>CMDB 不重复实现 K8s 控制台，统一通过 Rancher 查看和同步集群资产。</p></div><div><button :disabled="loading" @click="load"><RefreshCw :class="{spin:loading}"/>刷新</button><button class="primary" :disabled="syncing||!config.configured" @click="sync"><Boxes/>同步集群</button></div></header>
    <div v-if="error" class="rancher-error">{{error}}</div><div v-if="message" class="rancher-message">{{message}}</div>
    <div v-if="!config.configured" class="rancher-empty"><Settings/><strong>Rancher 尚未配置</strong><span>配置 <code>RANCHER_URL</code> 和 <code>RANCHER_TOKEN</code> 后，可同步集群并一键跳转 Rancher 控制台。</span></div>
    <template v-else>
      <div class="rancher-config"><span>Rancher 地址</span><strong>{{config.url}}</strong><b>已连接配置</b></div>
      <div class="rancher-clusters"><article v-for="cluster in clusters" :key="cluster.id"><div><strong>{{cluster.name}}</strong><span>{{cluster.id}} · {{cluster.kubernetesVersion||'版本未知'}}</span></div><div class="cluster-meta"><b :data-state="cluster.state">{{cluster.state}}</b><span>{{cluster.nodeCount}} 节点</span></div><button @click="open(cluster.dashboardUrl)">打开 Rancher <ExternalLink/></button></article><div v-if="!clusters.length" class="rancher-empty">Rancher 未返回集群，点击“同步集群”重新读取。</div></div>
    </template>
  </section>
</template>

<style scoped>
.rancher-panel{padding:18px;border:1px solid #173b55;border-radius:12px;background:#071925}.rancher-panel>header{display:flex;justify-content:space-between;gap:16px;align-items:flex-start}.rancher-panel header span{color:#22d3ee;font:10px/1.2 monospace;letter-spacing:.15em}.rancher-panel h2{margin:5px 0}.rancher-panel p{margin:0;color:#7894a8;font-size:11px}.rancher-panel header>div:last-child{display:flex;gap:8px}.rancher-panel button{display:flex;align-items:center;gap:6px;padding:8px 11px;border:1px solid #23455f;background:#0b263b;color:#bdd8e9;cursor:pointer}.rancher-panel button.primary{background:#087b9e}.rancher-panel button:disabled{opacity:.45;cursor:not-allowed}.rancher-error,.rancher-message{margin-top:12px;padding:9px 11px;border:1px solid #7b3140;background:#351723;color:#ff9cad}.rancher-message{border-color:#155e75;background:#082b38;color:#a5f3fc}.rancher-config{display:grid;grid-template-columns:auto 1fr auto;gap:10px;align-items:center;margin-top:14px;padding:11px;border:1px solid #16445c;background:#061620}.rancher-config span{color:#6f91a5}.rancher-config strong{color:#a5f3fc}.rancher-config b{color:#2dd4bf}.rancher-empty{display:flex;flex-direction:column;align-items:center;gap:7px;margin-top:14px;padding:34px;border:1px dashed #1b4258;color:#64748b;text-align:center}.rancher-empty svg{color:#22d3ee;width:28px}.rancher-empty strong{color:#c9e6f5}.rancher-empty code{color:#67e8f9}.rancher-clusters{display:grid;gap:9px;margin-top:12px}.rancher-clusters article{display:grid;grid-template-columns:1fr auto auto;gap:12px;align-items:center;padding:12px;border:1px solid #16445c;background:#061620}.rancher-clusters article>div:first-child{display:grid;gap:3px}.rancher-clusters span{color:#6f91a5;font-size:10px}.cluster-meta{display:grid;gap:3px;text-align:right}.cluster-meta b{color:#2dd4bf}.cluster-meta b:not([data-state=active]){color:#fbbf24}@media(max-width:1100px){.rancher-panel>header{flex-direction:column}.rancher-clusters article{grid-template-columns:1fr}}
</style>