<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Plus, Server, Trash2, X } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'
import { addIdcModule, addIdcRack, createIdcRoom, deleteIdcModule, deleteIdcRack, deleteIdcRoom, fetchIdcRooms, updateIdcModule, updateIdcRack } from '@/api/cmdb'
import type { IdcRoom } from '@/features/cmdb/types'
const emit = defineEmits<{ close: []; changed: [] }>()
const auth = useAuthStore()
const rooms = ref<IdcRoom[]>([])
const error = ref('')
const roomName = ref('')
const roomLocation = ref('')
const moduleName = ref<Record<string, string>>({})
const rackName = ref<Record<string, string>>({})
const rackU = ref<Record<string, number>>({})
const rackVoltage = ref<Record<string, string>>({})
async function load() {
  error.value = ''
  try { rooms.value = await fetchIdcRooms(auth.token) } catch (e) { error.value = e instanceof Error ? e.message : '加载失败' }
}
async function addRoom() {
  try { await createIdcRoom(auth.token, roomName.value.trim(), roomLocation.value.trim()); roomName.value = ''; roomLocation.value = ''; await load(); emit('changed') }
  catch (e) { error.value = e instanceof Error ? e.message : '创建失败' }
}
async function addModule(roomId: string) {
  try { await addIdcModule(auth.token, roomId, (moduleName.value[roomId] || '').trim()); moduleName.value[roomId] = ''; await load(); emit('changed') }
  catch (e) { error.value = e instanceof Error ? e.message : '创建失败' }
}
async function addRack(moduleId: string) {
  try { await addIdcRack(auth.token, moduleId, { name: (rackName.value[moduleId] || '').trim(), uTotal: rackU.value[moduleId] || 42, voltage: (rackVoltage.value[moduleId] || '220V').trim() }); rackName.value[moduleId] = ''; rackU.value[moduleId] = 42; rackVoltage.value[moduleId] = '220V'; await load(); emit('changed') }
  catch (e) { error.value = e instanceof Error ? e.message : '创建失败' }
}
async function renameRack(r:{id:string;name:string;uTotal:number;voltage:string}){const name=prompt('机柜名称',r.name);if(name===null||!name.trim())return;try{await updateIdcRack(auth.token,r.id,{name:name.trim(),uTotal:r.uTotal,voltage:r.voltage});await load();emit('changed')}catch(e){error.value=e instanceof Error?e.message:'改名失败'}}
async function renameModule(id:string,oldName:string){const name=prompt('模块名称',oldName);if(name===null||!name.trim())return;try{await updateIdcModule(auth.token,id,name.trim());await load();emit('changed')}catch(e){error.value=e instanceof Error?e.message:'改名失败'}}
async function removeRack(id:string){try{await deleteIdcRack(auth.token,id);await load();emit('changed')}catch(e){error.value=e instanceof Error?e.message:'删除失败'}}
async function removeModule(id:string){try{await deleteIdcModule(auth.token,id);await load();emit('changed')}catch(e){error.value=e instanceof Error?e.message:'删除失败(模块下有机柜时请先删除机柜)'}}
async function removeRoom(id:string){try{await deleteIdcRoom(auth.token,id);await load();emit('changed')}catch(e){error.value=e instanceof Error?e.message:'删除失败'}}
onMounted(load)
</script>
<template>
  <div class="modal-backdrop">
    <section class="idc-manager">
      <header>
        <div><span>DATA CENTER MODEL</span><h2>机房建模</h2></div>
        <button @click="emit('close')"><X /></button>
      </header>
      <div class="idc-add-room">
        <input v-model="roomName" placeholder="机房名称（如 上海一号机房）">
        <input v-model="roomLocation" placeholder="位置（如 上海市…）">
        <button class="primary" @click="addRoom"><Plus /> 新建机房</button>
      </div>
      <p v-if="error" class="cmdb-error">{{ error }}</p>
      <div class="idc-list">
        <article v-for="room in rooms" :key="room.id" class="idc-room">
          <header><strong>{{ room.name }}</strong><span>{{ room.location }}</span><button class="link-danger" title="删除机房" @click="removeRoom(room.id)"><Trash2 /></button></header>
          <div class="idc-modules">
            <div v-for="m in room.modules" :key="m.id" class="idc-module">
              <h4><Server /> 模块：{{ m.name }}<button class="link-plain" @click="renameModule(m.id,m.name)">改名</button><button class="link-danger" title="删除模块" @click="removeModule(m.id)"><Trash2 /></button></h4>
              <table v-if="m.racks.length"><thead><tr><th>机柜</th><th>U位</th><th>电压</th><th>已占用</th><th></th></tr></thead><tbody><tr v-for="rk in m.racks" :key="rk.id"><td>{{ rk.name }}</td><td>{{ rk.uTotal }}U</td><td>{{ rk.voltage }}</td><td><span v-if="(rk.occupiedU||[]).length">{{ (rk.occupiedU||[]).join(', ') }}</span><span v-else class="muted">空</span></td><td><button class="link-plain" @click="renameRack(rk)">改名</button><button class="link-danger" title="删除机柜" @click="removeRack(rk.id)"><Trash2 /></button></td></tr></tbody></table>
              <p v-else class="muted">暂无机柜</p>
              <div class="idc-inline">
                <input v-model="rackName[m.id]" placeholder="机柜名（如 A03）">
                <input v-model.number="rackU[m.id]" type="number" placeholder="U位">
                <input v-model="rackVoltage[m.id]" placeholder="电压(如220V)">
                <button @click="addRack(m.id)">加机柜</button>
              </div>
            </div>
            <div class="idc-inline">
              <input v-model="moduleName[room.id]" placeholder="模块名称（如 模块A）">
              <button @click="addModule(room.id)">加模块</button>
            </div>
          </div>
        </article>
        <p v-if="!rooms.length" class="muted">还没有机房，先新建一个。</p>
      </div>
    </section>
  </div>
</template>
<style scoped>
.idc-manager { width: min(980px, 94vw); max-height: 86vh; overflow: auto; }
.idc-manager header { display: flex; align-items: center; justify-content: space-between; }
.idc-add-room, .idc-inline { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.idc-add-room input, .idc-inline input { flex: 1 1 120px; }
.idc-room { border: 1px solid var(--line,#24314d); border-radius: 10px; padding: 12px; margin-top: 12px; background: var(--panel,#0d1526); }
.idc-room > header { display: flex; gap: 12px; }
.idc-room > header strong { font-size: 15px; }
.idc-room > header span { opacity: .6; }
.idc-module { margin: 10px 0 0 12px; }
.idc-module h4 { margin: 0 0 6px; display: flex; gap: 6px; align-items: center; font-size: 13px; }
table { border-collapse: collapse; width: 100%; font-size: 12px; }
th, td { border: 1px solid var(--line,#24314d); padding: 4px 8px; text-align: left; }
.muted { opacity: .5; font-size: 12px; }
</style>
<style scoped>
.link-danger { background:none; border:none; color:#f56c6c; cursor:pointer; margin-left:auto; }
.link-plain { background:none; border:none; color:#4d8dff; cursor:pointer; margin-left:6px; font-size:12px; }
h4 .link-danger { margin-left:8px; }
</style>