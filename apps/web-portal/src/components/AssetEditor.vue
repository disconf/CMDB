<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { Plus, Trash2, X } from 'lucide-vue-next'
import type { Asset, AssetInput, Attribute, CiModel, IdcRoom, Relation } from '@/features/cmdb/types'
import { validateAssetForm } from '@/features/cmdb/assetForm'
import { fetchAssetOptions } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'
import ResponsibilityEditor from './ResponsibilityEditor.vue'

const props = defineProps<{ asset?: Asset; models: CiModel[]; idcRooms?: IdcRoom[]; saving?: boolean; serverError?: string }>()
const emit = defineEmits<{ close: []; save: [value: AssetInput] }>()
const errors = ref<string[]>([])
const errorBox = ref<HTMLElement>()
const isEdit = computed(() => Boolean(props.asset))

const form = reactive<{ id: string; name: string; type: string; status: string; ip: string; environment: string; projectGroup: string; owner: string; location: string; source: string; tags: string[] }>({
  id: props.asset?.id ?? '',
  name: props.asset?.name ?? '',
  type: props.asset?.type ?? 'physical-server',
  status: props.asset?.status ?? 'online',
  ip: props.asset?.ip ?? '',
  environment: props.asset?.environment ?? '生产',
  projectGroup: props.asset?.projectGroup ?? '',
  owner: props.asset?.owner ?? '',
  location: props.asset?.location ?? '',
  source: props.asset?.source ?? 'manual',
  tags: [...(props.asset?.tags ?? [])],
})
const tagsText = ref(form.tags.join('|'))
const attrs = ref<Attribute[]>((props.asset?.attributes ?? []).map((a) => ({ name: a.name, label: a.label, value: a.value })))
const relations = ref<Relation[]>((props.asset?.relations ?? []).map((r) => ({ type: r.type, targetId: r.targetId, targetName: r.targetName })))
const auth = useAuthStore()
const hostType = ref<'physical'|'cluster'>('physical')
const hostTargetId = ref('')
const hostOptions = ref<Asset[]>([])
const typeChanged = computed(() => Boolean(props.asset && form.type !== props.asset.type))
const serverTypeSuggestion = computed(() => {
  const raw = attrValue('virtualization_type')
  if (!raw || raw === 'none') return '未检测到虚拟化，建议物理机'
  return `检测到 ${raw}，建议虚拟机`
})
const compatibleHosts = computed(() => hostOptions.value.filter(item => hostType.value === 'cluster' ? item.type === 'virtualization-cluster' : item.type === 'physical-server'))
function fieldMeta(name: string) { return modelFields().find((f) => f.name === name) }
function isSelectField(name: string) { const f = fieldMeta(name); return !!f && f.type === 'select' && !!f.options?.length }
function selectOptions(name: string) { return fieldMeta(name)?.options ?? [] }
function addRelation() { relations.value.push({ type: 'belongs-to', targetId: '', targetName: '' }) }
function removeRelation(index: number) { relations.value.splice(index, 1) }
async function loadHostOptions() {
  try { hostOptions.value = await fetchAssetOptions(auth.token, ['physical-server', 'virtualization-cluster']) } catch { hostOptions.value = [] }
  hydrateHostSelection()
}
function hydrateHostSelection() {
  const relation = relations.value.find(item => item.type === 'runs-on')
  if (!relation) return
  hostTargetId.value = relation.targetId
  const target = hostOptions.value.find(item => item.id === relation.targetId)
  if (target) hostType.value = target.type === 'k8s-cluster' ? 'cluster' : 'physical'
}
function syncHostRelation() {
  relations.value = relations.value.filter(item => item.type !== 'runs-on')
  if (form.type !== 'virtual-machine' || !hostTargetId.value) return
  const target = hostOptions.value.find(item => item.id === hostTargetId.value)
  relations.value.push({ type: 'runs-on', targetId: hostTargetId.value, targetName: target?.name || hostTargetId.value })
}
function clearPhysicalLocation() {
  clearAttr('idc_name'); clearAttr('module_name'); clearAttr('rack_no'); clearAttr('rack_id'); clearAttr('u_position'); clearAttr('voltage')
  idcRoomId.value = ''; idcModuleId.value = ''; idcRackId.value = ''; idcU.value = ''
}

function modelFields() {
  const model = props.models.find((m) => m.code === form.type)
  return model ? model.fields : []
}
function prefillFromModel() {
  const existing = new Set(attrs.value.map((a) => a.name))
  const added = modelFields().filter((f) => !existing.has(f.name)).map((f) => ({ name: f.name, label: f.label, value: f.defaultValue || '' }))
  if (added.length) attrs.value = [...attrs.value, ...added]
}
prefillFromModel()
onMounted(loadHostOptions)
watch(() => form.type, (value) => {
  prefillFromModel()
  if (value === 'virtual-machine') clearPhysicalLocation()
  if (value === 'physical-server') {
    relations.value = relations.value.filter(item => item.type !== 'runs-on')
    hostTargetId.value = ''
  }
})

const idcRoomId = ref('')
const idcModuleId = ref('')
const idcRackId = ref('')
const idcU = ref('')
const idcModules = computed(() => props.idcRooms?.find((x) => x.id === idcRoomId.value)?.modules ?? [])
const selectedModule = computed(() => idcModules.value.find((m) => m.id === idcModuleId.value))
const idcRacks = computed(() => selectedModule.value?.racks ?? [])
const selectedRack = computed(() => idcRacks.value.find((r) => r.id === idcRackId.value))
const ownU = computed(() => attrValue('u_position'))
const uOptions = computed(() => { const rack = selectedRack.value; const occupied = new Set(rack?.occupiedU ?? []); return Array.from({ length: rack?.uTotal ?? 0 }, (_, i) => String(i + 1)).filter((u) => !occupied.has(u) || u === ownU.value) })
function attrValue(name: string) { return attrs.value.find((a) => a.name === name)?.value || '' }
function setAttr(name: string, label: string, value: string) {
  const found = attrs.value.find((a) => a.name === name)
  if (found) { found.value = value; if (!found.label) found.label = label }
  else if (value) attrs.value.push({ name, label, value })
}
function clearAttr(name: string) {
  const index = attrs.value.findIndex((a) => a.name === name)
  if (index >= 0) attrs.value.splice(index, 1)
}
function syncIdcLocation() {
  const room = props.idcRooms?.find((x) => x.id === idcRoomId.value)
  const module = idcModules.value.find((x) => x.id === idcModuleId.value)
  const rack = idcRacks.value.find((x) => x.id === idcRackId.value)
  if (!room && !module && !rack) return
  form.location = [room?.name, module?.name, rack ? `${rack.name}${idcU.value ? `-${idcU.value}U` : ''}` : ''].filter(Boolean).join(' / ')
}
let hydratingIdc = false
function hydrateIdcSelection() {
  if (hydratingIdc || !props.idcRooms?.length) return
  hydratingIdc = true
  const roomName = attrValue('idc_name')
  const moduleName = attrValue('module_name')
  const rackID = attrValue('rack_id')
  const uPosition = attrValue('u_position')
  const room = props.idcRooms.find((x) => x.name === roomName)
  if (room) {
    idcRoomId.value = room.id
    const module = room.modules.find((x) => x.name === moduleName || x.racks.some((r) => r.id === rackID))
    if (module) {
      idcModuleId.value = module.id
      const rack = module.racks.find((x) => x.id === rackID)
      if (rack) { idcRackId.value = rack.id; idcU.value = uPosition }
    }
  }
  void nextTick(() => { hydratingIdc = false })
}
watch(() => props.idcRooms, hydrateIdcSelection, { immediate: true })
watch(idcRoomId, (v, old) => {
  if (hydratingIdc) return
  if (old && v !== old) { idcModuleId.value = ''; idcRackId.value = ''; idcU.value = '' }
  const room = props.idcRooms?.find((x) => x.id === v)
  if (room) { setAttr('idc_name', '机房名称', room.name) } else { clearAttr('idc_name') }
  syncIdcLocation()
})
watch(idcModuleId, (v, old) => {
  if (hydratingIdc) return
  if (old && v !== old) { idcRackId.value = ''; idcU.value = '' }
  const module = idcModules.value.find((x) => x.id === v)
  if (module) { setAttr('module_name', '模块/区域', module.name) } else { clearAttr('module_name') }
  syncIdcLocation()
})
watch(idcRackId, (v, old) => {
  if (hydratingIdc) return
  if (old && v !== old) idcU.value = ''
  const rack = selectedRack.value
  if (rack) { setAttr('rack_id', '机柜ID', rack.id); setAttr('rack_no', '机柜号', rack.name); if (rack.voltage) setAttr('voltage', '电压', rack.voltage) }
  else { clearAttr('rack_id'); clearAttr('rack_no'); clearAttr('voltage') }
  syncIdcLocation()
})
watch(idcU, (v) => { if (hydratingIdc) return; if (v) setAttr('u_position', 'U位', v); else clearAttr('u_position'); syncIdcLocation() })
function addAttr() { attrs.value.push({ name: '', label: '', value: '' }) }
function removeAttr(index: number) { attrs.value.splice(index, 1) }

function submit() {
  form.tags = tagsText.value.split('|').map((v) => v.trim()).filter(Boolean)
  const attributes = attrs.value.map((a) => ({ name: a.name.trim(), label: a.label.trim(), value: a.value.trim() })).filter((a) => a.name || a.label || a.value)
  errors.value = validateAssetForm(form)
  if (form.type === 'virtual-machine' && !hostTargetId.value) errors.value.push('虚拟机必须选择宿主物理机或虚拟化集群')
  if (attributes.some((a) => !a.name)) errors.value.push('扩展属性填写了显示名或值时必须填写字段名')
  if (errors.value.length) { void nextTick(() => errorBox.value?.scrollIntoView({ behavior: 'smooth', block: 'center' })); return }
  syncHostRelation()
  const cleanRelations = relations.value.map((r) => ({ type: r.type.trim(), targetId: r.targetId.trim(), targetName: r.targetName.trim() })).filter((r) => r.targetId || r.targetName)
  emit('save', { ...form, tags: [...form.tags], attributes: attributes.length ? attributes : undefined, relations: cleanRelations.length ? cleanRelations : undefined })
}
</script>
<template>
  <div class="modal-backdrop">
    <form class="asset-editor" @submit.prevent="submit">
      <header>
        <div><span>{{ isEdit ? '编辑资产' : '新增资产' }}</span><h2>{{ form.name || '登记新的配置项' }}</h2></div>
        <button type="button" aria-label="关闭" @click="emit('close')"><X /></button>
      </header>
      <div class="editor-grid">
        <label><span>资产编号 *</span><input v-model="form.id" :disabled="isEdit"></label>
        <label><span>资产名称 *</span><input v-model="form.name"></label>
        <label><span>资产类型 *</span><select v-model="form.type">
          <option v-for="model in models" :key="model.code" :value="model.code">{{ model.name }}</option>
        </select><small class="field-help">{{ serverTypeSuggestion }}</small></label>
        <label><span>状态</span><select v-model="form.status"><option value="online">在线</option><option value="warning">异常</option><option value="offline">离线</option></select></label>
        <label><span>IP 地址</span><input v-model="form.ip"></label>
        <label><span>环境</span><select v-model="form.environment"><option>生产</option><option>测试</option><option>开发</option></select></label>
        <label v-if="form.type === 'physical-server'" class="wide"><span>位置</span><input v-model="form.location" placeholder="选择机房后自动写入"></label>
        <label v-else-if="form.type === 'virtual-machine'" class="wide"><span>继承位置</span><input :value="form.location || '由宿主资产继承'" readonly></label>
        <label class="wide"><span>标签（使用 | 分隔）</span><input v-model="tagsText"></label>
      </div>
      <ResponsibilityEditor :owner="form.owner" :project-group="form.projectGroup" @update:owner="form.owner = $event" @update:project-group="form.projectGroup = $event" />
      <div v-if="typeChanged" class="type-change-note">资产类型将由“{{ props.asset?.typeName }}”调整为“{{ models.find(m => m.code === form.type)?.name || form.type }}”，保存后会切换可用关系与位置字段。</div>
      <section v-if="form.type === 'virtual-machine'" class="attr-editor host-picker">
        <header><h3>宿主关系</h3></header>
        <div class="idc-grid host-grid">
          <label><span>宿主类型</span><select v-model="hostType"><option value="physical">物理机</option><option value="cluster">虚拟化集群</option></select></label>
          <label class="wide"><span>{{ hostType === 'physical' ? '宿主物理机' : '虚拟化集群' }}</span><select v-model="hostTargetId"><option value="">请选择宿主</option><option v-for="host in compatibleHosts" :key="host.id" :value="host.id">{{ host.name }} · {{ host.ip || host.location || host.typeName }}</option></select></label>
          <label class="wide"><span>继承位置</span><input :value="form.location || '由宿主资产继承'" readonly></label>
        </div>
        <p class="attr-hint">虚拟机不直接选择机房、机柜、U位或交换机端口，这些信息从宿主资产继承。</p>
      </section>
      <section v-if="form.type === 'physical-server' && props.idcRooms?.length" class="attr-editor idc-picker">
        <header><h3>机房定位（可选）</h3></header>
        <div class="idc-grid">
          <label><span>机房</span><select v-model="idcRoomId"><option value="">请选择</option><option v-for="room in props.idcRooms ?? []" :key="room.id" :value="room.id">{{ room.name }}（{{ room.location }}）</option></select></label>
          <label><span>模块</span><select v-model="idcModuleId" :disabled="!idcModules.length"><option value="">请选择</option><option v-for="m in idcModules" :key="m.id" :value="m.id">{{ m.name }}</option></select></label>
          <label><span>机柜</span><select v-model="idcRackId" :disabled="!idcRacks.length"><option value="">请选择</option><option v-for="r in idcRacks" :key="r.id" :value="r.id">{{ r.name }}（{{ r.uTotal }}U / {{ r.voltage }}）</option></select></label>
          <label><span>U位</span><select v-model="idcU" :disabled="!uOptions.length"><option value="">请选择</option><option v-for="u in uOptions" :key="u" :value="u">{{ u }}U</option></select></label>
        </div>
      </section>
      <section class="attr-editor">
        <header>
          <h3>扩展属性 / 自定义字段</h3>
          <button type="button" @click="addAttr"><Plus /> 添加字段</button>
        </header>
        <p v-if="!isEdit" class="attr-hint">选择 CI 模型后自动带出字段（如带外管理IP、机房、机柜、U 位等），也可手动添加。</p>
        <div v-for="(item, index) in attrs" :key="index" class="attr-row">
          <input v-model="item.label" placeholder="显示名（如 机柜号）">
          <input v-model="item.name" placeholder="字段名（如 rack_no）">
          <select v-if="isSelectField(item.name)" v-model="item.value"><option v-for="o in selectOptions(item.name)" :key="o" :value="o">{{ o }}</option></select>
          <input v-else v-model="item.value" placeholder="值">
          <button type="button" class="attr-remove" @click="removeAttr(index)"><Trash2 /></button>
        </div>
        <div v-if="!attrs.length" class="attr-empty">暂无扩展字段</div>
      </section>
      <section class="attr-editor rel-editor">
        <header><h3>资产关系（可选）</h3><button type="button" @click="addRelation"><Plus /> 添加关系</button></header>
        <div v-for="(rel, index) in relations" v-show="rel.type !== 'runs-on' || form.type !== 'virtual-machine'" :key="index" class="rel-row">
          <select v-model="rel.type"><option value="belongs-to">属于(belongs-to)</option><option value="depends-on">依赖(depends-on)</option><option value="located-in">位于(located-in)</option><option value="runs-on">运行于(runs-on)</option></select>
          <input v-model="rel.targetId" placeholder="目标资产ID">
          <input v-model="rel.targetName" placeholder="目标资产名称">
          <button type="button" class="attr-remove" @click="removeRelation(index)"><Trash2 /></button>
        </div>
        <p v-if="!relations.length" class="attr-empty">暂未配置关系（如：该服务运行于哪台主机、位于哪个机房）</p>
      </section>
      <div v-if="errors.length || props.serverError" ref="errorBox" class="form-errors"><span v-for="error in errors" :key="error">{{ error }}</span><span v-if="props.serverError">{{ props.serverError }}</span></div>
      <footer>
        <button type="button" @click="emit('close')">取消</button>
        <button class="primary" type="submit" :disabled="props.saving">{{ props.saving ? '保存中...' : '保存资产' }}</button>
      </footer>
    </form>
  </div>
</template>
<style scoped>
.attr-editor { margin-top: 14px; border-top: 1px dashed var(--line, #2a3550); padding-top: 12px; }
.attr-editor > header { display: flex; align-items: center; justify-content: space-between; }
.attr-editor h3 { margin: 0 0 4px; font-size: 14px; }
.attr-editor > header button { display: inline-flex; align-items: center; gap: 4px; }
.attr-hint { margin: 0 0 8px; font-size: 12px; opacity: .65; }
.attr-row { display: grid; grid-template-columns: 1fr 1fr 2fr auto; gap: 8px; margin-bottom: 8px; }
.attr-row input { width: 100%; box-sizing: border-box; }
.attr-remove { background: none; border: none; cursor: pointer; color: var(--danger, #f56c6c); }
.attr-empty { font-size: 12px; opacity: .5; }
.rel-row { display: grid; grid-template-columns: 1.2fr 1fr 1fr auto; gap: 8px; margin-bottom: 8px; }
.idc-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; }
.idc-grid label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; }
.rel-row input, .rel-row select { width: 100%; box-sizing: border-box; }
.form-errors { position: sticky; bottom: 0; z-index: 2; display: flex; flex-direction: column; gap: 4px; margin: 14px 0; padding: 10px 12px; border: 1px solid #7f1d1d; border-radius: 8px; background: #46151d; color: #fecdd3; }
</style>











<style scoped>
.field-help { display:block; margin-top:4px; color:#6f8ca1; font-size:10px; }
.type-change-note { margin-top:12px; padding:9px 11px; border:1px solid #765b2c; border-radius:7px; background:rgba(255,193,103,.08); color:#f0c783; font-size:11px; line-height:1.55; }
.host-grid { grid-template-columns: .7fr 1.3fr; }
.host-grid .wide { grid-column: 1 / -1; }
</style>
