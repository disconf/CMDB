<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Plus } from 'lucide-vue-next'
import { createProjectGroup, fetchProjectGroups } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'
import type { ProjectGroup } from '@/features/cmdb/types'

const props = withDefaults(defineProps<{ modelValue: string; placeholder?: string; allowEmpty?: boolean }>(), {
  placeholder: '选择项目组',
  allowEmpty: false,
})
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const auth = useAuthStore()
const groups = ref<ProjectGroup[]>([])
const error = ref('')
const creating = ref(false)
const newName = ref('')
const busy = ref(false)

// 停用的项目组不再出现在下拉里；但资产当前值必须是停用的那个也保留，否则保存会把值改掉
const options = computed(() => groups.value.filter(item => item.enabled || item.code === props.modelValue))
const unregistered = computed(() => Boolean(props.modelValue) && !groups.value.some(item => item.code === props.modelValue))

async function load() {
  try {
    groups.value = await fetchProjectGroups(auth.token)
    error.value = ''
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '项目组加载失败'
  }
}

async function create() {
  const code = newName.value.trim()
  if (!code) return
  busy.value = true
  try {
    const created = await createProjectGroup(auth.token, { code, name: code })
    groups.value = [...groups.value, created]
    emit('update:modelValue', created.code)
    creating.value = false
    newName.value = ''
    error.value = ''
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '新建项目组失败'
  } finally {
    busy.value = false
  }
}

function onChange(event: Event) {
  emit('update:modelValue', (event.target as HTMLSelectElement).value)
}

onMounted(load)
watch(() => props.modelValue, value => {
  if (value && !groups.value.some(item => item.code === value)) void load()
})
defineExpose({ reload: load })
</script>

<template>
  <div class="project-group-select">
    <select :value="modelValue" @change="onChange">
      <option value="">{{ allowEmpty ? '（保持原项目组）' : '请选择项目组' }}</option>
      <option v-if="unregistered" :value="modelValue">{{ modelValue }}（未纳管）</option>
      <option v-for="item in options" :key="item.code" :value="item.code">{{ item.name }}</option>
    </select>
    <button type="button" class="link-button" :disabled="busy" @click="creating=!creating"><Plus/>新建</button>
    <div v-if="creating" class="inline-create">
      <input v-model="newName" placeholder="项目组名称" @keyup.enter.prevent="create">
      <button type="button" class="primary" :disabled="busy" @click="create">保存</button>
      <button type="button" @click="creating=false">取消</button>
    </div>
    <small v-if="unregistered" class="warn">当前值「{{ modelValue }}」尚未在项目组管理中登记，可在「CMDB 系统配置 → 项目组管理」一键纳管。</small>
    <small v-if="error" class="warn">{{ error }}</small>
  </div>
</template>
