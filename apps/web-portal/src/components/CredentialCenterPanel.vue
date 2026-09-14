<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { KeyRound, Plus, RefreshCw, ShieldCheck, Trash2 } from 'lucide-vue-next'
import { createCredential, deleteCredential, listCredentials, type Credential } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'

const auth = useAuthStore()
const credentials = ref<Credential[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const message = ref('')
const query = ref('')
const kindFilter = ref('')
const form = ref({ name: '', kind: 'ssh', username: '', secret: '', group: '', description: '' })

const filtered = computed(() => {
  const needle = query.value.trim().toLowerCase()
  return credentials.value.filter((item) => {
    if (kindFilter.value && item.kind !== kindFilter.value) return false
    if (!needle) return true
    return `${item.name} ${item.username} ${item.group} ${item.description}`.toLowerCase().includes(needle)
  })
})
const sshCount = computed(() => credentials.value.filter((item) => item.kind === 'ssh').length)
const snmpCount = computed(() => credentials.value.filter((item) => item.kind === 'snmp').length)

async function load() {
  loading.value = true
  error.value = ''
  try {
    credentials.value = await listCredentials(auth.token)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : 'Vault 凭据加载失败'
  } finally {
    loading.value = false
  }
}

async function submit() {
  if (!form.value.name.trim() || !form.value.secret.trim()) {
    error.value = '请填写凭据名称和密码/Community'
    return
  }
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    await createCredential(auth.token, {
      name: form.value.name.trim(),
      kind: form.value.kind,
      username: form.value.username.trim(),
      secret: form.value.secret,
      group: form.value.group.trim(),
      description: form.value.description.trim(),
    })
    form.value = { name: '', kind: form.value.kind, username: '', secret: '', group: '', description: '' }
    message.value = '凭据已加密写入 Vault'
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '凭据保存失败'
  } finally {
    busy.value = false
  }
}

async function remove(item: Credential) {
  if (!window.confirm(`确认删除凭据「${item.name}」？已绑定该凭据的采集计划需重新选择。`)) return
  busy.value = true
  error.value = ''
  message.value = ''
  try {
    await deleteCredential(auth.token, item.id)
    message.value = `已删除凭据：${item.name}`
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '凭据删除失败'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="discovery-panel credential-center">
    <header>
      <div><h2>凭据中心</h2><span>SSH、SNMP 等敏感凭据仅写入 Vault，前端不回显密码</span></div>
      <button @click="load"><RefreshCw :class="{spin:loading}"/>刷新</button>
    </header>
    <div class="credential-stats">
      <article><KeyRound/><span>凭据总数<strong>{{ credentials.length }}</strong></span></article>
      <article><ShieldCheck/><span>SSH 凭据<strong>{{ sshCount }}</strong></span></article>
      <article><ShieldCheck/><span>SNMP 凭据<strong>{{ snmpCount }}</strong></span></article>
      <article><ShieldCheck/><span>存储后端<strong>HashiCorp Vault</strong></span></article>
    </div>
    <div class="credential-layout">
      <section class="credential-form">
        <header><div><Plus/><h3>新增凭据</h3></div><span>支持账号密码与 SNMP v2c Community</span></header>
        <div class="form-grid">
          <label><span>凭据名称 *</span><input v-model="form.name" placeholder="例如：QA-Linux-运维账号"></label>
          <label><span>类型 *</span><select v-model="form.kind"><option value="ssh">SSH</option><option value="snmp">SNMP</option></select></label>
          <label><span>用户名</span><input v-model="form.username" :placeholder="form.kind === 'snmp' ? 'SNMP v2c 可留空' : 'root'"></label>
          <label><span>密码 / Community *</span><input v-model="form.secret" type="password" placeholder="不会回显，仅保存到 Vault"></label>
          <label><span>绑定分组</span><input v-model="form.group" placeholder="例如：QA 机房、核心网络组"></label>
          <label class="form-wide"><span>说明</span><input v-model="form.description" placeholder="用途、适用范围或轮换说明"></label>
        </div>
        <footer><button class="primary" :disabled="busy" @click="submit">{{ busy ? '保存中...' : '保存到 Vault' }}</button><span v-if="message">{{ message }}</span></footer>
      </section>
      <section class="credential-list">
        <header><div><h3>已登记凭据</h3><span>共 {{ credentials.length }} 条</span></div><div class="credential-filters"><input v-model="query" placeholder="搜索名称/用户名/分组"><select v-model="kindFilter"><option value="">全部类型</option><option value="ssh">SSH</option><option value="snmp">SNMP</option></select></div></header>
        <div v-if="error" class="credential-error">{{ error }}</div>
        <article v-for="item in filtered" :key="item.id" class="credential-row">
          <div class="credential-icon"><KeyRound/></div>
          <div><strong>{{ item.name }}</strong><span>{{ item.kind.toUpperCase() }} · {{ item.username || '未填写用户名' }} · {{ item.group || '未绑定分组' }}</span><small>{{ item.description || '暂无说明' }}</small></div>
          <button class="danger" :disabled="busy" @click="remove(item)"><Trash2/>删除</button>
        </article>
        <div v-if="!filtered.length" class="credential-empty">没有匹配的凭据</div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.credential-stats{display:grid;grid-template-columns:repeat(4,1fr);gap:12px;padding:18px 20px}.credential-stats article{display:flex;align-items:center;gap:11px;padding:13px;border:1px solid #18384a;border-radius:10px;background:#091b29}.credential-stats svg{color:#22d3ee}.credential-stats span{display:flex;flex-direction:column;color:#7890aa;font-size:11px}.credential-stats strong{margin-top:4px;color:#e5f4ff;font-size:15px}.credential-layout{display:grid;grid-template-columns:minmax(360px,.9fr) minmax(480px,1.35fr);gap:16px;padding:0 20px 20px}.credential-form,.credential-list{border:1px solid #18384a;border-radius:12px;background:#081a27;overflow:hidden}.credential-form>header,.credential-list>header{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 15px;border-bottom:1px solid #123043}.credential-form>header>div,.credential-list>header>div{display:flex;align-items:center;gap:8px}.credential-form h3,.credential-list h3{margin:0;color:#dcecff;font-size:14px}.credential-form header svg{color:#22d3ee}.credential-form header span,.credential-list header span{color:#6f91a5;font-size:11px}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:12px;padding:15px}.form-grid label{display:grid;gap:6px;color:#7894a8;font-size:11px}.form-grid input,.form-grid select,.credential-filters input,.credential-filters select{width:100%;box-sizing:border-box;border:1px solid #22465f;background:#061725;color:#dcecff;padding:9px;outline:none}.form-wide{grid-column:1/-1}.credential-form footer{display:flex;align-items:center;gap:10px;padding:0 15px 15px}.credential-form footer span{color:#2dd4bf;font-size:12px}.credential-filters{display:flex;gap:8px;align-items:center}.credential-filters input{width:190px}.credential-error{margin:12px 15px;padding:10px;border:1px solid #7f1d1d;background:#2b1017;color:#fda4af}.credential-list>article{display:grid;grid-template-columns:36px 1fr auto;align-items:center;gap:11px;padding:12px 15px;border-top:1px solid #123043}.credential-list>article>div:nth-child(2){display:flex;flex-direction:column;gap:3px}.credential-list>article strong{color:#dcecff;font-size:13px}.credential-list>article span,.credential-list>article small{color:#7291a5;font-size:11px}.credential-icon{display:grid;place-items:center;width:32px;height:32px;border-radius:8px;background:#0d3345;color:#67e8f9}.credential-list button.danger{display:flex;align-items:center;gap:5px}.credential-empty{padding:34px;text-align:center;color:#64869a}.credential-list button svg{width:14px}@media(max-width:1200px){.credential-stats{grid-template-columns:repeat(2,1fr)}.credential-layout{grid-template-columns:1fr}}@media(max-width:720px){.credential-stats,.form-grid{grid-template-columns:1fr}.form-wide{grid-column:auto}.credential-list>header{align-items:flex-start;flex-direction:column}.credential-filters{width:100%}.credential-filters input{width:100%}}
</style>
