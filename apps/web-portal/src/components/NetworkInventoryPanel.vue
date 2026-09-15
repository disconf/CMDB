<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Cable, ChevronRight, Network, RefreshCw, Search, Server, Waypoints } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { fetchNetworkDevices } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'
import type { NetworkDeviceInventory } from '@/features/cmdb/types'

const auth = useAuthStore()
const router = useRouter()
const devices = ref<NetworkDeviceInventory[]>([])
const selected = ref<NetworkDeviceInventory>()
const query = ref('')
const status = ref('all')
const loading = ref(false)
const error = ref('')

const filteredDevices = computed(() => devices.value.filter(item => {
  const text = `${item.asset.name} ${item.asset.ip} ${item.asset.owner}`.toLowerCase()
  if (query.value && !text.includes(query.value.toLowerCase())) return false
  if (status.value === 'online' && item.asset.status !== 'online') return false
  if (status.value === 'warning' && item.asset.status !== 'warning') return false
  return true
}))
const ports = computed(() => selected.value?.ports || [])
const upPorts = computed(() => ports.value.filter(port => port.operStatus === 'up').length)
const lldpPorts = computed(() => ports.value.filter(port => Boolean(port.neighborName)).length)
const totalPorts = computed(() => devices.value.reduce((sum, device) => sum + device.totalPorts, 0))
const totalUpPorts = computed(() => devices.value.reduce((sum, device) => sum + device.upPorts, 0))

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
async function load() {
  loading.value = true
  error.value = ''
  try {
    devices.value = await fetchNetworkDevices(auth.token)
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
        <header><div><span>{{ selected.asset.typeName }}</span><h3>{{ selected.asset.name }}</h3><small>{{ selected.asset.ip }} · {{ selected.asset.location || '位置待维护' }} · {{ selected.asset.source }}</small></div><div class="port-summary"><b>{{ selected.totalPorts }}</b><span>端口</span><b>{{ selected.upPorts }}</b><span>在线</span></div></header>
        <div class="port-table">
          <div class="port-row port-head"><span>端口</span><span>状态</span><span>速率</span><span>VLAN</span><span>MAC 地址</span><span>对端设备 / 端口</span><span>最近采集</span></div>
          <div v-for="port in ports" :key="port.id" class="port-row">
            <span><Cable/><i><strong>{{ port.name }}</strong><small>{{ port.alias || port.description || `ifIndex ${port.ifIndex}` }}</small></i></span>
            <span><b class="port-status" :data-status="tone(port.operStatus)">{{ statusLabel(port.operStatus) }}</b></span>
            <span>{{ speedLabel(port.speedMbps) }}</span><span>{{ port.vlan || '—' }}</span><span class="mono">{{ port.macAddress || '—' }}</span>
            <span><template v-if="port.neighborName"><strong>{{ port.neighborName }}</strong><small>{{ port.neighborPort || port.neighborChassis || 'LLDP' }}</small></template><template v-else>—</template></span><span>{{ port.lastSeenAt || '—' }}</span>
          </div>
          <div v-if="!ports.length" class="network-empty"><Server/>当前设备尚未采集到端口清单，请先执行 SNMP 采集。</div>
        </div>
        <footer><span>在线 {{ upPorts }} / 共 {{ ports.length }} 个端口</span><span>LLDP 邻居 {{ lldpPorts }} 条</span></footer>
      </main>
    </div>
  </section>
</template>

<style scoped>
.network-inventory-panel{margin-top:14px;border:1px solid #174d78;background:rgba(4,24,43,.72);color:#cfe6f4}.network-panel-heading{display:flex;justify-content:space-between;align-items:center;padding:18px;border-bottom:1px solid #173d58}.network-panel-heading span{font-size:9px;letter-spacing:2px;color:#25cce5}.network-panel-heading h2{margin:5px 0 0;font-weight:500}.network-panel-heading p{margin:6px 0 0;color:#6e8fa5;font-size:11px}.network-panel-heading>div:last-child{display:flex;gap:8px}.network-panel-heading button{height:34px;padding:0 12px;border:1px solid #285772;background:#0b2a42;color:#bcd8e8;display:flex;align-items:center;gap:7px;cursor:pointer}.network-panel-heading button.primary{background:#0d6fa8;border-color:#1b93cd;color:#fff}.network-panel-heading svg{width:15px}.network-stats{display:grid;grid-template-columns:repeat(4,1fr);gap:9px;padding:13px 18px}.network-stats article{display:flex;gap:10px;align-items:center;padding:12px;border:1px solid #17415e;background:#061a2c}.network-stats svg{width:20px;color:#32b8e8}.network-stats span{display:grid;gap:4px;color:#6f90a5;font-size:10px}.network-stats strong{font-size:19px;color:#d9f1fb;font-weight:500}.network-layout{display:grid;grid-template-columns:300px 1fr;min-height:520px;border-top:1px solid #173d58}.device-list{border-right:1px solid #173d58;background:#041729;padding:10px}.device-filters{display:grid;grid-template-columns:1fr 92px;gap:7px;margin-bottom:10px}.device-filters label{display:flex;align-items:center;border:1px solid #24536f;background:#031525;padding:0 8px}.device-filters input{border:0;background:none;color:#d7ebf6;outline:0;width:100%;padding:8px}.device-filters select{border:1px solid #24536f;background:#071e32;color:#91afc1;padding:0 6px}.device-list>button{width:100%;display:grid;grid-template-columns:9px 1fr 16px;gap:9px;align-items:center;text-align:left;padding:11px 9px;border:1px solid transparent;background:transparent;color:#8faabd;cursor:pointer}.device-list>button:hover,.device-list>button.active{border-color:#1a6389;background:#082841;color:#ddf3ff}.device-list>button>i{width:7px;height:7px;border-radius:50%;background:#48d89a}.device-list>button>i[data-status=warning]{background:#ffb34d}.device-list>button>i[data-status=offline]{background:#718697}.device-list button span{display:grid;gap:3px}.device-list button strong{font-weight:500;color:#d4ebf8}.device-list button small{font-size:9px;color:#607f94}.device-list svg{width:14px}.port-workspace{min-width:0;background:#051b2e}.port-workspace>header{display:flex;justify-content:space-between;align-items:center;padding:14px 16px;border-bottom:1px solid #173d58}.port-workspace>header span{color:#2fc7e3;font-size:9px}.port-workspace>header h3{margin:4px 0;font-weight:500}.port-workspace>header small{color:#6b8da2}.port-summary{display:grid;grid-template-columns:auto auto;gap:2px 7px;align-items:baseline;text-align:right}.port-summary b{font-size:20px;color:#38d9ef;font-weight:500}.port-summary span{color:#7594a8}.port-table{overflow:auto}.port-row{min-width:900px;display:grid;grid-template-columns:1.15fr .55fr .7fr .55fr 1fr 1.35fr .9fr;gap:8px;align-items:center;padding:10px 14px;border-bottom:1px solid rgba(35,79,108,.45);font-size:10px;color:#9bb8ca}.port-head{background:#08243a;color:#5f8499;font-size:9px}.port-row>span{min-width:0;display:grid;gap:3px}.port-row>span:first-child{grid-template-columns:15px 1fr;align-items:center}.port-row i{font-style:normal;display:grid;gap:2px}.port-row strong{font-weight:500;color:#d2e8f4;overflow:hidden;text-overflow:ellipsis}.port-row small{color:#5f7f94;font-size:9px;overflow:hidden;text-overflow:ellipsis}.port-row svg{width:14px;color:#32b9e6}.port-status{display:inline-block;width:max-content;padding:3px 7px;background:rgba(72,216,154,.11);color:#62e2a8}.port-status[data-status=critical]{background:rgba(255,83,105,.12);color:#ff8292}.port-status[data-status=unknown]{background:rgba(120,139,153,.12);color:#9badb8}.mono{font-family:Consolas,monospace;color:#79bdd8}.port-workspace footer{display:flex;justify-content:space-between;padding:10px 14px;color:#65869a;font-size:9px}.network-empty{min-height:150px;display:flex;align-items:center;justify-content:center;gap:8px;color:#638196}.network-empty svg{width:18px}.network-error{padding:9px 18px;background:rgba(255,88,108,.08);color:#ff8393}.spin{animation:spin 1s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}@media(max-width:1200px){.network-stats{grid-template-columns:repeat(2,1fr)}.network-layout{grid-template-columns:250px 1fr}}
</style>
