<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { BellRing, Cable, CheckCircle2, ChevronRight, ClipboardCheck, Link2, Network, RefreshCw, Search, Server, ShieldAlert, Waypoints } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { acknowledgeNetworkAnomaly, createNetworkAnomalyTicket, fetchNetworkAnomalies, fetchNetworkDevices, fetchNetworkLinkHistory, fetchNetworkLinks, fetchVLANInventory, resolveNetworkAnomaly, syncNetworkAnomalies } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'
import NetworkAutomationPanel from './NetworkAutomationPanel.vue'
import NetworkTopologyDiffPanel from './NetworkTopologyDiffPanel.vue'
import NetworkCompliancePanel from './NetworkCompliancePanel.vue'
import NetworkOnboardingPanel from './NetworkOnboardingPanel.vue'
import type { NetworkAnomaly, NetworkDeviceInventory, NetworkLink, NetworkLinkHistoryEntry, VLANInventory } from '@/features/cmdb/types'

const auth = useAuthStore()
const router = useRouter()
const devices = ref<NetworkDeviceInventory[]>([])
const links = ref<NetworkLink[]>([])
const history = ref<NetworkLinkHistoryEntry[]>([])
const vlans = ref<VLANInventory[]>([])
const anomalies = ref<NetworkAnomaly[]>([])
const selected = ref<NetworkDeviceInventory>()
const query = ref('')
const status = ref('all')
const loading = ref(false)
const error = ref('')
const anomalyBusy = ref(false)
const anomalyMessage = ref('')

const filteredDevices = computed(() => devices.value.filter(item => {
  const text = `${item.asset.name} ${item.asset.ip} ${item.asset.owner}`.toLowerCase()
  if (query.value && !text.includes(query.value.toLowerCase())) return false
  if (status.value === 'online' && item.asset.status !== 'online') return false
  if (status.value === 'warning' && item.asset.status !== 'warning') return false
  return true
}))
const ports = computed(() => selected.value?.ports || [])
const portStatus = ref('all')
const portQuery = ref('')
const visiblePorts = computed(() => ports.value.filter(port => {
  const text = `${port.name} ${port.alias || ''} ${port.description || ''} ${port.neighborName || ''}`.toLowerCase()
  if (portQuery.value && !text.includes(portQuery.value.toLowerCase())) return false
  if (portStatus.value === 'up' && port.operStatus !== 'up') return false
  if (portStatus.value === 'down' && port.operStatus === 'up') return false
  return true
}))
const upPorts = computed(() => ports.value.filter(port => port.operStatus === 'up').length)
const lldpPorts = computed(() => ports.value.filter(port => Boolean(port.neighborName)).length)
const totalPorts = computed(() => devices.value.reduce((sum, device) => sum + device.totalPorts, 0))
const totalUpPorts = computed(() => devices.value.reduce((sum, device) => sum + device.upPorts, 0))
const confirmedLinks = computed(() => links.value.filter(link => link.verification === 'confirmed' || link.verification === 'lldp-confirmed').length)
const conflictLinks = computed(() => links.value.filter(link => link.verification === 'conflict').length)
const criticalAnomalies = computed(() => anomalies.value.filter(item => item.status !== 'resolved' && item.severity === 'critical').length)
const warningAnomalies = computed(() => anomalies.value.filter(item => item.status !== 'resolved' && item.severity === 'warning').length)

function deviceAttr(name:string){ return selected.value?.asset.attributes.find(attribute => attribute.name === name)?.value || '—' }
function aggregationLabel(port:{alias?:string;description?:string;neighborName?:string;neighborPort?:string}) { const text = `${port.alias || ''} ${port.description || ''}`.toLowerCase(); if (text.includes('eth-trunk') || text.includes('port-channel') || text.includes('portchannel') || text.includes('lacp') || text.includes('lag')) return '聚合端口'; if (port.neighborName) return 'LLDP 链路'; return '普通端口' }
function fdbSource() {
  const fdb = selected.value?.asset.attributes.find(attribute => attribute.name === 'network_fdb_source')?.value || '未采集'
  const arp = selected.value?.asset.attributes.find(attribute => attribute.name === 'network_arp_source')?.value || '未采集'
  return `MAC表 ${fdb} / ARP ${arp}`
}
function statusLabel(value: string) {
  return ({ up: '在线', down: '离线', testing: '测试', dormant: '休眠', unknown: '未知', 'not-present': '未安装', 'lower-layer-down': '下层故障' } as Record<string,string>)[value] || value || '未知'
}
function tone(value: string) {
  if (value === 'up') return 'healthy'
  if (value === 'down' || value === 'lower-layer-down') return 'critical'
  return 'unknown'
}
function speedLabel(value?: string) {
  if (!value) return '—'
  const speed = Number(value)
  if (!Number.isFinite(speed)) return value
  if (speed >= 100000) return `${(speed / 1000).toFixed(0)} Gbps`
  if (speed >= 1000) return `${(speed / 1000).toFixed(speed % 1000 ? 1 : 0)} Gbps`
  return `${speed} Mbps`
}
async function acknowledge(item:NetworkAnomaly) {
  anomalyBusy.value = true
  anomalyMessage.value = ''
  try {
    const updated = await acknowledgeNetworkAnomaly(auth.token, item.id, '平台确认处理')
    anomalies.value = anomalies.value.map(current => current.id === updated.id ? updated : current)
    anomalyMessage.value = '异常已确认，处理状态已记录'
  } catch (reason) {
    anomalyMessage.value = reason instanceof Error ? reason.message : '确认失败'
  } finally { anomalyBusy.value = false }
}
async function resolve(item:NetworkAnomaly) {
  anomalyBusy.value = true
  anomalyMessage.value = ''
  try {
    const updated = await resolveNetworkAnomaly(auth.token, item.id, '平台确认恢复')
    anomalies.value = anomalies.value.map(current => current.id === updated.id ? updated : current)
    anomalyMessage.value = '异常已关闭，并同步关闭关联工单'
  } catch (reason) {
    anomalyMessage.value = reason instanceof Error ? reason.message : '关闭失败'
  } finally { anomalyBusy.value = false }
}
async function createTicket(item:NetworkAnomaly) {
  anomalyBusy.value = true
  anomalyMessage.value = ''
  try {
    const result = await createNetworkAnomalyTicket(auth.token, item.id)
    anomalyMessage.value = result.created ? '已创建异常工单' : '异常工单已存在，已复用'
  } catch (reason) {
    anomalyMessage.value = reason instanceof Error ? reason.message : '派单失败'
  } finally { anomalyBusy.value = false }
}
async function syncAlerts() {
  anomalyBusy.value = true
  anomalyMessage.value = ''
  try {
    const result = await syncNetworkAnomalies(auth.token)
    anomalyMessage.value = `已同步 ${result.synced} 条网络异常到告警中心`
  } catch (reason) {
    anomalyMessage.value = reason instanceof Error ? reason.message : '同步告警失败'
  } finally { anomalyBusy.value = false }
}
function anomalyStatusLabel(status:string) { return ({open:'待处理','acknowledged':'已确认','resolved':'已关闭'} as Record<string,string>)[status] || status }
async function load() {
  loading.value = true
  error.value = ''
  try {
    const results = await Promise.all([fetchNetworkDevices(auth.token), fetchNetworkLinks(auth.token), fetchNetworkLinkHistory(auth.token, 12), fetchVLANInventory(auth.token), fetchNetworkAnomalies(auth.token)])
    devices.value = results[0]
    links.value = results[1]
    history.value = results[2]
    vlans.value = results[3]
    anomalies.value = results[4]
    if (!selected.value || !devices.value.some(item => item.asset.id === selected.value?.asset.id)) {
      selected.value = devices.value[0]
    } else {
      selected.value = devices.value.find(item => item.asset.id === selected.value?.asset.id)
    }
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '网络设备加载失败'
  } finally {
    loading.value = false
  }
}
onMounted(load)
</script>

<template>
  <section class="network-inventory-panel">
    <header class="network-panel-heading">
      <div><span>PHYSICAL NETWORK INVENTORY</span><h2>网络设备与端口清单</h2><p>统一查看交换机端口状态、VLAN、速率和 LLDP 邻居关系。</p></div>
      <div><button @click="router.push('/topology')"><Waypoints/>打开物理拓扑</button><button class="primary" :disabled="loading" @click="load"><RefreshCw :class="{spin:loading}"/>刷新</button></div>
    </header>
    <div class="network-stats">
      <article><Network/><span>网络设备<strong>{{ devices.length }}</strong></span></article>
      <article><Cable/><span>端口总数<strong>{{ totalPorts }}</strong></span></article>
      <article><Cable/><span>在线端口<strong>{{ totalUpPorts }}</strong></span></article>
      <article><Waypoints/><span>LLDP 邻居<strong>{{ devices.reduce((sum,item)=>sum+(item.ports?.filter(p=>p.neighborName).length||0),0) }}</strong></span></article>
      <article><Link2/><span>已确认链路<strong>{{ confirmedLinks }}</strong></span></article>
      <article><ShieldAlert/><span>待复核链路<strong>{{ conflictLinks }}</strong></span></article>
      <article><Network/><span>VLAN 数量<strong>{{ vlans.length }}</strong></span></article>
      <article><ShieldAlert/><span>网络异常<strong>{{ criticalAnomalies }} / {{ warningAnomalies }}</strong></span></article>
    </div>
    <div v-if="error" class="network-error">{{ error }}</div>
    <div class="network-layout">
      <aside class="device-list">
        <div class="device-filters"><label><Search/><input v-model="query" placeholder="搜索设备、IP 或负责人"></label><select v-model="status"><option value="all">全部状态</option><option value="online">在线</option><option value="warning">异常</option></select></div>
        <button v-for="device in filteredDevices" :key="device.asset.id" :class="{active:selected?.asset.id===device.asset.id}" @click="selected=device">
          <i :data-status="device.asset.status"></i><span><strong>{{ device.asset.name }}</strong><small>{{ device.asset.ip }} · {{ device.asset.owner }}</small></span><ChevronRight/>
        </button>
        <div v-if="!filteredDevices.length" class="network-empty">暂无匹配的网络设备</div>
      </aside>
      <main v-if="selected" class="port-workspace">
        <header><div><span>SNMP NETWORK DEVICE</span><h3>{{ selected.asset.name }}</h3><small>{{ selected.asset.ip }} · {{ selected.asset.location || '位置待维护' }} · {{ selected.asset.source }}</small></div><div class="port-summary"><b>{{ selected.totalPorts }}</b><span>端口</span><b>{{ selected.upPorts }}</b><span>UP</span><b>{{ selected.totalPorts-selected.upPorts }}</b><span>DOWN</span></div></header>
        <div class="device-meta"><span><small>厂商</small><strong>{{ deviceAttr('vendor') }}</strong></span><span><small>型号</small><strong>{{ deviceAttr('model') }}</strong></span><span><small>序列号</small><strong>{{ deviceAttr('serial') }}</strong></span><span><small>SNMP</small><strong>{{ deviceAttr('snmp_discovered') === 'true' ? '已采集' : '待采集' }}</strong></span><span><small>最近采集</small><strong>{{ deviceAttr('network_discovery_at') }}</strong></span><span><small>MAC / ARP</small><strong>{{ fdbSource() }}</strong></span></div>
        <div class="port-toolbar"><label><Search/><input v-model="portQuery" placeholder="搜索端口、描述或对端"></label><select v-model="portStatus"><option value="all">全部端口</option><option value="up">仅 UP</option><option value="down">仅 DOWN</option></select><span>显示 {{ visiblePorts.length }} / {{ ports.length }}</span></div>
        <div class="port-table">
          <div class="port-row port-head"><span>端口</span><span>状态</span><span>速率</span><span>VLAN</span><span>链路类型</span><span>MAC 地址</span><span>对端设备 / 端口</span><span>最近采集</span></div>
          <div v-for="port in visiblePorts" :key="port.id" class="port-row">
            <span><Cable/><i><strong>{{ port.name }}</strong><small>{{ port.alias || port.description || `ifIndex ${port.ifIndex}` }}</small></i></span>
            <span><b class="port-status" :data-status="tone(port.operStatus)">{{ statusLabel(port.operStatus) }}</b></span>
            <span>{{ speedLabel(port.speedMbps) }}</span><span>{{ port.vlan || '—' }}</span><span class="mono">{{ port.macAddress || (port.macAddresses?.length ? `${port.macAddresses.length} 个学习 MAC` : '—') }}<small v-if="port.arpEntries?.length">{{port.arpEntries.length}} 个 ARP</small></span>
            <span><template v-if="port.neighborName"><strong>{{ port.neighborName }}</strong><small>{{ port.neighborPort || port.neighborChassis || 'LLDP' }}</small></template><template v-else>—</template></span><span>{{ port.lastSeenAt || '—' }}</span>
          </div>
          <div v-if="!ports.length" class="network-empty"><Server/>当前设备尚未采集到端口清单，请先执行 SNMP 采集。</div>
        </div>
        <footer><span>在线 {{ upPorts }} / 共 {{ ports.length }} 个端口</span><span>LLDP 邻居 {{ lldpPorts }} 条</span></footer>
      </main>
    </div>
    <section class="link-history">
      <header><div><Link2/><span><strong>最近链路变更</strong><small>人工绑定、解绑和拓扑快照触发记录</small></span></div></header>
      <div class="link-history-list">
        <article v-for="entry in history" :key="entry.id"><i></i><div><strong>{{ entry.assetName || entry.assetId }}</strong><span>{{ entry.changes.map(change=>`${change.before || '空'} → ${change.after || '空'}`).join('；') }}</span></div><time>{{ entry.occurredAt }}</time><b>{{ entry.operator }}</b></article>
        <div v-if="!history.length" class="network-empty">暂无链路变更记录</div>
      </div>
    </section>
    <section class="vlan-overview"><header><div><Network/><span><strong>VLAN 端口视图</strong><small>端口、ARP 和 MAC 自动归组</small></span></div></header><div class="vlan-list"><article v-for="vlan in vlans" :key="vlan.vlan"><strong>VLAN {{vlan.vlan}}</strong><span>{{vlan.memberCount}} 个成员端口 · {{vlan.upMembers}} 在线</span><small>{{vlan.learnedMacCount}} MAC · {{vlan.arpEntryCount}} ARP</small></article><div v-if="!vlans.length" class="network-empty">暂未采集到 VLAN 端口数据</div></div></section>
    <section class="anomaly-section">
      <header><div><ShieldAlert/><span><strong>网络异常与漂移</strong><small>IP/MAC 冲突、端口漂移和链路异常</small></span></div><button :disabled="anomalyBusy" @click="syncAlerts"><BellRing/>同步告警中心</button></header>
      <div v-if="anomalyMessage" class="anomaly-message"><CheckCircle2/>{{anomalyMessage}}</div>
      <div class="anomaly-list">
        <article v-for="item in anomalies" :key="item.id" :data-severity="item.severity" :data-status="item.status"><i></i><div><strong>{{item.title}}</strong><span>{{item.detail}}</span><small v-if="item.evidence?.length">{{item.evidence.join('；')}}</small><small>状态：{{anomalyStatusLabel(item.status)}}<template v-if="item.acknowledgedBy"> · {{item.acknowledgedBy}}</template><template v-if="item.note"> · {{item.note}}</template></small></div><b>{{item.severity==='critical'?'严重':'关注'}}</b><span class="anomaly-actions"><button :disabled="anomalyBusy||item.status!=='open'" @click="acknowledge(item)">确认</button><button :disabled="anomalyBusy" @click="createTicket(item)"><ClipboardCheck/>派单</button><button :disabled="anomalyBusy||item.status==='resolved'" @click="resolve(item)">关闭</button></span></article>
        <div v-if="!anomalies.length" class="network-empty">当前没有检测到网络异常</div>
      </div>
    </section>
    <NetworkAutomationPanel />`r`n    <NetworkTopologyDiffPanel />`r`n    <NetworkCompliancePanel />`r`n    <NetworkOnboardingPanel />
  </section>
</template>

<style scoped>
.network-inventory-panel{margin-top:14px;border:1px solid #174d78;background:rgba(4,24,43,.72);color:#cfe6f4}.network-panel-heading{display:flex;justify-content:space-between;align-items:center;padding:18px;border-bottom:1px solid #173d58}.network-panel-heading span{font-size:9px;letter-spacing:2px;color:#25cce5}.network-panel-heading h2{margin:5px 0 0;font-weight:500}.network-panel-heading p{margin:6px 0 0;color:#6e8fa5;font-size:11px}.network-panel-heading>div:last-child{display:flex;gap:8px}.network-panel-heading button{height:34px;padding:0 12px;border:1px solid #285772;background:#0b2a42;color:#bcd8e8;display:flex;align-items:center;gap:7px;cursor:pointer}.network-panel-heading button.primary{background:#0d6fa8;border-color:#1b93cd;color:#fff}.network-panel-heading svg{width:15px}.network-stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(145px,1fr));gap:9px;padding:13px 18px}.network-stats article{display:flex;gap:10px;align-items:center;padding:12px;border:1px solid #17415e;background:#061a2c}.network-stats svg{width:20px;color:#32b8e8}.network-stats span{display:grid;gap:4px;color:#6f90a5;font-size:10px}.network-stats strong{font-size:19px;color:#d9f1fb;font-weight:500}.network-layout{display:grid;grid-template-columns:300px 1fr;min-height:520px;border-top:1px solid #173d58}.device-list{border-right:1px solid #173d58;background:#041729;padding:10px}.device-filters{display:grid;grid-template-columns:1fr 92px;gap:7px;margin-bottom:10px}.device-filters label{display:flex;align-items:center;border:1px solid #24536f;background:#031525;padding:0 8px}.device-filters input{border:0;background:none;color:#d7ebf6;outline:0;width:100%;padding:8px}.device-filters select{border:1px solid #24536f;background:#071e32;color:#91afc1;padding:0 6px}.device-list>button{width:100%;display:grid;grid-template-columns:9px 1fr 16px;gap:9px;align-items:center;text-align:left;padding:11px 9px;border:1px solid transparent;background:transparent;color:#8faabd;cursor:pointer}.device-list>button:hover,.device-list>button.active{border-color:#1a6389;background:#082841;color:#ddf3ff}.device-list>button>i{width:7px;height:7px;border-radius:50%;background:#48d89a}.device-list>button>i[data-status=warning]{background:#ffb34d}.device-list>button>i[data-status=offline]{background:#718697}.device-list button span{display:grid;gap:3px}.device-list button strong{font-weight:500;color:#d4ebf8}.device-list button small{font-size:9px;color:#607f94}.device-list svg{width:14px}.port-workspace{min-width:0;background:#051b2e}.port-workspace>header{display:flex;justify-content:space-between;align-items:center;padding:14px 16px;border-bottom:1px solid #173d58}.port-workspace>header span{color:#2fc7e3;font-size:9px}.port-workspace>header h3{margin:4px 0;font-weight:500}.port-workspace>header small{color:#6b8da2}.port-summary{display:grid;grid-template-columns:auto auto;gap:2px 7px;align-items:baseline;text-align:right}.port-summary b{font-size:20px;color:#38d9ef;font-weight:500}.port-summary span{color:#7594a8}.port-table{overflow:auto}.port-row{min-width:1040px;display:grid;grid-template-columns:1.05fr .5fr .6fr .5fr .75fr .95fr 1.2fr .8fr;gap:8px;align-items:center;padding:10px 14px;border-bottom:1px solid rgba(35,79,108,.45);font-size:10px;color:#9bb8ca}.port-head{background:#08243a;color:#5f8499;font-size:9px}.port-row>span{min-width:0;display:grid;gap:3px}.port-row>span:first-child{grid-template-columns:15px 1fr;align-items:center}.port-row i{font-style:normal;display:grid;gap:2px}.port-row strong{font-weight:500;color:#d2e8f4;overflow:hidden;text-overflow:ellipsis}.port-row small{color:#5f7f94;font-size:9px;overflow:hidden;text-overflow:ellipsis}.port-row svg{width:14px;color:#32b9e6}.port-status{display:inline-block;width:max-content;padding:3px 7px;background:rgba(72,216,154,.11);color:#62e2a8}.port-status[data-status=critical]{background:rgba(255,83,105,.12);color:#ff8292}.port-status[data-status=unknown]{background:rgba(120,139,153,.12);color:#9badb8}.mono{font-family:Consolas,monospace;color:#79bdd8}.device-meta{display:grid;grid-template-columns:repeat(6,1fr);gap:1px;background:#15374f;border-top:1px solid #15374f}.device-meta>span{display:grid;gap:4px;padding:10px 12px;background:#061a2c}.device-meta small{color:#638397;font-size:9px}.device-meta strong{color:#cfe7f3;font-size:10px;font-weight:500}.port-toolbar{display:flex;align-items:center;gap:8px;padding:10px 14px;border-top:1px solid #173d58;background:#071c2c}.port-toolbar label{display:flex;align-items:center;gap:6px;flex:1;max-width:360px;height:30px;border:1px solid #28536c;background:#031525;padding:0 8px}.port-toolbar label svg{width:13px;color:#648397}.port-toolbar input{flex:1;border:0;outline:none;background:transparent;color:#d5edf8;font-size:10px}.port-toolbar select{height:30px;border:1px solid #28536c;background:#031525;color:#d5edf8;font-size:10px}.port-toolbar span{color:#7890a8;font-size:9px}.port-workspace footer{display:flex;justify-content:space-between;padding:10px 14px;color:#65869a;font-size:9px}.network-empty{min-height:150px;display:flex;align-items:center;justify-content:center;gap:8px;color:#638196}.network-empty svg{width:18px}.vlan-overview,.anomaly-section{border-top:1px solid #173d58;padding:13px 18px}.vlan-overview>header>div,.anomaly-section>header>div{display:flex;align-items:center;gap:8px}.vlan-overview>header svg,.anomaly-section>header svg{width:16px;color:#2fc7e3}.vlan-overview>header span,.anomaly-section>header span{display:grid;gap:3px}.vlan-overview>header strong,.anomaly-section>header strong{font-size:11px;font-weight:500}.vlan-overview>header small,.anomaly-section>header small{font-size:9px;color:#638397}.vlan-list{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:7px;margin-top:10px}.vlan-list article{display:grid;gap:4px;padding:9px;background:#061a2c;border:1px solid #153d57}.vlan-list strong{color:#69d8f1;font-weight:500}.vlan-list span{font-size:9px;color:#9eb9c9}.vlan-list small{font-size:8px;color:#648397}.anomaly-list{display:grid;gap:6px;margin-top:10px}.anomaly-list article{display:grid;grid-template-columns:8px 1fr auto auto;gap:8px;align-items:center;padding:8px 10px;background:#061a2c;border:1px solid #153d57}.anomaly-list article>i{width:7px;height:7px;border-radius:50%;background:#ffb34d}.anomaly-list article[data-severity=critical]>i{background:#ff5b6b;box-shadow:0 0 8px #ff5b6b}.anomaly-list article>div{display:grid;gap:3px}.anomaly-list strong{font-size:10px;font-weight:500;color:#d4eaf6}.anomaly-list span{font-size:9px;color:#7897aa}.anomaly-list small{font-size:8px;color:#5f7f94}.anomaly-list b{font-size:9px;color:#ffc167}.anomaly-list article[data-severity=critical] b{color:#ff8292}.anomaly-list article[data-status=acknowledged]{opacity:.82}.anomaly-list article[data-status=resolved]{opacity:.55}.anomaly-section>header{display:flex;justify-content:space-between;align-items:center}.anomaly-section>header button{height:29px;padding:0 9px;border:1px solid #245b80;background:#082943;color:#b8d7e7;display:flex;align-items:center;gap:5px;cursor:pointer}.anomaly-section>header button svg{width:13px}.anomaly-actions{display:flex!important;gap:4px}.anomaly-actions button{display:flex;align-items:center;gap:3px;border:1px solid #28536c;background:#0b263b;color:#a9c6d9;padding:4px 6px;font-size:8px;cursor:pointer}.anomaly-actions button:disabled{opacity:.35;cursor:not-allowed}.anomaly-actions svg{width:11px}.anomaly-message{display:flex;align-items:center;gap:6px;margin-top:8px;padding:7px;background:rgba(68,214,145,.1);color:#61dfa2;font-size:9px}.anomaly-message svg{width:13px}.network-error{padding:9px 18px;background:rgba(255,88,108,.08);color:#ff8393}.link-history{border-top:1px solid #173d58;padding:13px 18px}.link-history>header>div{display:flex;gap:8px;align-items:center}.link-history>header svg{width:16px;color:#2fc7e3}.link-history>header span{display:grid;gap:3px}.link-history>header strong{font-size:11px;font-weight:500}.link-history>header small{font-size:9px;color:#638397}.link-history-list{display:grid;gap:5px;margin-top:10px}.link-history-list article{display:grid;grid-template-columns:8px 1fr 145px 90px;gap:8px;align-items:center;padding:8px 10px;background:#061a2c;font-size:9px}.link-history-list article>i{width:6px;height:6px;border-radius:50%;background:#38cbe8}.link-history-list article>div{display:grid;gap:3px}.link-history-list strong{font-weight:500;color:#d4eaf6}.link-history-list span{color:#66869b}.link-history-list time{color:#7090a4}.link-history-list b{font-weight:500;color:#9ebac9}.spin{animation:spin 1s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}@media(max-width:1200px){.network-stats{grid-template-columns:repeat(2,1fr)}.network-layout{grid-template-columns:250px 1fr}}
</style>
