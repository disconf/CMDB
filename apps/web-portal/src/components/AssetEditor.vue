<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { Plus, Trash2, X } from 'lucide-vue-next'
import type { Asset, AssetInput, Attribute, CiModel } from '@/features/cmdb/types'
import { validateAssetForm } from '@/features/cmdb/assetForm'

const props = defineProps<{ asset?: Asset; models: CiModel[] }>()
const emit = defineEmits<{ close: []; save: [value: AssetInput] }>()
const errors = ref<string[]>([])
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

function modelFields() {
  const model = props.models.find((m) => m.code === form.type)
  return model ? model.fields : []
}
function prefillFromModel() {
  if (isEdit.value) return
  const existing = new Set(attrs.value.map((a) => a.name))
  const added = modelFields().filter((f) => !existing.has(f.name)).map((f) => ({ name: f.name, label: f.label, value: f.defaultValue || '' }))
  if (added.length) attrs.value = [...attrs.value, ...added]
}
if (!isEdit.value) prefillFromModel()
watch(() => form.type, () => { if (!isEdit.value) prefillFromModel() })

function addAttr() { attrs.value.push({ name: '', label: '', value: '' }) }
function removeAttr(index: number) { attrs.value.splice(index, 1) }

function submit() {
  form.tags = tagsText.value.split('|').map((v) => v.trim()).filter(Boolean)
  errors.value = validateAssetForm(form)
  if (errors.value.length) return
  const attributes = attrs.value.map((a) => ({ name: a.name.trim(), label: a.label.trim(), value: a.value.trim() })).filter((a) => a.name || a.value)
  emit('save', { ...form, tags: [...form.tags], attributes: attributes.length ? attributes : undefined })
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
        <label><span>CI 模型 *</span><select v-model="form.type" :disabled="isEdit">
          <option v-for="model in models" :key="model.code" :value="model.code">{{ model.name }}</option>
        </select></label>
        <label><span>状态</span><select v-model="form.status"><option value="online">在线</option><option value="warning">异常</option><option value="offline">离线</option></select></label>
        <label><span>IP 地址</span><input v-model="form.ip"></label>
        <label><span>环境</span><select v-model="form.environment"><option>生产</option><option>测试</option><option>开发</option></select></label>
        <label><span>项目组 *</span><input v-model="form.projectGroup"></label>
        <label><span>负责人 *</span><input v-model="form.owner"></label>
        <label class="wide"><span>位置</span><input v-model="form.location"></label>
        <label class="wide"><span>标签（使用 | 分隔）</span><input v-model="tagsText"></label>
      </div>
      <section class="attr-editor">
        <header>
          <h3>扩展属性 / 自定义字段</h3>
          <button type="button" @click="addAttr"><Plus /> 添加字段</button>
        </header>
        <p v-if="!isEdit" class="attr-hint">选择 CI 模型后自动带出字段（如带外管理IP、机房、机柜、U 位等），也可手动添加。</p>
        <div v-for="(item, index) in attrs" :key="index" class="attr-row">
          <input v-model="item.label" placeholder="显示名（如 机柜号）">
          <input v-model="item.name" placeholder="字段名（如 rack_no）">
          <input v-model="item.value" placeholder="值">
          <button type="button" class="attr-remove" @click="removeAttr(index)"><Trash2 /></button>
        </div>
        <div v-if="!attrs.length" class="attr-empty">暂无扩展字段</div>
      </section>
      <div v-if="errors.length" class="form-errors"><span v-for="error in errors" :key="error">{{ error }}</span></div>
      <footer>
        <button type="button" @click="emit('close')">取消</button>
        <button class="primary" type="submit">保存资产</button>
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
</style>
