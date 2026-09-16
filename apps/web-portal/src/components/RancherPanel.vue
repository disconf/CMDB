<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { Boxes, ExternalLink, RefreshCw, Save, Settings, ShieldCheck } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'

type Cluster = { id:string;name:string;state:string;kubernetesVersion:string;nodeCount:number;dashboardUrl:string }
type Config = { configured:boolean;url?:string;username?:string;authType?:string;insecure?:boolean;enabled?:boolean;credentialId?:string;updatedAt?:string }
const auth = useAuthStore()
const config = ref<Config>({ configured:false })
const clusters = ref<Cluster[]>([])
const loading = ref(false)
const syncing = ref(false)
const saving = ref(false)
const testing = ref(false)
const showForm = ref(false)
const error = ref('')
const message = ref('')
const form = reactive({ url:'', username:'', authType:'token', secret:'', insecure:true, enabled:true })
const headers = () => ({ Authorization:`Bearer ${auth.token}`, 'Content-Type':'application/json' })
async function request<T>(path:string,init?:RequestInit){const response=await fetch(path,{...init,headers:headers()});const body=await response.json().catch(()=>({message:'请求失败'}));if(!response.ok)throw new Error((body as {message?:string}).message||`请求失败 (${response.status})`);return body as T}
function applyConfig(data:Config){config.value=data;form.url=data.url||'';form.username=data.username||'';form.authType=data.authType||'token';form.insecure=data.insecure??true;form.enabled=data.enabled??true;form.secret='';showForm.value=!data.configured}
async function load(){loading.value=true;error.value='';try{applyConfig(await request<Config>('/api/v1/cmdb/rancher/config'));if(config.value.configured)clusters.value=await request<Cluster[]>('/api/v1/cmdb/rancher/clusters')}catch(reason){error.value=reason instanceof Error?reason.message:'Rancher 数据加载失败'}finally{loading.value=false}}
async function testConnection(){testing.value=true;error.value='';message.value='';try{await request('/api/v1/cmdb/rancher/config/test',{method:'POST',body:JSON.stringify({url:form.url,username:form.username,authType:form.authType,secret:form.secret,insecure:form.insecure})});message.value='连接测试通过，Rancher API 可访问'}catch(reason){error.value=reason instanceof Error?reason.message:'连接测试失败'}finally{testing.value=false}}
async function save(){saving.value=true;error.value='';message.value='';try{const updated=await request<Config>('/api/v1/cmdb/rancher/config',{method:'PUT',body:JSON.stringify({url:form.url,username:form.username,authType:form.authType,secret:form.secret,insecure:form.insecure,enabled:form.enabled})});applyConfig(updated);message.value='Rancher 集成配置已保存';if(updated.configured)clusters.value=await request<Cluster[]>('/api/v1/cmdb/rancher/clusters')}catch(reason){error.value=reason instanceof Error?reason.message:'保存失败'}finally{saving.value=false}}
async function sync(){syncing.value=true;error.value='';message.value='';try{const result=await request<{clusters:number;nodes:number;namespaces:number;workloads:number}>('/api/v1/cmdb/rancher/sync',{method:'POST'});message.value=`已同步集群 ${result.clusters} 个、节点 ${result.nodes} 个、命名空间 ${result.namespaces} 个、工作负载 ${result.workloads} 个`;await load()}catch(reason){error.value=reason instanceof Error?reason.message:'Rancher 同步失败'}finally{syncing.value=false}}
function open(url:string){if(url)window.open(url,'_blank','noopener,noreferrer')}
onMounted(load)
</script>

<template>
  <section class="rancher-panel">
    <header><div><span>INTEGRATION / RANCHER</span><h2>Rancher 集成与 K8s 集群入口</h2><p>连接配置集中维护，K8s 页面只保留同步和跳转操作。</p></div><div><button :disabled="loading" @click="showForm=!showForm"><Settings/>{{showForm?'收起配置':'配置连接'}}</button><button :disabled="loading" @click="load"><RefreshCw :class="{spin:loading}"/>刷新</button><button class="primary" :disabled="syncing||!config.configured" @click="sync"><Boxes/>同步集群</button></div></header>
    <div v-if="error" class="rancher-error">{{error}}</div><div v-if="message" class="rancher-message">{{message}}</div>

    <form v-if="showForm" class="rancher-form" @submit.prevent="save">
      <div class="form-heading"><ShieldCheck/><div><strong>Rancher 连接配置</strong><span>Token 或密码保存在 Vault，数据库只保存凭据引用。</span></div></div>
      <div class="form-grid">
        <label class="wide"><span>Rancher 地址 *</span><input v-model="form.url" placeholder="https://172.28.68.57:8443"></label>
        <label><span>认证方式</span><select v-model="form.authType"><option value="token">API Token</option><option value="password">用户名密码</option></select></label>
        <label><span>用户名</span><input v-model="form.username" :disabled="form.authType==='token'" placeholder="仅用户名密码模式需要"></label>
        <label class="wide"><span>{{form.authType==='token'?'API Token':'密码'}}</span><input v-model="form.secret" type="password" :placeholder="config.credentialId?'留空表示继续使用已保存凭据':'请输入凭据'"></label>
        <label class="check"><input v-model="form.insecure" type="checkbox"><span>跳过 TLS 证书校验</span></label>
        <label class="check"><input v-model="form.enabled" type="checkbox"><span>启用 Rancher 集成</span></label>
      </div>
      <div class="form-actions"><button type="button" :disabled="testing" @click="testConnection">{{testing?'测试中…':'测试连接'}}</button><button class="primary" :disabled="saving" type="submit"><Save/>{{saving?'保存中…':'保存配置'}}</button></div>
    </form>

    <div v-if="!config.configured" class="rancher-empty"><Settings/><strong>Rancher 尚未配置</strong><span>点击“配置连接”填写 Rancher 地址和凭据，测试通过后保存。</span></div>
    <template v-else>
      <div class="rancher-config"><span>连接状态</span><strong>{{config.url}}</strong><b>已配置</b><small>{{config.enabled?'已启用':'已停用'}} · {{config.updatedAt||'环境变量配置'}}</small></div>
      <div class="rancher-clusters"><article v-for="cluster in clusters" :key="cluster.id"><div><strong>{{cluster.name}}</strong><span>{{cluster.id}} · {{cluster.kubernetesVersion||'版本未知'}}</span></div><div class="cluster-meta"><b :data-state="cluster.state">{{cluster.state}}</b><span>{{cluster.nodeCount}} 节点</span></div><button @click="open(cluster.dashboardUrl)">打开 Rancher <ExternalLink/></button></article><div v-if="!clusters.length" class="rancher-empty">Rancher 未返回集群，点击“同步集群”重新读取。</div></div>
    </template>
  </section>
</template>

<style scoped>
.rancher-panel{padding:18px;border:1px solid #173b55;border-radius:12px;background:#071925}.rancher-panel>header{display:flex;justify-content:space-between;gap:16px;align-items:flex-start}.rancher-panel header span{color:#22d3ee;font:10px/1.2 monospace;letter-spacing:.15em}.rancher-panel h2{margin:5px 0}.rancher-panel p{margin:0;color:#7894a8;font-size:11px}.rancher-panel header>div:last-child{display:flex;gap:8px;flex-wrap:wrap}.rancher-panel button{display:flex;align-items:center;gap:6px;padding:8px 11px;border:1px solid #23455f;background:#0b263b;color:#bdd8e9;cursor:pointer;border-radius:6px}.rancher-panel button.primary{background:#087b9e}.rancher-panel button:disabled{opacity:.45;cursor:not-allowed}.rancher-error,.rancher-message{margin-top:12px;padding:9px 11px;border:1px solid #7b3140;background:#351723;color:#ff9cad;border-radius:6px}.rancher-message{border-color:#155e75;background:#082b38;color:#a5f3fc}.rancher-form{margin-top:14px;padding:14px;border:1px solid #246c98;background:#061620;border-radius:9px}.form-heading{display:flex;align-items:center;gap:9px;margin-bottom:12px}.form-heading svg{color:#31c9eb}.form-heading strong,.form-heading span{display:block}.form-heading span{color:#7894a8;font-size:10px;margin-top:3px}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px}.form-grid label{display:grid;gap:5px}.form-grid label.wide{grid-column:1/-1}.form-grid label.check{display:flex;align-items:center;gap:6px}.form-grid span{font-size:10px;color:#7894a8}.form-grid input,.form-grid select{height:34px;border:1px solid #28536c;background:#031525;color:#d5edf8;padding:0 8px;border-radius:6px}.form-actions{display:flex;justify-content:flex-end;gap:7px;margin-top:12px}.rancher-config{display:grid;grid-template-columns:auto 1fr auto;gap:10px;align-items:center;margin-top:14px;padding:11px;border:1px solid #16445c;background:#061620}.rancher-config span{color:#6f91a5}.rancher-config strong{color:#a5f3fc}.rancher-config b{color:#2dd4bf}.rancher-config small{grid-column:2/-1;color:#64748b;font-size:9px}.rancher-empty{display:flex;flex-direction:column;align-items:center;gap:7px;margin-top:14px;padding:34px;border:1px dashed #1b4258;color:#64748b;text-align:center}.rancher-empty svg{color:#22d3ee;width:28px}.rancher-empty strong{color:#c9e6f5}.rancher-clusters{display:grid;gap:9px;margin-top:12px}.rancher-clusters article{display:grid;grid-template-columns:1fr auto auto;gap:12px;align-items:center;padding:12px;border:1px solid #16445c;background:#061620}.rancher-clusters article>div:first-child{display:grid;gap:3px}.rancher-clusters span{color:#6f91a5;font-size:10px}.cluster-meta{display:grid;gap:3px;text-align:right}.cluster-meta b{color:#2dd4bf}.cluster-meta b:not([data-state=active]){color:#fbbf24}.spin{animation:spin 1s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}@media(max-width:900px){.rancher-panel>header{flex-direction:column}.form-grid{grid-template-columns:1fr}.form-grid label.wide{grid-column:auto}.rancher-clusters article{grid-template-columns:1fr}}
</style>
