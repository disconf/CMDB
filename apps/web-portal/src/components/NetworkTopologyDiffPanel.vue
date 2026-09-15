<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { AlertTriangle, Camera, GitCompare, RefreshCw, ShieldAlert } from 'lucide-vue-next'
import { captureNetworkTopologySnapshot, fetchNetworkTopologyDiff, fetchNetworkTopologySnapshots } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'
import type { NetworkDiff, NetworkDiffChange, TopologySnapshot } from '@/features/cmdb/types'

const auth = useAuthStore()
const snapshots = ref<TopologySnapshot[]>([])
const diff = ref<NetworkDiff>()
const from = ref('')
const to = ref('')
const kind = ref('all')
const action = ref('all')
const busy = ref(false)
const message = ref('')
const error = ref('')

const changes = computed(() => (diff.value?.changes || []).filter(item => (kind.value === 'all' || item.kind === kind.value) && (action.value === 'all' || item.action === action.value)))
function actionLabel(value:string) { return ({ added:'新增', removed:'消失', changed:'变更' } as Record<string,string>)[value] || value }
function kindLabel(value:string) { return ({ device:'设备', port:'端口', vlan:'VLAN', link:'链路', anomaly:'异常' } as Record<string,string>)[value] || value }
function changeTone(item:NetworkDiffChange) { if (item.kind === 'anomaly' || item.name.includes('未授权')) return 'critical'; if (item.action === 'removed') return 'warning'; return item.action === 'added' ? 'healthy' : 'warning' }
async function load() {
  busy.value = true; error.value = ''
  try {
    snapshots.value = await fetchNetworkTopologySnapshots(auth.token, 30)
    if (!to.value && snapshots.value[0]) to.value = snapshots.value[0].id
    if (!from.value && snapshots.value[1]) from.value = snapshots.value[1].id
    diff.value = await fetchNetworkTopologyDiff(auth.token, from.value, to.value)
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '拓扑对比加载失败' } finally { busy.value = false }
}
async function capture() {
  busy.value = true; message.value = ''; error.value = ''
  try {
    const item = await captureNetworkTopologySnapshot(auth.token)
    message.value = `已保存网络快照 ${item.createdAt}`
    await load()
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '快照生成失败' } finally { busy.value = false }
}
async function compare() {
  busy.value = true; error.value = ''
  try { diff.value = await fetchNetworkTopologyDiff(auth.token, from.value, to.value) } catch (reason) { error.value = reason instanceof Error ? reason.message : '对比失败' } finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <section class="diff-panel">
    <header><div><GitCompare/><span><strong>网络拓扑历史对比</strong><small>识别新增设备、端口漂移、链路消失和未授权接入</small></span></div><div><button :disabled="busy" @click="capture"><Camera/>保存快照</button><button class="primary" :disabled="busy" @click="load"><RefreshCw/>刷新对比</button></div></header>
    <div class="diff-selectors"><label><span>基准快照</span><select v-model="from"><option v-for="item in snapshots" :key="item.id" :value="item.id">{{item.createdAt}} · {{item.nodeCount}} 节点</option></select></label><label><span>目标快照</span><select v-model="to"><option v-for="item in snapshots" :key="item.id" :value="item.id">{{item.createdAt}} · {{item.nodeCount}} 节点</option></select></label><button :disabled="busy||!from||!to" @click="compare"><GitCompare/>开始对比</button></div>
    <div v-if="message" class="diff-message">{{message}}</div><div v-if="error" class="diff-error">{{error}}</div>
    <div v-if="diff" class="diff-summary"><article><strong>{{diff.summary.added}}</strong><span>新增</span></article><article><strong>{{diff.summary.removed}}</strong><span>消失</span></article><article><strong>{{diff.summary.changed}}</strong><span>变更</span></article><article class="critical"><ShieldAlert/><strong>{{diff.summary.unknownAccess}}</strong><span>未授权接入</span></article></div>
    <div class="diff-toolbar"><select v-model="kind"><option value="all">全部类型</option><option value="device">设备</option><option value="port">端口</option><option value="vlan">VLAN</option><option value="link">链路</option><option value="anomaly">异常</option></select><select v-model="action"><option value="all">全部动作</option><option value="added">新增</option><option value="removed">消失</option><option value="changed">变更</option></select><span>共 {{changes.length}} 条差异</span></div>
    <div class="diff-table"><div class="diff-head"><span>类型</span><span>动作</span><span>对象</span><span>说明</span><span>变化前</span><span>变化后</span></div><div v-for="item in changes" :key="`${item.kind}-${item.action}-${item.id}`" class="diff-row" :data-tone="changeTone(item)"><span>{{kindLabel(item.kind)}}</span><b>{{actionLabel(item.action)}}</b><span><strong>{{item.name}}</strong><small>{{item.id}}</small></span><span>{{item.detail}}</span><code>{{item.before||'-'}}</code><code>{{item.after||'-'}}</code></div><div v-if="!changes.length" class="diff-empty"><AlertTriangle/>当前没有符合条件的拓扑差异</div></div>
  </section>
</template>

<style scoped>
.diff-panel{margin-top:14px;border:1px solid #174d78;background:rgba(4,24,43,.72)}.diff-panel>header{display:flex;justify-content:space-between;align-items:center;padding:14px 18px;border-bottom:1px solid #173d58}.diff-panel>header>div:first-child{display:flex;align-items:center;gap:8px}.diff-panel>header svg{width:18px;color:#31cbea}.diff-panel>header span{display:grid;gap:3px}.diff-panel>header strong{font-size:12px;font-weight:500}.diff-panel>header small{font-size:9px;color:#638397}.diff-panel>header>div:last-child{display:flex;gap:7px}.diff-panel button{display:flex;align-items:center;gap:5px;border:1px solid #28536c;background:#0b263b;color:#a9c6d9;padding:6px 9px;font-size:9px;cursor:pointer}.diff-panel button.primary{background:#0b6da5;border-color:#1a93c8;color:#fff}.diff-panel button:disabled{opacity:.4}.diff-panel button svg{width:12px}.diff-selectors{display:grid;grid-template-columns:1fr 1fr auto;gap:9px;align-items:end;padding:12px 18px}.diff-selectors label{display:grid;gap:4px}.diff-selectors span{font-size:9px;color:#66879b}.diff-selectors select,.diff-toolbar select{height:31px;border:1px solid #24536f;background:#031525;color:#c4dfed;padding:0 8px}.diff-summary{display:grid;grid-template-columns:repeat(4,1fr);gap:7px;padding:0 18px 12px}.diff-summary article{display:flex;align-items:center;justify-content:center;gap:9px;padding:10px;background:#061a2c;border:1px solid #153d57}.diff-summary strong{font-size:20px;font-weight:500;color:#67d9f3}.diff-summary span{font-size:9px;color:#66879b}.diff-summary svg{width:18px;color:#ff8292}.diff-summary .critical{border-color:#7f3647}.diff-toolbar{display:flex;gap:7px;align-items:center;padding:0 18px 10px}.diff-toolbar span{margin-left:auto;font-size:9px;color:#66879b}.diff-table{padding:0 18px 16px}.diff-head,.diff-row{display:grid;grid-template-columns:70px 60px 1.2fr 1.7fr 1fr 1fr;gap:7px;align-items:center;padding:7px 8px;font-size:8px}.diff-head{background:#08243a;color:#608197}.diff-row{background:#061a2c;border-bottom:1px solid #15374e;color:#9eb9c9}.diff-row b{font-weight:500;color:#61dfa2}.diff-row[data-tone=warning] b{color:#ffc167}.diff-row[data-tone=critical] b{color:#ff8292}.diff-row strong,.diff-row small{display:block}.diff-row small{color:#5f7f94}.diff-row code{font-family:Consolas,monospace;color:#77b9d6;overflow:hidden;text-overflow:ellipsis}.diff-message,.diff-error{margin:8px 18px;padding:7px;font-size:9px}.diff-message{background:rgba(68,214,145,.1);color:#61dfa2}.diff-error{background:rgba(255,82,104,.1);color:#ff8998}.diff-empty{display:flex;align-items:center;justify-content:center;gap:7px;padding:24px;color:#638397;font-size:9px}.diff-empty svg{width:16px}@media(max-width:1100px){.diff-selectors{grid-template-columns:1fr}.diff-summary{grid-template-columns:repeat(2,1fr)}.diff-head,.diff-row{grid-template-columns:60px 50px 1fr 1.2fr}}
</style>
