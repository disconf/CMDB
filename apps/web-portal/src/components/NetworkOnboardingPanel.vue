<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { CheckCircle2, FilePlus2, Server, X, XCircle } from 'lucide-vue-next'
import { approveOnboardingRequest, createOnboardingRequest, fetchComplianceDecisions, fetchOnboardingRequests, rejectOnboardingRequest } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'
import type { ComplianceDecision, OnboardingRequest } from '@/features/cmdb/types'

const auth = useAuthStore()
const decisions = ref<ComplianceDecision[]>([])
const requests = ref<OnboardingRequest[]>([])
const selected = ref<ComplianceDecision>()
const busy = ref(false)
const message = ref('')
const error = ref('')
const showForm = ref(false)
const form = reactive({ name:'', assetType:'physical-server', environment:'', projectGroup:'', owner:'', location:'', note:'' })
const candidates = computed(() => decisions.value.filter(item => item.status === 'unmatched' || item.status === 'denied'))

function open(decision:ComplianceDecision) {
  selected.value = decision
  form.name = decision.macAddress || decision.ip || '新设备'
  form.environment = decision.environment || ''
  form.projectGroup = decision.projectGroup || ''
  form.location = decision.location || ''
  showForm.value = true
}
async function load() {
  busy.value = true; error.value = ''
  try { [decisions.value, requests.value] = await Promise.all([fetchComplianceDecisions(auth.token), fetchOnboardingRequests(auth.token)]) } catch (reason) { error.value = reason instanceof Error ? reason.message : '入网流程加载失败' } finally { busy.value = false }
}
async function create() {
  if (!selected.value) return
  busy.value = true; error.value = ''
  try { await createOnboardingRequest(auth.token, { anomalyId:selected.value.anomalyId, ...form }); message.value = '入网申请已创建，等待审批'; showForm.value = false; await load() } catch (reason) { error.value = reason instanceof Error ? reason.message : '入网申请创建失败' } finally { busy.value = false }
}
async function approve(item:OnboardingRequest) { busy.value = true; try { await approveOnboardingRequest(auth.token, item.id); message.value = '设备已纳管并自动写入白名单'; await load() } catch (reason) { error.value = reason instanceof Error ? reason.message : '入网审批失败' } finally { busy.value = false } }
async function reject(item:OnboardingRequest) { busy.value = true; try { await rejectOnboardingRequest(auth.token, item.id, '审批拒绝'); await load() } catch (reason) { error.value = reason instanceof Error ? reason.message : '入网申请驳回失败' } finally { busy.value = false } }
onMounted(load)
</script>

<template>
  <section class="onboarding-panel">
    <header><div><FilePlus2/><span><strong>新设备入网流程</strong><small>未授权设备审批、自动纳管和写入白名单</small></span></div><button :disabled="busy" @click="load">刷新</button></header>
    <div v-if="message" class="onboarding-message">{{message}}</div><div v-if="error" class="onboarding-error">{{error}}</div>
    <div class="candidate-list"><h4>待处理未授权接入</h4><article v-for="item in candidates" :key="item.id"><Server/><div><strong>{{item.macAddress||item.ip}}</strong><span>{{item.ip||'-'}} · VLAN {{item.vlan||'-'}} · {{item.reason}}</span></div><button :disabled="busy" @click="open(item)"><FilePlus2/>发起入网</button></article><div v-if="!candidates.length" class="onboarding-empty">当前没有需要入网审批的设备</div></div>
    <div class="request-list"><h4>入网申请记录</h4><div class="request-head"><span>设备</span><span>IP / MAC</span><span>归属</span><span>状态</span><span>资产/规则</span><span>操作</span></div><div v-for="item in requests" :key="item.id" class="request-row"><span><strong>{{item.name}}</strong><small>{{item.createdBy}} · {{item.createdAt}}</small></span><span>{{item.ip||'-'}} / {{item.macAddress||'-'}}</span><span>{{item.projectGroup||'-'}} · {{item.owner||'-'}}</span><b :data-status="item.status">{{item.status}}</b><span>{{item.assetId||'-'}}<small>{{item.ruleId||''}}</small></span><span class="request-actions"><button v-if="item.status==='pending'" :disabled="busy" @click="approve(item)"><CheckCircle2/>批准并纳管</button><button v-if="item.status==='pending'" :disabled="busy" @click="reject(item)"><XCircle/>驳回</button></span></div><div v-if="!requests.length" class="onboarding-empty">暂无入网申请</div></div>
    <div v-if="showForm" class="onboarding-modal" @click.self="showForm=false"><form @submit.prevent="create"><header><div><span>DEVICE ONBOARDING</span><h3>新设备入网申请</h3></div><button type="button" @click="showForm=false"><X/></button></header><div class="form-grid"><label><span>设备名称</span><input v-model="form.name" required></label><label><span>资产类型</span><select v-model="form.assetType"><option value="physical-server">物理服务器</option><option value="virtual-machine">虚拟机</option><option value="network-device">网络设备</option></select></label><label><span>环境</span><input v-model="form.environment"></label><label><span>项目组</span><input v-model="form.projectGroup"></label><label><span>负责人</span><input v-model="form.owner" required></label><label><span>位置/机房</span><input v-model="form.location"></label><label class="wide"><span>入网说明</span><input v-model="form.note"></label></div><footer><button type="button" @click="showForm=false">取消</button><button class="primary" type="submit" :disabled="busy">提交入网申请</button></footer></form></div>
  </section>
</template>

<style scoped>
.onboarding-panel{margin-top:14px;border:1px solid #174d78;background:rgba(4,24,43,.72)}.onboarding-panel>header{display:flex;justify-content:space-between;align-items:center;padding:14px 18px;border-bottom:1px solid #173d58}.onboarding-panel>header>div{display:flex;align-items:center;gap:8px}.onboarding-panel>header svg{width:18px;color:#31cbea}.onboarding-panel>header span{display:grid;gap:3px}.onboarding-panel>header strong{font-size:12px;font-weight:500}.onboarding-panel>header small{font-size:9px;color:#638397}.onboarding-panel button{display:flex;align-items:center;gap:5px;border:1px solid #28536c;background:#0b263b;color:#a9c6d9;padding:6px 9px;font-size:9px;cursor:pointer}.onboarding-panel button.primary{background:#0b6da5;border-color:#1a93c8;color:#fff}.onboarding-panel button:disabled{opacity:.4}.onboarding-panel button svg{width:12px}.candidate-list,.request-list{padding:12px 18px;border-top:1px solid #173d58}.candidate-list h4,.request-list h4{font-size:10px;margin:0 0 8px}.candidate-list article{display:grid;grid-template-columns:22px 1fr auto;gap:8px;align-items:center;padding:8px;background:#061a2c;border-bottom:1px solid #15374e}.candidate-list article svg{width:17px;color:#ffc167}.candidate-list article div{display:grid;gap:3px}.candidate-list article strong{font-size:10px;font-weight:500}.candidate-list article span{font-size:9px;color:#66879b}.request-head,.request-row{display:grid;grid-template-columns:1.1fr 1.2fr 1fr .7fr 1.2fr 1fr;gap:6px;align-items:center;padding:7px 8px;font-size:8px}.request-head{background:#08243a;color:#608197}.request-row{background:#061a2c;border-bottom:1px solid #15374e;color:#9eb9c9}.request-row strong,.request-row small{display:block}.request-row small{color:#5f7f94}.request-row b{font-weight:500;color:#ffc167}.request-row b[data-status=onboarded]{color:#61dfa2}.request-row b[data-status=rejected]{color:#ff8292}.request-actions{display:flex!important;gap:4px}.request-actions button{padding:4px 6px;font-size:8px}.onboarding-message,.onboarding-error{margin:8px 18px;padding:7px;font-size:9px}.onboarding-message{background:rgba(68,214,145,.1);color:#61dfa2}.onboarding-error{background:rgba(255,82,104,.1);color:#ff8998}.onboarding-empty{padding:18px;text-align:center;color:#638397;font-size:9px}.onboarding-modal{position:fixed;inset:0;z-index:90;background:rgba(0,8,16,.78);display:grid;place-items:center}.onboarding-modal form{width:min(620px,92vw);background:#041a2e;border:1px solid #246c98}.onboarding-modal form>header{display:flex;justify-content:space-between;padding:14px 17px;border-bottom:1px solid #174d78}.onboarding-modal h3{margin:4px 0}.onboarding-modal header span{font-size:8px;color:#31c6e7;letter-spacing:2px}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:9px;padding:14px 17px}.form-grid label{display:grid;gap:4px}.form-grid label.wide{grid-column:1/-1}.form-grid label span{font-size:9px;color:#6b8da2}.form-grid input,.form-grid select{height:31px;border:1px solid #24536f;background:#031525;color:#c4dfed;padding:0 7px}.onboarding-modal footer{display:flex;justify-content:flex-end;gap:7px;padding:12px 17px;border-top:1px solid #173d58}@media(max-width:1100px){.request-head,.request-row{grid-template-columns:1fr 1fr .8fr .7fr 1fr}.request-row>span:nth-child(3){display:none}}
</style>
