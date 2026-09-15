<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Activity, ClipboardCheck, Clock3, Play, Plus, Power, Settings2, ShieldCheck, Trash2, X } from 'lucide-vue-next'
import { createNetworkAutomationPolicy, deleteNetworkAutomationPolicy, evaluateNetworkAutomation, fetchNetworkAutomationLifecycle, fetchNetworkAutomationPolicies, updateNetworkAutomationPolicy } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'
import type { NetworkAutomationLifecycle, NetworkAutomationPolicy, NetworkAutomationReport } from '@/features/cmdb/types'

const auth = useAuthStore()
const policies = ref<NetworkAutomationPolicy[]>([])
const lifecycle = ref<NetworkAutomationLifecycle[]>([])
const report = ref<NetworkAutomationReport>()
const busy = ref(false)
const message = ref('')
const error = ref('')
const showForm = ref(false)
const form = reactive({ name:'严重网络异常自动闭环', severity:'critical', anomalyType:'*', environment:'', projectGroup:'', location:'', autoTicket:true, assignee:'网络运维组', priority:10, escalationMinutes:30, escalateTo:'网络负责人', verificationTemplate:'tpl-health' })
const activeCount = computed(() => policies.value.filter(item => item.enabled).length)
const openCount = computed(() => lifecycle.value.filter(item => item.status !== 'resolved').length)

async function load() {
  busy.value = true
  error.value = ''
  try {
    const [items, states] = await Promise.all([fetchNetworkAutomationPolicies(auth.token), fetchNetworkAutomationLifecycle(auth.token)])
    policies.value = items
    lifecycle.value = states
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '自动化策略加载失败'
  } finally { busy.value = false }
}
async function run() {
  busy.value = true
  message.value = ''
  error.value = ''
  try {
    report.value = await evaluateNetworkAutomation(auth.token)
    message.value = `自动化工单 ${report.value.autoTickets}，升级 ${report.value.escalated}，恢复验证 ${report.value.verificationJobs}`
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '自动化执行失败'
  } finally { busy.value = false }
}
async function create() {
  busy.value = true
  error.value = ''
  try {
    await createNetworkAutomationPolicy(auth.token, { ...form })
    showForm.value = false
    message.value = '自动化策略已创建'
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '策略创建失败'
  } finally { busy.value = false }
}
async function toggle(item:NetworkAutomationPolicy) {
  busy.value = true
  error.value = ''
  try {
    await updateNetworkAutomationPolicy(auth.token, item.id, { ...item, enabled: !item.enabled })
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '策略状态更新失败'
  } finally { busy.value = false }
}
async function remove(item:NetworkAutomationPolicy) {
  if (item.id === 'default-critical-network') return
  busy.value = true
  error.value = ''
  try {
    await deleteNetworkAutomationPolicy(auth.token, item.id)
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '策略删除失败'
  } finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <section class="automation-panel">
    <header><div><Settings2/><span><strong>异常自动化策略</strong><small>自动派单、持续升级和恢复验证</small></span></div><div><button :disabled="busy" @click="showForm=true"><Plus/>新增策略</button><button class="primary" :disabled="busy" @click="run"><Play/>立即执行</button></div></header>
    <div class="automation-stats"><article><ShieldCheck/><span>启用策略<strong>{{activeCount}}</strong></span></article><article><Activity/><span>处理中异常<strong>{{openCount}}</strong></span></article><article><ClipboardCheck/><span>自动工单<strong>{{report?.autoTickets||0}}</strong></span></article><article><Clock3/><span>升级次数<strong>{{report?.escalated||0}}</strong></span></article><article><Play/><span>验证任务<strong>{{report?.verificationJobs||0}}</strong></span></article></div>
    <div v-if="message" class="automation-message">{{message}}</div><div v-if="error" class="automation-error">{{error}}</div>
    <div class="policy-grid"><article v-for="item in policies" :key="item.id"><header><div><strong>{{item.name}}</strong><span>优先级 {{item.priority}} · {{item.severity}} · {{item.anomalyType}}</span></div><button :disabled="busy" @click="toggle(item)"><Power/>{{item.enabled?'停用':'启用'}}</button></header><p>环境 {{item.environment||'全部'}} · 项目组 {{item.projectGroup||'全部'}} · 位置 {{item.location||'全部'}}</p><dl><div><dt>自动派单</dt><dd>{{item.autoTicket?'是':'否'}}</dd></div><div><dt>处理人</dt><dd>{{item.assignee||'未设置'}}</dd></div><div><dt>升级阈值</dt><dd>{{item.escalationMinutes?`${item.escalationMinutes} 分钟`:'不升级'}}</dd></div><div><dt>升级对象</dt><dd>{{item.escalateTo||'未设置'}}</dd></div><div><dt>恢复验证</dt><dd>{{item.verificationTemplate||'不验证'}}</dd></div></dl><footer><span :data-status="item.enabled?'enabled':'disabled'">{{item.enabled?'已启用':'已停用'}}</span><button v-if="item.id!=='default-critical-network'" :disabled="busy" @click="remove(item)"><Trash2/>删除</button></footer></article></div>
    <div class="lifecycle-list"><h4>异常生命周期</h4><div class="lifecycle-head"><span>异常</span><span>首次发现</span><span>工单</span><span>升级</span><span>验证任务</span><span>状态</span></div><div v-for="item in lifecycle.slice(0,20)" :key="item.anomalyId" class="lifecycle-row"><span><strong>{{item.title||item.anomalyId}}</strong><small>{{item.target||'-'}}</small></span><span>{{item.firstSeenAt}}</span><span>{{item.ticketId||'-'}}</span><span>{{item.escalatedAt||'-'}}</span><span>{{item.verificationJobId||'-'}}</span><b :data-status="item.status">{{item.status==='resolved'?'已恢复':'处理中'}}</b></div><div v-if="!lifecycle.length" class="automation-empty">暂无异常生命周期记录</div></div>
    <div v-if="showForm" class="automation-modal" @click.self="showForm=false"><form @submit.prevent="create"><header><div><span>AUTOMATION POLICY</span><h3>新增自动化策略</h3></div><button type="button" @click="showForm=false"><X/></button></header><div class="form-grid"><label><span>策略名称</span><input v-model="form.name" required></label><label><span>优先级</span><input v-model.number="form.priority" type="number" min="1"></label><label><span>严重级别</span><select v-model="form.severity"><option value="critical">严重</option><option value="warning">关注</option><option value="*">全部</option></select></label><label><span>异常类型</span><input v-model="form.anomalyType" placeholder="* 或 mac-drift"></label><label><span>环境</span><input v-model="form.environment" placeholder="全部"></label><label><span>项目组</span><input v-model="form.projectGroup" placeholder="全部"></label><label><span>位置/机房</span><input v-model="form.location" placeholder="全部"></label><label><span>处理人</span><input v-model="form.assignee"></label><label><span>升级分钟</span><input v-model.number="form.escalationMinutes" type="number" min="0"></label><label><span>升级对象</span><input v-model="form.escalateTo"></label><label><span>验证模板</span><input v-model="form.verificationTemplate" placeholder="tpl-health"></label><label class="check"><input v-model="form.autoTicket" type="checkbox"><span>自动创建工单</span></label></div><footer><button type="button" @click="showForm=false">取消</button><button class="primary" :disabled="busy" type="submit">创建策略</button></footer></form></div>
  </section>
</template>

<style scoped>
.automation-panel{margin-top:14px;border:1px solid #174d78;background:rgba(4,24,43,.72)}.automation-panel>header{display:flex;justify-content:space-between;align-items:center;padding:14px 18px;border-bottom:1px solid #173d58}.automation-panel>header>div:first-child{display:flex;gap:8px;align-items:center}.automation-panel>header svg{width:18px;color:#31cbea}.automation-panel>header span{display:grid;gap:3px}.automation-panel>header strong{font-size:12px;font-weight:500}.automation-panel>header small{font-size:9px;color:#638397}.automation-panel>header>div:last-child{display:flex;gap:7px}.automation-panel button{display:flex;align-items:center;gap:5px;border:1px solid #28536c;background:#0b263b;color:#a9c6d9;padding:6px 9px;font-size:9px;cursor:pointer}.automation-panel button.primary{background:#0b6da5;border-color:#1a93c8;color:#fff}.automation-panel button:disabled{opacity:.4}.automation-panel button svg{width:12px}.automation-stats{display:grid;grid-template-columns:repeat(5,1fr);gap:7px;padding:10px 18px}.automation-stats article{display:flex;gap:8px;align-items:center;padding:9px;background:#061a2c;border:1px solid #153d57}.automation-stats svg{width:18px;color:#32c5e8}.automation-stats span{display:grid;gap:3px;color:#66879b;font-size:9px}.automation-stats strong{font-size:17px;color:#d5edf8;font-weight:500}.policy-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(270px,1fr));gap:8px;padding:0 18px 12px}.policy-grid article{padding:11px;border:1px solid #17415e;background:#061a2c}.policy-grid article>header{display:flex;justify-content:space-between}.policy-grid article>header span,.policy-grid p{font-size:9px;color:#66879b}.policy-grid article>header strong{display:block;font-weight:500;color:#d4eaf6}.policy-grid dl{display:grid;grid-template-columns:1fr 1fr;gap:6px;margin:9px 0}.policy-grid dt{font-size:8px;color:#5e7e93}.policy-grid dd{margin:3px 0 0;font-size:9px;color:#b9d6e5}.policy-grid footer{display:flex;justify-content:space-between;align-items:center}.policy-grid footer span{padding:3px 6px;background:rgba(72,216,154,.1);color:#5bdfa1;font-size:8px}.policy-grid footer span[data-status=disabled]{background:rgba(120,139,153,.12);color:#91a2ad}.lifecycle-list{padding:12px 18px 16px;border-top:1px solid #173d58}.lifecycle-list h4{font-size:10px;margin:0 0 8px}.lifecycle-head,.lifecycle-row{display:grid;grid-template-columns:1.5fr 1fr 1fr 1fr 1fr .6fr;gap:6px;align-items:center;padding:7px 8px;font-size:8px}.lifecycle-head{background:#08243a;color:#608197}.lifecycle-row{background:#061a2c;border-bottom:1px solid #15374e;color:#9eb9c9}.lifecycle-row strong,.lifecycle-row small{display:block}.lifecycle-row small{color:#5f7f94}.lifecycle-row b{font-weight:500;color:#ffc167}.lifecycle-row b[data-status=resolved]{color:#5bdfa1}.automation-message,.automation-error{margin:8px 18px;padding:7px;font-size:9px}.automation-message{background:rgba(68,214,145,.1);color:#61dfa2}.automation-error{background:rgba(255,82,104,.1);color:#ff8998}.automation-empty{padding:18px;text-align:center;color:#638397;font-size:9px}.automation-modal{position:fixed;inset:0;z-index:70;background:rgba(0,8,16,.78);display:grid;place-items:center}.automation-modal form{width:min(680px,92vw);max-height:88vh;overflow:auto;background:#041a2e;border:1px solid #246c98}.automation-modal form>header{display:flex;justify-content:space-between;padding:14px 17px;border-bottom:1px solid #174d78}.automation-modal h3{margin:4px 0}.automation-modal header span{font-size:8px;color:#31c6e7;letter-spacing:2px}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:9px;padding:14px 17px}.form-grid label{display:grid;gap:4px}.form-grid label span{font-size:9px;color:#6b8da2}.form-grid input,.form-grid select{height:31px;border:1px solid #24536f;background:#031525;color:#c4dfed;padding:0 7px}.form-grid label.check{display:flex;align-items:center;gap:7px}.form-grid label.check input{width:16px;height:16px}.automation-modal footer{display:flex;justify-content:flex-end;gap:7px;padding:12px 17px;border-top:1px solid #173d58}@media(max-width:1100px){.automation-stats{grid-template-columns:repeat(2,1fr)}}
</style>
