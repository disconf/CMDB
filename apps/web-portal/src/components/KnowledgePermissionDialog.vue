<template>
  <div v-if="open" class="permission-backdrop" @click.self="emit('close')">
    <section class="permission-dialog">
      <header>
        <div>
          <span>KNOWLEDGE ACL</span>
          <h3>权限设置</h3>
          <p>{{ resourceType === 'folder' ? '目录' : '文档' }}：{{ resourceId }}</p>
        </div>
        <button aria-label="关闭" @click="emit('close')"><X/></button>
      </header>
      <div v-if="loading" class="permission-message">正在读取权限…</div>
      <div v-else class="permission-body">
        <div class="permission-list">
          <article v-for="(entry, index) in entries" :key="entry.subjectType + entry.subjectId">
            <div>
              <b>{{ subjectLabel(entry.subjectType) }} · {{ entry.subjectId }}</b>
              <span>{{ permissionLabel(entry.permission) }} · {{ entry.effect === 'deny' ? '拒绝' : '允许' }}{{ entry.inherit ? ' · 向下继承' : '' }}</span>
            </div>
            <button class="danger" @click="entries.splice(index, 1)">移除</button>
          </article>
          <p v-if="!entries.length">当前没有显式权限，按目录继承和知识空间默认权限访问。</p>
        </div>
        <form class="permission-form" @submit.prevent="addEntry">
          <label>授权对象<select v-model="form.subjectType"><option value="user">用户</option><option value="role">角色</option><option value="project_group">项目组</option></select></label>
          <label>对象 ID<input v-model="form.subjectId" placeholder="用户名 / 角色名 / 项目组编码" required></label>
          <label>权限<select v-model="form.permission"><option value="view">查看</option><option value="edit">编辑</option><option value="publish">发布</option><option value="manage">管理</option></select></label>
          <label>效果<select v-model="form.effect"><option value="allow">允许</option><option value="deny">拒绝</option></select></label>
          <label class="inline"><input v-model="form.inherit" type="checkbox">向下继承</label>
          <button class="primary" type="submit">添加</button>
        </form>
        <p v-if="error" class="permission-error">{{ error }}</p>
        <p v-if="message" class="permission-message">{{ message }}</p>
      </div>
      <footer>
        <button @click="emit('close')">取消</button>
        <button class="primary" :disabled="saving" @click="save">{{ saving ? '保存中' : '保存权限' }}</button>
      </footer>
    </section>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { X } from 'lucide-vue-next'
import { fetchKnowledgeACL, saveKnowledgeACL, type KnowledgeACLInput } from '@/api/knowledge'

const props = defineProps<{ open: boolean; resourceType: 'folder' | 'document'; resourceId: string; token: string }>()
const emit = defineEmits<{ (event: 'close'): void; (event: 'saved'): void }>()

const entries = ref<KnowledgeACLInput[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const message = ref('')
const form = reactive<KnowledgeACLInput>({ subjectType: 'user', subjectId: '', permission: 'view', effect: 'allow', inherit: true })

watch(
  () => [props.open, props.resourceType, props.resourceId, props.token],
  async ([open]) => {
    if (!open || !props.resourceId) return
    loading.value = true
    error.value = ''
    message.value = ''
    try {
      const items = await fetchKnowledgeACL(props.token, props.resourceType, props.resourceId)
      entries.value = items.map((item) => ({
        subjectType: item.subjectType,
        subjectId: item.subjectId,
        permission: item.permission,
        effect: item.effect,
        inherit: item.inherit,
      }))
    } catch (reason) {
      error.value = reason instanceof Error ? reason.message : '权限读取失败'
    } finally {
      loading.value = false
    }
  },
  { immediate: true },
)

function addEntry() {
  if (!form.subjectId.trim()) return
  const existing = entries.value.find(
    (item) => item.subjectType === form.subjectType && item.subjectId === form.subjectId.trim(),
  )
  if (existing) {
    Object.assign(existing, form, { subjectId: form.subjectId.trim() })
  } else {
    entries.value.push({ ...form, subjectId: form.subjectId.trim() })
  }
  form.subjectId = ''
}

async function save() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    await saveKnowledgeACL(props.token, props.resourceType, props.resourceId, entries.value)
    message.value = '权限已保存'
    emit('saved')
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '权限保存失败'
  } finally {
    saving.value = false
  }
}

function subjectLabel(value: string) {
  return ({ user: '用户', role: '角色', project_group: '项目组' } as Record<string, string>)[value] || value
}

function permissionLabel(value: string) {
  return ({ view: '查看', edit: '编辑', publish: '发布', manage: '管理' } as Record<string, string>)[value] || value
}
</script>

<style scoped>
.permission-backdrop { position: fixed; inset: 0; z-index: 80; display: grid; place-items: center; padding: 24px; background: rgba(2, 8, 14, .82); }
.permission-dialog { width: min(760px, 96vw); max-height: 90vh; overflow: auto; border: 1px solid #246c98; border-radius: 12px; background: #071925; color: #dcecff; }
.permission-dialog > header { display: flex; justify-content: space-between; align-items: flex-start; padding: 16px 18px; border-bottom: 1px solid #173b55; }
.permission-dialog header span { color: #22d3ee; font-size: 10px; letter-spacing: .14em; }
.permission-dialog h3 { margin: 5px 0; }
.permission-dialog p { margin: 0; color: #718ba2; font-size: 11px; }
.permission-dialog header button { border: 0; background: transparent; color: #8aa6b9; cursor: pointer; }
.permission-body { display: grid; gap: 14px; padding: 16px 18px; }
.permission-list { display: grid; gap: 7px; }
.permission-list article { display: flex; align-items: center; gap: 10px; padding: 10px 12px; border: 1px solid #173b55; background: #0a263a; }
.permission-list article > div { display: grid; flex: 1; gap: 4px; }
.permission-list b { font-size: 12px; }
.permission-list span { color: #7891a4; font-size: 10px; }
.permission-list > p { padding: 16px; border: 1px dashed #21445d; color: #617e92; font-size: 11px; }
.permission-form { display: grid; grid-template-columns: 1fr 1.3fr 1fr 1fr auto auto; gap: 8px; align-items: end; padding: 12px; border: 1px solid #21445d; background: #081f31; }
.permission-form label { display: grid; gap: 5px; color: #7891a4; font-size: 10px; }
.permission-form label.inline { display: flex; align-items: center; gap: 5px; padding-bottom: 8px; }
.permission-form input, .permission-form select { height: 32px; border: 1px solid #22465f; background: #061725; color: #dcecff; padding: 0 7px; }
.permission-form input[type=checkbox] { width: auto; height: auto; }
.permission-dialog button { border: 1px solid #21445d; background: #0b263b; color: #bdd8e9; padding: 7px 10px; cursor: pointer; }
.permission-dialog button.primary { background: #087c9e; border-color: #087c9e; color: #fff; }
.permission-dialog button.danger { border-color: #783e49; color: #ff9aa7; }
.permission-dialog > footer { display: flex; justify-content: flex-end; gap: 8px; padding: 14px 18px; border-top: 1px solid #173b55; }
.permission-error, .permission-message { padding: 8px 10px; font-size: 11px; }
.permission-error { color: #ff9aa7; background: #35161e; border: 1px solid #783e49; }
.permission-message { color: #a5f3fc; background: #082b38; border: 1px solid #155e75; }
@media (max-width: 760px) { .permission-form { grid-template-columns: 1fr 1fr; } }
</style>
