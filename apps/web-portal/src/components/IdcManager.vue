<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Building2, Layers3, Plus, RefreshCw, Server, Trash2 } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'
import { addIdcModule, addIdcRack, createIdcRoom, deleteIdcModule, deleteIdcRack, deleteIdcRoom, fetchIdcRooms, updateIdcModule, updateIdcRack } from '@/api/cmdb'
import type { IdcPlacement, IdcRack, IdcRoom } from '@/features/cmdb/types'

const emit = defineEmits<{ changed: [] }>()
const auth = useAuthStore()
const rooms = ref<IdcRoom[]>([])
const error = ref('')
const loading = ref(false)
const roomName = ref('')
const roomLocation = ref('')
const moduleName = ref('')
const rackName = ref('')
const rackU = ref(42)
const rackVoltage = ref('220V')
const selectedRoomId = ref('')
const selectedModuleId = ref('')
const selectedRackId = ref('')
const selectedPlacement = ref<IdcPlacement>()

const selectedRoom = computed(() => rooms.value.find(room => room.id === selectedRoomId.value))
const selectedModule = computed(() => selectedRoom.value?.modules.find(module => module.id === selectedModuleId.value))
const selectedRack = computed(() => selectedModule.value?.racks.find(rack => rack.id === selectedRackId.value))
const rackUnits = computed(() => { const total = selectedRack.value?.uTotal || 0; return Array.from({ length: total }, (_, index) => total - index) })
const occupiedCount = computed(() => rooms.value.reduce((total, room) => total + room.modules.reduce((moduleTotal, module) => moduleTotal + module.racks.reduce((rackTotal, rack) => rackTotal + (rack.occupiedU?.length || 0), 0), 0), 0))
const rackCount = computed(() => rooms.value.reduce((total, room) => total + room.modules.reduce((count, module) => count + module.racks.length, 0), 0))
const typeColors:Record<string,string> = { 'physical-server':'#2f9cf4','virtual-machine':'#56b4ff','k8s-node':'#5f8cff','network-device':'#a879ff','load-balancer':'#36c5d8','storage-device':'#36c98f','security-device':'#ff8c5a','database':'#d6b64a','middleware':'#d66bd8' }
function colorFor(type:string) { return typeColors[type] || '#6d8ea8' }
function typeLabel(type:string) { return ({'physical-server':'物理服务器','virtual-machine':'虚拟机','k8s-node':'K8s 节点','network-device':'网络设备','load-balancer':'负载均衡','storage-device':'存储设备','security-device':'安全设备',database:'数据库',middleware:'中间件'} as Record<string,string>)[type] || type }
function placementTop(rack:IdcRack, item:IdcPlacement) { return (rack.uTotal - (item.uStart + Math.max(item.uHeight, 1) - 1)) * 24 }
function placementHeight(item:IdcPlacement) { return Math.max(item.uHeight, 1) * 24 - 2 }
async function load() {
  loading.value = true; error.value = ''
  try {
    rooms.value = await fetchIdcRooms(auth.token)
    if (!rooms.value.some(room => room.id === selectedRoomId.value)) selectedRoomId.value = rooms.value[0]?.id || ''
    const room = rooms.value.find(item => item.id === selectedRoomId.value)
    if (!room?.modules.some(module => module.id === selectedModuleId.value)) selectedModuleId.value = room?.modules[0]?.id || ''
    const module = room?.modules.find(item => item.id === selectedModuleId.value)
    if (!module?.racks.some(rack => rack.id === selectedRackId.value)) selectedRackId.value = module?.racks[0]?.id || ''
    selectedPlacement.value = undefined
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '机房数据加载失败' } finally { loading.value = false }
}
function selectRoom(id:string) { selectedRoomId.value=id; const room=rooms.value.find(item=>item.id===id); selectedModuleId.value=room?.modules[0]?.id||''; selectedRackId.value=room?.modules[0]?.racks[0]?.id||''; selectedPlacement.value=undefined }
function selectModule(id:string) { selectedModuleId.value=id; const module=selectedRoom.value?.modules.find(item=>item.id===id); selectedRackId.value=module?.racks[0]?.id||''; selectedPlacement.value=undefined }
function selectRack(id:string) { selectedRackId.value=id; selectedPlacement.value=undefined }
async function addRoom() { if(!roomName.value.trim()){error.value='请输入机房名称';return} try{await createIdcRoom(auth.token,roomName.value.trim(),roomLocation.value.trim());roomName.value='';roomLocation.value='';await load();emit('changed')}catch(reason){error.value=reason instanceof Error?reason.message:'创建机房失败'} }
async function addModule() { if(!selectedRoom.value||!moduleName.value.trim()){error.value='请选择机房并输入模块名称';return} try{await addIdcModule(auth.token,selectedRoom.value.id,moduleName.value.trim());moduleName.value='';await load();emit('changed')}catch(reason){error.value=reason instanceof Error?reason.message:'创建模块失败'} }
async function addRack() { if(!selectedModule.value||!rackName.value.trim()){error.value='请选择模块并输入机柜名称';return} try{await addIdcRack(auth.token,selectedModule.value.id,{name:rackName.value.trim(),uTotal:rackU.value||42,voltage:(rackVoltage.value||'220V').trim()});rackName.value='';rackU.value=42;rackVoltage.value='220V';await load();emit('changed')}catch(reason){error.value=reason instanceof Error?reason.message:'创建机柜失败'} }
async function renameModule(id:string,oldName:string){const value=prompt('模块名称',oldName);if(!value?.trim())return;try{await updateIdcModule(auth.token,id,value.trim());await load();emit('changed')}catch(reason){error.value=reason instanceof Error?reason.message:'修改模块失败'}}
async function renameRack(rack:IdcRack){const value=prompt('机柜名称',rack.name);if(!value?.trim())return;try{await updateIdcRack(auth.token,rack.id,{name:value.trim(),uTotal:rack.uTotal,voltage:rack.voltage});await load();emit('changed')}catch(reason){error.value=reason instanceof Error?reason.message:'修改机柜失败'}}
async function removeRack(id:string){try{await deleteIdcRack(auth.token,id);await load();emit('changed')}catch(reason){error.value=reason instanceof Error?reason.message:'删除机柜失败'}}
async function removeModule(id:string){try{await deleteIdcModule(auth.token,id);await load();emit('changed')}catch(reason){error.value=reason instanceof Error?reason.message:'删除模块失败，请先删除模块下机柜'}}
async function removeRoom(id:string){try{await deleteIdcRoom(auth.token,id);await load();emit('changed')}catch(reason){error.value=reason instanceof Error?reason.message:'删除机房失败'}}
onMounted(load)
</script>

<template>
  <section class="idc-page">
    <header class="idc-heading"><div><span>DATA CENTER &amp; RACK MODEL</span><h1>机房与机柜管理</h1><p>按机房、模块、机柜和 U 位维护设备空间，并与 CMDB 资产自动关联。</p></div><button @click="load"><RefreshCw :class="{spin:loading}"/>刷新</button></header>
    <div class="idc-stats"><article><Building2/><span>机房<strong>{{rooms.length}}</strong></span></article><article><Layers3/><span>模块<strong>{{rooms.reduce((n,r)=>n+r.modules.length,0)}}</strong></span></article><article><Server/><span>机柜<strong>{{rackCount}}</strong></span></article><article><Server/><span>已占 U 位<strong>{{occupiedCount}}</strong></span></article></div>
    <p v-if="error" class="idc-error">{{error}}</p>
    <div class="idc-layout">
      <aside class="idc-sidebar">
        <section class="idc-create"><h3>新建机房</h3><input v-model="roomName" placeholder="机房名称"><input v-model="roomLocation" placeholder="机房位置"><button class="primary" @click="addRoom"><Plus/>创建</button></section>
        <section class="idc-tree"><header><strong>机房目录</strong><small>{{rooms.length}} 个机房</small></header><button v-for="room in rooms" :key="room.id" :class="{active:selectedRoomId===room.id}" @click="selectRoom(room.id)"><Building2/><span><strong>{{room.name}}</strong><small>{{room.location||'未设置位置'}} · {{room.modules.length}} 模块</small></span></button><div v-if="!rooms.length" class="idc-empty">暂无机房，请先创建。</div></section>
      </aside>
      <main class="idc-module-area">
        <header><div><h2>{{selectedRoom?.name||'请选择机房'}}</h2><p>{{selectedRoom?.location||'选择左侧机房查看模块和机柜'}}</p></div><div class="idc-inline"><input v-model="moduleName" placeholder="新模块名称"><button @click="addModule"><Plus/>加模块</button></div></header>
        <div v-if="selectedRoom" class="module-grid"><button v-for="module in selectedRoom.modules" :key="module.id" :class="{active:selectedModuleId===module.id}" @click="selectModule(module.id)"><Layers3/><span><strong>{{module.name}}</strong><small>{{module.racks.length}} 个机柜</small></span><i @click.stop="renameModule(module.id,module.name)">改名</i><i class="danger" @click.stop="removeModule(module.id)"><Trash2/></i></button></div>
        <div v-if="selectedModule" class="rack-toolbar"><strong>{{selectedModule.name}} · 机柜</strong><div class="idc-inline"><input v-model="rackName" placeholder="机柜号，如 A03"><input v-model.number="rackU" type="number" min="1" max="60" placeholder="U位数"><input v-model="rackVoltage" placeholder="电压"><button @click="addRack"><Plus/>加机柜</button></div></div>
        <div v-if="selectedModule" class="rack-tabs"><button v-for="rack in selectedModule.racks" :key="rack.id" :class="{active:selectedRackId===rack.id}" @click="selectRack(rack.id)"><strong>{{rack.name}}</strong><small>{{rack.uTotal}}U · {{rack.voltage}} · 已占 {{rack.occupiedU?.length||0}}U</small></button><span v-if="!selectedModule.racks.length">该模块还没有机柜</span></div>
        <section v-if="selectedRack" class="rack-stage">
          <header><div><h3>{{selectedRack.name}} 2D 机柜立面</h3><p>{{selectedRack.voltage}} · 总 {{selectedRack.uTotal}}U · 占用 {{selectedRack.occupiedU?.length||0}}U</p></div><button @click="renameRack(selectedRack)">编辑机柜</button><button class="danger" @click="removeRack(selectedRack.id)"><Trash2/>删除</button></header>
          <div class="rack-legend"><span v-for="item in [['physical-server','服务器'],['network-device','网络'],['security-device','安全'],['storage-device','存储'],['load-balancer','负载均衡']]" :key="item[0]"><i :style="{background:colorFor(item[0])}"></i>{{item[1]}}</span></div>
          <div class="rack-2d"><div class="u-scale"><span v-for="u in rackUnits" :key="u">{{u}}U</span></div><div class="rack-frame" :style="{height:(selectedRack.uTotal*24)+'px'}"><div v-for="u in rackUnits" :key="'slot'+u" class="rack-slot" :style="{top:((selectedRack.uTotal-u)*24)+'px'}"><span>{{u}}</span></div><button v-for="item in selectedRack.placements||[]" :key="item.assetId" class="rack-device" :class="{selected:selectedPlacement?.assetId===item.assetId}" :style="{top:placementTop(selectedRack,item)+'px',height:placementHeight(item)+'px','--device-color':colorFor(item.assetType)}" @click="selectedPlacement=item"><strong>{{item.assetName}}</strong><small>{{typeLabel(item.assetType)}} · {{item.uStart}}-{{item.uStart+Math.max(item.uHeight,1)-1}}U</small><em>{{item.uHeight}}U</em></button></div></div>
          <div class="rack-detail"><template v-if="selectedPlacement"><strong>{{selectedPlacement.assetName}}</strong><span>{{typeLabel(selectedPlacement.assetType)}}</span><span>U{{selectedPlacement.uStart}} - U{{selectedPlacement.uStart+Math.max(selectedPlacement.uHeight,1)-1}}</span><span>状态 {{selectedPlacement.status||'未知'}}</span></template><span v-else>点击机柜中的设备可查看资产占用详情。设备高度在资产编辑页维护，支持 1U、2U、4U 等连续占位。</span></div>
        </section>
        <div v-else class="idc-empty large">选择机房和模块后，可查看对应机柜的 2D U 位占用视图。</div>
      </main>
    </div>
  </section>
</template>
<style scoped>
.idc-page{padding:20px;color:#d8edf8}.idc-heading{display:flex;justify-content:space-between;align-items:flex-end;gap:16px}.idc-heading span{color:#22d3ee;font:10px/1.2 monospace;letter-spacing:.16em}.idc-heading h1{margin:6px 0;font-size:24px}.idc-heading p{margin:0;color:#7190a5;font-size:11px}.idc-heading>button{display:flex;align-items:center;gap:7px;height:34px;padding:0 12px;border:1px solid #245573;background:#082943;color:#b9d8e8;cursor:pointer}.idc-stats{display:grid;grid-template-columns:repeat(4,1fr);gap:10px;margin:16px 0}.idc-stats article{display:flex;align-items:center;gap:10px;padding:13px;border:1px solid #174d78;background:#071d30}.idc-stats svg{color:#2fc7e8}.idc-stats span{display:grid;gap:4px;color:#6f8ea3;font-size:10px}.idc-stats strong{color:#e0f5ff;font-size:20px}.idc-error{padding:9px 12px;border:1px solid #8d3a47;background:#2c1118;color:#ff9ba8}.idc-layout{display:grid;grid-template-columns:280px minmax(0,1fr);gap:14px;align-items:start}.idc-sidebar,.idc-module-area{border:1px solid #174d78;background:#061a2c}.idc-create{display:grid;gap:8px;padding:14px;border-bottom:1px solid #17384f}.idc-create h3,.idc-tree header strong{margin:0}.idc-create input,.idc-inline input{height:34px;border:1px solid #245573;background:#031525;color:#d9edf7;padding:0 9px}.idc-create button,.idc-inline button{height:34px;border:1px solid #246b98;background:#0b456b;color:#d8f4ff;display:flex;align-items:center;gap:6px;justify-content:center;cursor:pointer}.idc-tree{padding:10px}.idc-tree header{display:flex;justify-content:space-between;padding:4px 3px 10px;color:#68889d;font-size:11px}.idc-tree>button{width:100%;display:grid;grid-template-columns:24px 1fr;gap:8px;align-items:center;padding:10px;margin-bottom:5px;border:1px solid transparent;background:transparent;color:#86a7ba;text-align:left;cursor:pointer}.idc-tree>button.active{border-color:#14627f;background:#072b3e;color:#d9f7ff}.idc-tree>button span{display:grid;gap:3px}.idc-tree small{color:#66859a;font-size:9px}.idc-module-area>header{display:flex;justify-content:space-between;align-items:center;gap:12px;padding:14px;border-bottom:1px solid #17384f}.idc-module-area h2{margin:0;font-size:18px}.idc-module-area p{margin:4px 0 0;color:#6c8ca2;font-size:10px}.idc-inline{display:flex;gap:7px;align-items:center;flex-wrap:wrap}.module-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:8px;padding:12px}.module-grid>button{display:grid;grid-template-columns:22px 1fr auto auto;gap:7px;align-items:center;padding:10px;border:1px solid #17435f;background:#081f31;color:#a8c5d5;text-align:left;cursor:pointer}.module-grid>button.active{border-color:#1a9bc2;background:#083149}.module-grid span{display:grid;gap:3px}.module-grid small{color:#658499;font-size:9px}.module-grid i{color:#5fb7d6;font-size:10px;font-style:normal}.module-grid i.danger{color:#ff7f8f}.rack-toolbar{display:flex;justify-content:space-between;align-items:center;gap:12px;padding:12px 14px;border-top:1px solid #17384f}.rack-tabs{display:flex;gap:7px;padding:0 14px 12px;overflow-x:auto}.rack-tabs button{min-width:150px;padding:8px 10px;border:1px solid #17435f;background:#061620;color:#8ba8ba;text-align:left;cursor:pointer}.rack-tabs button.active{border-color:#21b9df;background:#083047;color:#dff9ff}.rack-tabs strong,.rack-tabs small{display:block}.rack-tabs small{margin-top:3px;color:#68879a;font-size:9px}.rack-stage{margin:0 14px 14px;border:1px solid #17435f;background:#031521}.rack-stage>header{display:flex;align-items:center;gap:8px;padding:12px 14px;border-bottom:1px solid #17384f}.rack-stage>header>div{flex:1}.rack-stage h3{margin:0;font-size:14px}.rack-stage p{margin:3px 0 0;color:#68879a;font-size:10px}.rack-stage button{padding:7px 9px;border:1px solid #245573;background:#082943;color:#b9d8e8;cursor:pointer}.rack-stage button.danger{color:#ff9aa7}
.rack-legend{display:flex;gap:14px;flex-wrap:wrap;padding:10px 14px;color:#819bac;font-size:10px}.rack-legend span{display:flex;align-items:center;gap:5px}.rack-legend i{width:8px;height:8px;border-radius:2px}.rack-2d{display:grid;grid-template-columns:44px minmax(260px,560px);gap:8px;justify-content:center;padding:12px 24px 20px}.u-scale{display:grid;color:#65839a;font-size:9px;text-align:right;padding-right:5px}.u-scale span{height:24px;line-height:24px}.rack-frame{position:relative;border:2px solid #28536c;background:repeating-linear-gradient(to bottom,#071b2a 0,#071b2a 22px,#0c2a3c 22px,#0c2a3c 24px);box-shadow:inset 0 0 28px rgba(37,160,210,.08)}.rack-slot{position:absolute;left:0;right:0;height:24px;border-bottom:1px solid rgba(48,107,137,.28)}.rack-device{position:absolute;left:8px;right:8px;display:grid;grid-template-columns:1fr auto;gap:2px 8px;align-items:center;padding:5px 9px;border:1px solid color-mix(in srgb,var(--device-color),white 20%);border-left:4px solid var(--device-color);background:linear-gradient(90deg,color-mix(in srgb,var(--device-color),transparent 78%),#061a28);color:#effbff;text-align:left;cursor:pointer;overflow:hidden}.rack-device.selected{outline:2px solid #fff3;box-shadow:0 0 16px color-mix(in srgb,var(--device-color),transparent 35%)}.rack-device strong{font-size:10px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.rack-device small{grid-column:1;color:#94afc0;font-size:8px}.rack-device em{grid-row:1/3;grid-column:2;color:var(--device-color);font-size:12px;font-style:normal;font-weight:700}.rack-detail{display:flex;gap:14px;flex-wrap:wrap;padding:11px 14px;border-top:1px solid #17384f;color:#809daf;font-size:10px}.rack-detail strong{color:#dff8ff}.idc-empty{padding:28px;text-align:center;color:#607f94;font-size:11px}.idc-empty.large{margin:14px;border:1px dashed #245573}.spin{animation:spin .9s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}@media(max-width:1100px){.idc-layout{grid-template-columns:1fr}.idc-sidebar{max-height:340px;overflow:auto}.idc-stats{grid-template-columns:repeat(2,1fr)}}@media(max-width:760px){.idc-page{padding:12px}.idc-heading,.rack-toolbar,.idc-module-area>header{align-items:flex-start;flex-direction:column}.rack-2d{grid-template-columns:38px minmax(220px,1fr);padding:10px}.module-grid{grid-template-columns:1fr}}
</style>
