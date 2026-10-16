<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Check, Plus, RefreshCw, Trash2, X } from 'lucide-vue-next'
import { createProjectGroup, deleteProjectGroup, fetchProjectGroups, fetchUnregisteredProjectGroups, updateProjectGroup } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'
import type { GroupCount, ProjectGroup } from '@/features/cmdb/types'

const emit = defineEmits<{ close: []; changed: [] }>()
const auth = useAuthStore()
const groups = ref<ProjectGroup[]>([])
const unregistered = ref<GroupCount[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const message = ref('')
const editing = ref<ProjectGroup>()
const form = ref({ code: '', name: '', description: '', owner: '', ownerPhone: '', enabled: true })

const totalAssets = computed(() => groups.value.reduce((sum, item) => sum + item.count, 0))

async function load() {
  loading.value = true
  try {
    const [list, pending] = await Promise.all([fetchProjectGroups(auth.token), fetchUnregisteredProjectGroups(auth.token)])
    groups.value = list
    unregistered.value = pending
    error.value = ''
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '项目组加载失败'
  } finally {
    loading.value = false
  }
}

function edit(item?: ProjectGroup, code?: string) {
  editing.value = item
  form.value = item
    ? { code: item.code, name: item.name, description: item.description, owner: item.owner, ownerPhone: item.ownerPhone, enabled: item.enabled }
    : { code: code || '', name: code || '', description: '', owner: '', ownerPhone: '', enabled: true }
}

async function save() {
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    const payload = { code: form.value.code.trim(), name: form.value.name.trim(), description: form.value.description, owner: form.value.owner, ownerPhone: form.value.ownerPhone, enabled: form.value.enabled }
    if (editing.value) await updateProjectGroup(auth.token, editing.value.code, payload)
    else await createProjectGroup(auth.token, payload)
    message.value = editing.value ? `项目组「${payload.name}」已更新` : `项目组「${payload.name}」已创建`
    editing.value = undefined
    await load()
    emit('changed')
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '保存项目组失败'
  } finally {
    busy.value = false
  }
}

async function remove(item: ProjectGroup) {
  if (!window.confirm(`确认删除项目组「${item.name}」？`)) return
  busy.value = true
  try {
    await deleteProjectGroup(auth.token, item.code)
    message.value = `项目组「${item.name}」已删除`
    await load()
    emit('changed')
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '删除项目组失败'
  } finally {
    busy.value = false
  }
}

async function adopt(item: GroupCount) {
  busy.value = true
  try {
    await createProjectGroup(auth.token, { code: item.group, name: item.group })
    message.value = `已纳管「${item.group}」，该编码下 ${item.count} 个资产自动归入`
    await load()
    emit('changed')
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '纳管失败'
  } finally {
    busy.value = false
  }
}

onMounted(load)
defineExpose({ reload: load })
</script>

<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <section class="project-group-manager">
      <header>
        <div><small>PROJECT GROUPS</small><h2>项目组管理</h2><p>项目组用于资产归属与权限范围；编码会写入资产，创建后不建议修改。</p></div>
        <button aria-label="关闭" @click="emit('close')"><X/></button>
      </header>
      <p v-if="message" class="cmdb-message">{{ message }}</p>
      <p v-if="error" class="cmdb-error">{{ error }}</p>
      <div class="pg-body">
        <div class="pg-list">
          <header><div><strong>已登记项目组</strong><span>{{ groups.length }} 个 · 覆盖 {{ totalAssets }} 个资产</span></div><button :disabled="loading" @click="load"><RefreshCw :class="{spin:loading}"/>刷新</button><button class="primary" @click="edit()"><Plus/>新建</button></header>
          <article v-for="item in groups" :key="item.code" :class="{disabled:!item.enabled}">
            <div><strong>{{ item.name }}</strong><small>{{ item.code }} · {{ item.count }} 个资产<template v-if="item.owner"> · 负责人 {{ item.owner }}</template><template v-if="item.ownerPhone"> {{ item.ownerPhone }}</template></small><span v-if="item.description">{{ item.description }}</span></div>
            <span class="pg-state">{{ item.enabled ? '启用' : '停用' }}</span>
            <button @click="edit(item)">编辑</button>
            <button :disabled="item.count > 0" :title="item.count > 0 ? '该组下还有资产，不能删除' : '删除'" @click="remove(item)"><Trash2/></button>
          </article>
          <div v-if="!groups.length && !loading" class="pg-empty">还没有登记项目组，点击「新建」创建第一个。</div>
        </div>
        <div class="pg-pending">
          <header><div><strong>未纳管取值</strong><span>资产上使用但尚未登记的项目组，可直接纳管</span></div></header>
          <article v-for="item in unregistered" :key="item.group">
            <div><strong>{{ item.group }}</strong><small>{{ item.count }} 个资产正在使用</small></div>
            <button :disabled="busy" @click="adopt(item)"><Check/>纳管</button>
          </article>
          <div v-if="!unregistered.length" class="pg-empty">全部资产的项目组都已纳管。</div>
        </div>
        <form class="pg-form" @submit.prevent="save">
          <header><h3>{{ editing ? '编辑项目组' : '新建项目组' }}</h3></header>
          <label><span>项目组编码 *</span><input v-model="form.code" required :disabled="!!editing" placeholder="例如 core-system"></label>
          <label><span>显示名称 *</span><input v-model="form.name" required placeholder="例如 核心系统组"></label>
          <label><span>负责人</span><input v-model="form.owner" placeholder="姓名"></label>
          <label><span>负责人电话</span><input v-model="form.ownerPhone" placeholder="手机号"></label>
          <label class="wide"><span>说明</span><textarea v-model="form.description" rows="2"></textarea></label>
          <label class="check"><input v-model="form.enabled" type="checkbox"><span>启用（停用后不再出现在下拉里）</span></label>
          <footer>
            <button type="button" @click="editing=undefined;form={code:'',name:'',description:'',owner:'',ownerPhone:'',enabled:true}">重置</button>
            <button class="primary" :disabled="busy">{{ editing ? '保存修改' : '创建项目组' }}</button>
          </footer>
        </form>
      </div>
    </section>
  </div>
</template>
