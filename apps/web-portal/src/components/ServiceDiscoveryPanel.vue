<script setup lang="ts">
import { ref } from 'vue'
import { Activity, CheckCircle2, RefreshCw, Search } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'

type Result = { scanned:number; found:number; adopted:number; merged:number; conflicts:number; taskId?:string; services:Array<{id:string;name:string;ip:string;type:string;attributes:Array<{name:string;value:string}>}>; hosts:Array<{ip:string;hostname?:string;status:string;services:number;error?:string}> }
const auth = useAuthStore()
const cidrs = ref('')
const port = ref(22)
const credentialId = ref('')
const requireApproval = ref(false)
const loading = ref(false)
const error = ref('')
const message = ref('')
const result = ref<Result>()
async function scan(){
  const targets = cidrs.value.split(/\r?\n/).map(value => value.trim()).filter(Boolean)
  if (!targets.length) { error.value = '请输入一个或多个 IP/CIDR'; return }
  loading.value = true; error.value = ''; message.value = ''
  try {
    const response = await fetch('/api/v1/discovery/scan-services', { method:'POST', headers:{ Authorization:`Bearer ${auth.token}`, 'Content-Type':'application/json' }, body:JSON.stringify({ cidrs:targets, port:port.value, credentialId:credentialId.value, requireApproval:requireApproval.value }) })
    const body = await response.json().catch(() => ({ message:'扫描失败' }))
    if (!response.ok) throw new Error((body as {message?:string}).message || `扫描失败 (${response.status})`)
    result.value = body as Result
    message.value = `扫描 ${result.value.scanned} 台，发现 ${result.value.found} 个服务实例，新纳管 ${result.value.adopted}，合并 ${result.value.merged}`
  } catch(reason) { error.value = reason instanceof Error ? reason.message : '服务扫描失败' } finally { loading.value = false }
}
function attr(item:Result['services'][number], name:string){return item.attributes.find(value => value.name === name)?.value || '—'}
</script>

<template>
  <section class="service-discovery-panel">
    <header><div><span>SERVICE DISCOVERY</span><h3>中间件与数据库自动发现</h3><p>通过 SSH 只读识别进程、监听端口、数据目录、日志路径和常见 Exporter。</p></div><button :disabled="loading" @click="scan"><Search :class="{spin:loading}"/>开始扫描</button></header>
    <div v-if="error" class="service-error">{{error}}</div><div v-if="message" class="service-message">{{message}}</div>
    <div class="service-form"><label class="wide">IP / CIDR（每行一个）<textarea v-model="cidrs" rows="3" placeholder="172.28.69.0/24&#10;172.28.68.0/24"></textarea></label><label>SSH 端口<input v-model.number="port" type="number" min="1" max="65535"></label><label>SSH 凭据<input v-model="credentialId" placeholder="留空使用服务端默认"></label><label class="approval"><input v-model="requireApproval" type="checkbox">发现结果进入审批队列</label></div>
    <div v-if="result" class="service-summary"><article><Activity/><strong>{{result.scanned}}</strong><span>扫描主机</span></article><article><CheckCircle2/><strong>{{result.found}}</strong><span>发现服务</span></article><article><strong>{{result.adopted}}</strong><span>新纳管</span></article><article><strong>{{result.merged}}</strong><span>合并刷新</span></article><article><strong>{{result.conflicts}}</strong><span>冲突</span></article></div>
    <div v-if="result?.hosts?.length" class="host-results"><article v-for="host in result.hosts" :key="host.ip" :data-status="host.status"><span><strong>{{host.hostname||host.ip}}</strong><small>{{host.ip}}</small></span><b>{{host.status==='failed'?'SSH失败':host.status==='discovered'?`发现 ${host.services} 个服务`:'未发现服务'}}</b><em v-if="host.error">{{host.error}}</em></article></div>
    <div v-if="result?.services?.length" class="service-results"><article v-for="item in result.services" :key="item.id"><header><div><strong>{{item.name}}</strong><span>{{item.ip}} · {{item.type}}</span></div><b>{{attr(item,'listen_port')}}</b></header><dl><div><dt>进程</dt><dd>{{attr(item,'process_name')}}</dd></div><div><dt>启动用户</dt><dd>{{attr(item,'run_user')}}</dd></div><div><dt>数据目录</dt><dd>{{attr(item,'data_dir')}}</dd></div><div><dt>日志路径</dt><dd>{{attr(item,'log_path')}}</dd></div><div><dt>Exporter</dt><dd>{{attr(item,'exporter_port')}}</dd></div></dl></article></div>
  </section>
</template>

<style scoped>
.service-discovery-panel{padding:16px;border:1px solid #173b55;border-radius:12px;background:#071925}.service-discovery-panel>header{display:flex;justify-content:space-between;gap:14px}.service-discovery-panel header span{color:#22d3ee;font:10px/1.2 monospace;letter-spacing:.15em}.service-discovery-panel h3{margin:5px 0}.service-discovery-panel p{margin:0;color:#7894a8;font-size:10px}.service-discovery-panel button{display:flex;align-items:center;gap:6px;padding:9px 12px;border:1px solid #23455f;background:#087b9e;color:white;cursor:pointer}.service-discovery-panel button:disabled{opacity:.5}.service-form{display:grid;grid-template-columns:1fr 150px 220px auto;gap:10px;margin-top:14px}.service-form label{display:grid;gap:5px;color:#8fb2c4;font-size:10px}.service-form label.wide{grid-column:1/2}.service-form input,.service-form textarea{padding:8px;border:1px solid #22465f;background:#061725;color:#dcecff;font:inherit}.service-form .approval{display:flex;align-items:center;gap:6px;padding-top:18px}.service-error,.service-message{margin-top:11px;padding:9px 11px;border:1px solid #7b3140;background:#351723;color:#ff9cad}.service-message{border-color:#155e75;background:#082b38;color:#a5f3fc}.service-summary{display:grid;grid-template-columns:repeat(5,1fr);gap:8px;margin-top:12px}.service-summary article{display:grid;grid-template-columns:auto 1fr;gap:2px 7px;padding:10px;border:1px solid #173b55;background:#061620}.service-summary svg{grid-row:1/3;color:#22d3ee}.service-summary strong{font-size:19px}.service-summary span{color:#7894a8;font-size:9px}.service-results{display:grid;grid-template-columns:repeat(auto-fit,minmax(360px,1fr));gap:9px;margin-top:12px}.service-results article{padding:11px;border:1px solid #16445c;background:#061620}.service-results article>header{display:flex;justify-content:space-between}.service-results header span{display:block;color:#6f91a5;font-size:9px}.service-results header b{color:#67e8f9}.service-results dl{display:grid;grid-template-columns:1fr 1fr;gap:6px;margin:9px 0 0}.service-results dl div{min-width:0}.service-results dt{color:#64748b;font-size:9px}.service-results dd{margin:2px 0;overflow:hidden;color:#bdd8e9;font-size:10px;text-overflow:ellipsis;white-space:nowrap}@media(max-width:1200px){.service-form{grid-template-columns:1fr 1fr}.service-form label.wide{grid-column:1/3}.service-summary{grid-template-columns:repeat(2,1fr)}}
.host-results{display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));gap:8px;margin-top:12px}.host-results article{display:grid;grid-template-columns:1fr auto;gap:6px;padding:9px 11px;border:1px solid #16445c;background:#061620}.host-results span{display:grid;gap:2px}.host-results small{color:#64748b}.host-results b{color:#2dd4bf;font-size:10px}.host-results article[data-status=failed]{border-color:#7b3140}.host-results article[data-status=failed] b{color:#fb7185}.host-results em{grid-column:1/-1;color:#fda4af;font-size:9px;font-style:normal}</style>