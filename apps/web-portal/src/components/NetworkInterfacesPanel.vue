<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Cable, Check, Link2, LoaderCircle, Network, Unlink } from 'lucide-vue-next'
import { createNetworkLink, deleteNetworkLink, fetchNetworkDevices, fetchNetworkInterfaces } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'
import type { NetworkDeviceInventory, NetworkInterface } from '@/features/cmdb/types'

const props = defineProps<{ assetId:string }>()
const auth = useAuthStore()
const interfaces = ref<NetworkInterface[]>([])
const devices = ref<NetworkDeviceInventory[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const selectedInterface = ref('')
const manualInterface = ref('')
const targetDeviceID = ref('')
const targetPort = ref('')
const manualPort = ref('')

const selectedDevice = computed(() => devices.value.find(item => item.asset.id === targetDeviceID.value))
const targetPorts = computed(() => selectedDevice.value?.ports || [])
const activeInterface = computed(() => selectedInterface.value || manualInterface.value)
const canBind = computed(() => Boolean(activeInterface.value.trim() && targetDeviceID.value && (targetPort.value || manualPort.value).trim()))

function interfaceStatus(value:string) {
  const normalized = value.toLowerCase()
  if (normalized === 'up' || normalized === 'unknown') return normalized === 'up' ? '在线' : '未知'
  if (normalized === 'down') return '离线'
  return value || '未知'
}
function resetForm() {
  selectedInterface.value = ''
  manualInterface.value = ''
  targetDeviceID.value = ''
  targetPort.value = ''
  manualPort.value = ''
}
async function load() {
  loading.value = true
  error.value = ''
  try {
    const [items, inventory] = await Promise.all([fetchNetworkInterfaces(auth.token, props.assetId), fetchNetworkDevices(auth.token)])
    interfaces.value = items
    devices.value = inventory
    if (items.length === 1) selectedInterface.value = items[0].name
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '网卡信息加载失败'
  } finally {
    loading.value = false
  }
}
async function bind() {
  if (!canBind.value) return
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    await createNetworkLink(auth.token, {
      sourceAssetId: props.assetId,
      sourceInterface: activeInterface.value.trim(),
      targetAssetId: targetDeviceID.value,
      targetPort: (targetPort.value || manualPort.value).trim(),
    })
    message.value = '物理链路绑定成功'
    resetForm()
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '绑定失败'
  } finally {
    saving.value = false
  }
}
async function unbind(id:string) {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    await deleteNetworkLink(auth.token, id)
    message.value = '物理链路已解除'
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '解绑失败'
  } finally {
    saving.value = false
  }
}
watch(() => props.assetId, load, { immediate: true })
</script>

<template>
  <section class="network-interface-panel">
    <header><div><Cable/><span><strong>网卡与物理链路</strong><small>网卡 → 交换机端口，支持手工绑定与 LLDP 自动发现</small></span></div><LoaderCircle v-if="loading" class="spin"/></header>
    <div v-if="error" class="interface-message error">{{ error }}</div>
    <div v-if="message" class="interface-message success"><Check/>{{ message }}</div>
    <div v-if="interfaces.length" class="interface-list">
      <article v-for="item in interfaces" :key="item.id">
        <div class="interface-name"><Cable/><span><strong>{{ item.name }}</strong><small>{{ item.type || 'ethernet' }} · MTU {{ item.mtu || '—' }}</small></span><b :data-status="item.operStatus">{{ interfaceStatus(item.operStatus) }}</b></div>
        <dl><div><dt>MAC</dt><dd>{{ item.macAddress || '—' }}</dd></div><div><dt>IPv4</dt><dd>{{ item.ipv4 || '—' }}</dd></div><div><dt>速率</dt><dd>{{ item.speedMbps ? `${item.speedMbps} Mbps` : '—' }}</dd></div></dl>
        <div v-if="item.link" class="link-binding"><Link2/><span><strong>{{ item.link.targetAssetName }}</strong><small>{{ item.link.targetPort }} · {{ item.link.source === 'lldp' ? 'LLDP 自动发现' : '人工绑定' }}</small></span><button :disabled="saving" @click="unbind(item.link.id)"><Unlink/>解绑</button></div>
        <div v-else class="empty-binding"><Network/>尚未绑定交换机端口</div>
      </article>
    </div>
    <div v-else-if="!loading" class="interface-empty">未采集到网卡清单。可执行 SSH 采集，或在下方手工填写网卡名称后绑定。</div>
    <div class="binding-form">
      <h4><Link2/>新建物理链路绑定</h4>
      <label><span>服务器网卡</span><select v-if="interfaces.length" v-model="selectedInterface"><option value="">请选择网卡</option><option v-for="item in interfaces" :key="item.id" :value="item.name">{{ item.name }} · {{ item.macAddress || 'MAC 未知' }}</option></select><input v-else v-model="manualInterface" placeholder="例如 eth0 / ens192"></label>
      <label><span>交换机 / 网络设备</span><select v-model="targetDeviceID" @change="targetPort='';manualPort=''"><option value="">请选择设备</option><option v-for="device in devices" :key="device.asset.id" :value="device.asset.id">{{ device.asset.name }} · {{ device.asset.ip }}</option></select></label>
      <label><span>交换机端口</span><select v-if="targetPorts.length" v-model="targetPort"><option value="">请选择端口</option><option v-for="port in targetPorts" :key="port.id" :value="port.name">{{ port.name }} · {{ port.operStatus }} · {{ port.alias || port.description || '无描述' }}</option></select><input v-else v-model="manualPort" placeholder="例如 GE0/0/1"></label>
      <button class="bind-action" :disabled="!canBind || saving" @click="bind"><LoaderCircle v-if="saving" class="spin"/><Link2 v-else/>保存绑定</button>
    </div>
  </section>
</template>

<style scoped>
.network-interface-panel{margin:12px 18px 16px;padding:13px;border:1px solid #1d4d6b;background:#061c30}.network-interface-panel>header{display:flex;justify-content:space-between;align-items:center}.network-interface-panel>header>div{display:flex;gap:8px;align-items:center}.network-interface-panel>header svg{width:18px;color:#31c4e7}.network-interface-panel>header span{display:grid;gap:3px}.network-interface-panel>header strong{font-size:11px;font-weight:500}.network-interface-panel>header small{font-size:8px;color:#63859a}.interface-list{display:grid;gap:8px;margin-top:11px}.interface-list article{padding:10px;border:1px solid #173f5b;background:#04182a}.interface-name{display:flex;align-items:center;gap:7px}.interface-name svg{width:14px;color:#38bce2}.interface-name span{display:grid;gap:2px;flex:1}.interface-name strong{font-size:10px;font-weight:500}.interface-name small{font-size:8px;color:#5d7e93}.interface-name b{padding:3px 6px;background:rgba(72,216,154,.1);color:#5bdfa1;font-size:8px;font-weight:500}.interface-name b[data-status=down]{background:rgba(255,84,105,.12);color:#ff8292}.interface-name b[data-status=unknown]{background:rgba(120,139,153,.14);color:#9dafba}.interface-list dl{display:grid;grid-template-columns:1fr 1fr 80px;gap:6px;margin:9px 0}.interface-list dl div{display:grid;gap:3px}.interface-list dt{font-size:8px;color:#5a7a8f}.interface-list dd{margin:0;font-size:9px;color:#b8d6e6;overflow:hidden;text-overflow:ellipsis}.link-binding,.empty-binding{display:flex;align-items:center;gap:7px;padding:7px;background:#08263c;font-size:9px}.empty-binding{color:#648397}.link-binding>svg,.empty-binding>svg{width:13px;color:#31c5e8}.link-binding span{display:grid;gap:2px;flex:1}.link-binding strong{font-weight:500}.link-binding small{color:#638397}.link-binding button{display:flex;align-items:center;gap:4px;border:1px solid #31536b;background:#0a2032;color:#9eb9c9;padding:4px 6px;font-size:8px;cursor:pointer}.link-binding button svg{width:11px}.binding-form{display:grid;gap:8px;margin-top:12px;padding-top:11px;border-top:1px solid #173d58}.binding-form h4{display:flex;align-items:center;gap:6px;margin:0;font-size:10px;font-weight:500;color:#b9d9e9}.binding-form h4 svg{width:13px;color:#38c7e8}.binding-form label{display:grid;gap:5px}.binding-form label>span{font-size:8px;color:#66879b}.binding-form select,.binding-form input{height:31px;border:1px solid #24536f;background:#031525;color:#c4dfed;padding:0 8px;outline:0}.bind-action{height:33px;border:1px solid #1a8fc5;background:#0b6da5;color:#fff;display:flex;align-items:center;justify-content:center;gap:6px;cursor:pointer}.bind-action:disabled{opacity:.45;cursor:not-allowed}.bind-action svg{width:14px}.interface-message{display:flex;align-items:center;gap:6px;margin-top:9px;padding:7px;font-size:9px}.interface-message svg{width:13px}.interface-message.error{background:rgba(255,82,104,.1);color:#ff8998}.interface-message.success{background:rgba(68,214,145,.1);color:#61dfa2}.interface-empty{margin-top:10px;padding:11px;border:1px dashed #28516b;color:#66879b;font-size:9px}.spin{animation:spin 1s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}
</style>
