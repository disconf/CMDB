<script setup lang="ts">
import {computed,onMounted,ref} from 'vue'
import {Clock3,Pause,Pencil,Play,Plus,RadioTower,RefreshCw,Server,Trash2,Wifi,X} from 'lucide-vue-next'
import {useAuthStore} from '@/stores/useAuthStore'

type Schedule={id:string;name:string;kind:'node-exporter'|'ssh'|'snmp';cidrs:string[];port?:number;ports?:number[];defaultType?:string;credentialId?:string;requireApproval:boolean;intervalMinutes:number;enabled:boolean;running:boolean;nextRunAt?:string;lastRunAt?:string;lastTaskId?:string;lastStatus?:string;lastError?:string;createdBy?:string;createdAt?:string}
type Credential={id:string;name:string;kind:string;username?:string}
type ScheduleForm={id:string;name:string;kind:'node-exporter'|'ssh'|'snmp';cidrs:string;ports:string;defaultType:string;credentialId:string;requireApproval:boolean;intervalMinutes:number;enabled:boolean}
const auth=useAuthStore(),schedules=ref<Schedule[]>([]),credentials=ref<Credential[]>([]),loading=ref(false),busy=ref(false),error=ref(''),notice=ref(''),showForm=ref(false)
const emptyForm=():ScheduleForm=>({id:'',name:'',kind:'node-exporter',cidrs:'172.28.68.0/24\n172.28.69.0/24',ports:'9100,19100',defaultType:'',credentialId:'',requireApproval:false,intervalMinutes:60,enabled:true})
const form=ref<ScheduleForm>(emptyForm())
const emit=defineEmits<{openTask:[id:string]}>()
const headers=computed(()=>({Authorization:`Bearer ${auth.token}`,'Content-Type':'application/json'}))
async function request<T>(url:string,options?:RequestInit){const response=await fetch(url,{...options,headers:headers.value});if(!response.ok){let message=`请求失败 (${response.status})`;try{const body=await response.json() as {message?:string;code?:string};message=body.message||body.code||message}catch{}throw new Error(message)}if(response.status===204)return undefined as T;return response.json() as Promise<T>}
async function load(){loading.value=true;error.value='';try{[schedules.value,credentials.value]=await Promise.all([request<Schedule[]>('/api/v1/discovery/schedules'),request<Credential[]>('/api/v1/credentials')])}catch(e){error.value=e instanceof Error?e.message:'采集计划加载失败'}finally{loading.value=false}}
function create(){form.value=emptyForm();showForm.value=true;notice.value=''}
function edit(item:Schedule){form.value={id:item.id,name:item.name,kind:item.kind,cidrs:(item.cidrs||[]).join('\n'),ports:(item.ports?.length?item.ports:(item.port?[item.port]:[])).join(','),defaultType:item.defaultType||'',credentialId:item.credentialId||'',requireApproval:item.requireApproval,intervalMinutes:item.intervalMinutes,enabled:item.enabled};showForm.value=true;notice.value=''}
function close(){showForm.value=false}
async function save(){busy.value=true;error.value='';notice.value='';try{const ports=Array.from(new Set(form.value.ports.split(/[^0-9]+/).map(Number).filter(v=>v>=1&&v<=65535)));const payload={name:form.value.name,kind:form.value.kind,cidrs:form.value.cidrs.split(/\r?\n/).map(v=>v.trim()).filter(Boolean),ports:form.value.kind==='node-exporter'?ports:undefined,port:form.value.kind==='node-exporter'?(ports[0]||0):(form.value.kind==='ssh'?(ports[0]||22):(ports[0]||161)),defaultType:form.value.defaultType,credentialId:form.value.credentialId,requireApproval:form.value.requireApproval,intervalMinutes:form.value.intervalMinutes,enabled:form.value.enabled};if(form.value.id){await request(`/api/v1/discovery/schedules/${encodeURIComponent(form.value.id)}`,{method:'PUT',body:JSON.stringify(payload)});notice.value='采集计划已更新'}else{await request('/api/v1/discovery/schedules',{method:'POST',body:JSON.stringify(payload)});notice.value='采集计划已创建'}close();await load()}catch(e){error.value=e instanceof Error?e.message:'采集计划保存失败'}finally{busy.value=false}}
async function toggle(item:Schedule){busy.value=true;error.value='';try{await request(`/api/v1/discovery/schedules/${encodeURIComponent(item.id)}/toggle`,{method:'POST'});await load()}catch(e){error.value=e instanceof Error?e.message:'状态更新失败'}finally{busy.value=false}}
async function run(item:Schedule){busy.value=true;error.value='';notice.value='';try{const result=await request<{taskId:string;status:string;scanned:number;found:number;adopted:number;merged:number;conflicts:number}>(`/api/v1/discovery/schedules/${encodeURIComponent(item.id)}/run`,{method:'POST'});notice.value=`执行完成：扫描 ${result.scanned}，发现 ${result.found}，入库 ${result.adopted}，合并 ${result.merged}`;await load();if(result.taskId)emit('openTask',result.taskId)}catch(e){error.value=e instanceof Error?e.message:'立即执行失败'}finally{busy.value=false}}
async function remove(item:Schedule){if(!window.confirm(`确认删除采集计划“${item.name}”？`))return;busy.value=true;error.value='';try{await request(`/api/v1/discovery/schedules/${encodeURIComponent(item.id)}`,{method:'DELETE'});await load()}catch(e){error.value=e instanceof Error?e.message:'删除失败'}finally{busy.value=false}}
function kindLabel(kind:string){return kind==='node-exporter'?'node_exporter':kind==='ssh'?'SSH 采集':'SNMP 采集'}
function intervalLabel(minutes:number){if(minutes%1440===0)return `${minutes/1440} 天`;if(minutes%60===0)return `${minutes/60} 小时`;return `${minutes} 分钟`}
function formatTime(value?:string){if(!value)return '-';const date=new Date(value);return Number.isNaN(date.getTime())?value:date.toLocaleString('zh-CN',{hour12:false})}
const credentialOptions=computed(()=>credentials.value.filter(item=>item.kind===(form.value.kind==='ssh'?'ssh':'snmp')))
onMounted(load)
</script>

<template>
<section class="collection-schedules">
  <header class="schedule-heading">
    <div><span>COLLECTION PLANS</span><h2>采集计划与定时调度</h2><p>按固定周期自动执行 node_exporter、SSH 或 SNMP 采集，并将结果写入 CMDB 与任务记录。</p></div>
    <div class="heading-actions"><button @click="load" :disabled="loading"><RefreshCw :class="{spin:loading}"/>刷新</button><button class="primary" @click="create"><Plus/>新建计划</button></div>
  </header>
  <div v-if="error" class="schedule-message error">{{error}}</div>
  <div v-if="notice" class="schedule-message success">{{notice}}</div>
  <div v-if="showForm" class="schedule-form">
    <header><div><span>{{form.id?'EDIT PLAN':'NEW PLAN'}}</span><h3>{{form.id?'编辑采集计划':'新建采集计划'}}</h3></div><button @click="close"><X/></button></header>
    <div class="form-grid">
      <label><span>计划名称</span><input v-model="form.name" maxlength="80" placeholder="例如：核心网段 node_exporter 扫描"></label>
      <label><span>采集类型</span><select v-model="form.kind"><option value="node-exporter">node_exporter 网段扫描</option><option value="ssh">SSH 主机采集</option><option value="snmp">SNMP 网络设备采集</option></select></label>
      <label class="wide"><span>目标网段（每行一个 CIDR）</span><textarea v-model="form.cidrs" rows="3" placeholder="172.28.68.0/24"></textarea></label>
      <label><span>{{form.kind==='node-exporter'?'Exporter 候选端口':'服务端口'}}</span><input v-model="form.ports" :placeholder="form.kind==='node-exporter'?'9100,19100':(form.kind==='ssh'?'22':'161')"></label>
      <label><span>执行周期</span><select v-model.number="form.intervalMinutes"><option :value="5">每 5 分钟</option><option :value="15">每 15 分钟</option><option :value="30">每 30 分钟</option><option :value="60">每小时</option><option :value="360">每 6 小时</option><option :value="720">每 12 小时</option><option :value="1440">每天</option></select></label>
      <label><span>资产类型</span><select v-model="form.defaultType"><option value="">自动识别</option><option value="physical-server">物理服务器</option><option value="virtual-machine">虚拟机</option><option value="network-device">网络设备</option></select></label>
      <label><span>{{form.kind==='ssh'?'SSH 凭据':'SNMP 凭据'}}</span><select v-model="form.credentialId"><option value="">使用服务端默认</option><option v-for="item in credentialOptions" :key="item.id" :value="item.id">{{item.name}} ({{item.username||'-'}})</option></select></label>
      <label class="check"><input v-model="form.enabled" type="checkbox"><span>创建后立即启用</span></label>
      <label class="check"><input v-model="form.requireApproval" type="checkbox"><span>发现结果需要人工审批后入账</span></label>
    </div>
    <footer><button @click="close">取消</button><button class="primary" :disabled="busy" @click="save">{{busy?'保存中...':(form.id?'保存修改':'创建计划')}}</button></footer>
  </div>
  <div v-if="schedules.length" class="schedule-list">
    <article v-for="item in schedules" :key="item.id" class="schedule-card" :data-enabled="item.enabled">
      <div class="schedule-icon"><RadioTower v-if="item.kind==='node-exporter'"/><Server v-else-if="item.kind==='ssh'"/><Wifi v-else/></div>
      <div class="schedule-main">
        <header><div><strong>{{item.name}}</strong><span>{{kindLabel(item.kind)}} · 每 {{intervalLabel(item.intervalMinutes)}}</span></div><b :class="{running:item.running}">{{item.running?'执行中':(item.enabled?'已启用':'已暂停')}}</b></header>
        <p>{{(item.cidrs||[]).join(' , ')}} · {{item.kind==='node-exporter'?`端口 ${(item.ports||[item.port]).join(',')}`:`端口 ${item.port}`}} {{item.requireApproval?'· 需审批':''}}</p>
        <div class="schedule-meta"><span><Clock3/>下次 {{formatTime(item.nextRunAt)}}</span><span>上次 {{formatTime(item.lastRunAt)}}</span><span>结果 <b :data-status="item.lastStatus">{{item.lastStatus||'尚未执行'}}</b></span></div>
        <div v-if="item.lastError" class="schedule-error">{{item.lastError}}</div>
        <footer><button :disabled="busy||item.running" @click="run(item)"><Play/>立即执行</button><button v-if="item.lastTaskId" @click="emit('openTask',item.lastTaskId!)">查看任务</button><button :disabled="busy" @click="toggle(item)"><Pause/>{{item.enabled?'暂停':'启用'}}</button><button :disabled="busy" @click="edit(item)"><Pencil/>编辑</button><button class="danger" :disabled="busy" @click="remove(item)"><Trash2/>删除</button></footer>
      </div>
    </article>
  </div>
  <div v-else-if="!loading" class="schedule-empty"><Clock3/><strong>还没有采集计划</strong><span>创建计划后，后台会按周期自动采集并纳管资产。</span><button class="primary" @click="create"><Plus/>创建第一个计划</button></div>
</section>
</template>

<style scoped>
.collection-schedules{display:flex;flex-direction:column;gap:16px}.schedule-heading{display:flex;justify-content:space-between;gap:20px;align-items:flex-start}.schedule-heading>div:first-child>span{font-size:10px;letter-spacing:.16em;color:#4f7cff;font-weight:800}.schedule-heading h2{margin:5px 0;font-size:22px}.schedule-heading p{margin:0;color:var(--text-muted,#718096);font-size:13px}.heading-actions,.schedule-card footer,.schedule-form footer{display:flex;gap:8px;flex-wrap:wrap}.collection-schedules button{display:inline-flex;align-items:center;gap:6px;border:1px solid var(--border,#dfe5ef);background:#fff;color:#25324a;padding:8px 12px;border-radius:8px;cursor:pointer;font-size:12px}.collection-schedules button.primary{background:#315efb;border-color:#315efb;color:#fff}.collection-schedules button.danger{color:#c62f3d;border-color:#f0c9ce}.collection-schedules button:disabled{opacity:.55;cursor:not-allowed}.schedule-message{padding:10px 13px;border-radius:8px;font-size:13px}.schedule-message.error{background:#fff1f2;color:#b42332;border:1px solid #fecdd3}.schedule-message.success{background:#effdf5;color:#147a4b;border:1px solid #bbf7d0}.schedule-form,.schedule-card{border:1px solid var(--border,#e3e8f1);background:#fff;border-radius:12px;box-shadow:0 8px 24px rgba(31,45,78,.05)}.schedule-form{padding:18px}.schedule-form>header,.schedule-main>header{display:flex;justify-content:space-between;align-items:flex-start}.schedule-form>header span{font-size:10px;color:#4f7cff;font-weight:800;letter-spacing:.14em}.schedule-form h3{margin:3px 0 16px}.schedule-form header button{border:0;background:transparent!important;padding:4px!important}.form-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.form-grid label{display:flex;flex-direction:column;gap:6px;font-size:12px;color:#526079}.form-grid label.wide{grid-column:1/-1}.form-grid label.check{flex-direction:row;align-items:center;padding-top:24px}.form-grid input,.form-grid select,.form-grid textarea{border:1px solid #d8e0ec;border-radius:8px;padding:9px 10px;font:inherit;color:#1f2b42;background:#fff}.form-grid textarea{resize:vertical}.schedule-form footer{justify-content:flex-end;margin-top:16px}.schedule-list{display:flex;flex-direction:column;gap:12px}.schedule-card{display:flex;gap:14px;padding:16px}.schedule-card[data-enabled=false]{background:#fafbfc}.schedule-icon{width:40px;height:40px;border-radius:10px;background:#eef3ff;color:#315efb;display:grid;place-items:center;flex:0 0 auto}.schedule-main{min-width:0;flex:1}.schedule-main>header strong{display:block;font-size:15px;color:#1c2942}.schedule-main>header span{display:block;color:#77849a;font-size:12px;margin-top:3px}.schedule-main>header b{font-size:11px;padding:4px 8px;border-radius:20px;background:#edf8f1;color:#1d7a4a}.schedule-main>header b.running{background:#fff5df;color:#a46700}.schedule-main>p{color:#53627a;font-size:13px;margin:10px 0;overflow-wrap:anywhere}.schedule-meta{display:flex;gap:16px;flex-wrap:wrap;color:#7b879a;font-size:12px}.schedule-meta span{display:inline-flex;gap:5px;align-items:center}.schedule-meta svg{width:13px}.schedule-meta b[data-status=failed]{color:#c62f3d}.schedule-meta b[data-status=pending-approval]{color:#a46700}.schedule-error{margin-top:9px;padding:8px 10px;background:#fff4f4;color:#b42332;border-radius:7px;font-size:12px}.schedule-card footer{margin-top:13px}.schedule-empty{min-height:260px;border:1px dashed #cbd5e5;border-radius:12px;display:flex;flex-direction:column;justify-content:center;align-items:center;gap:9px;color:#748198;background:#fbfcfe}.schedule-empty strong{color:#26344d}.schedule-empty svg{width:32px;height:32px;color:#95a3b8}.spin{animation:spin 1s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}@media(max-width:760px){.schedule-heading{flex-direction:column}.form-grid{grid-template-columns:1fr}.form-grid label.wide{grid-column:auto}.schedule-card{padding:13px}.schedule-card footer{display:grid;grid-template-columns:repeat(2,1fr)}}
</style>
