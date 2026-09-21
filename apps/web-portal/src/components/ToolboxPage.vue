<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Activity, Boxes, Clock3, Cpu, Database, FileSearch, GitBranch, History, Network, Play, Radio, RefreshCw, Search, Server, ShieldCheck, Terminal } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'
import { categoryLabel, resultTone } from '@/features/toolbox/toolboxModel'

type Field = { name:string; label:string; placeholder:string; required:boolean; secret?:boolean }
type Tool = { id:string; name:string; category:string; description:string; icon:string; placeholder:string; risk:string; configured:boolean; readOnly:boolean; fields:Field[] }
type Line = { time:string; level:string; text:string }
type Result = { id:string; toolId:string; toolName:string; category:string; target:string; status:string; operator:string; startedAt:string; finishedAt:string; duration:string; durationMs:number; summary:string; error?:string; output:Line[]; parameters?:Record<string,string> }
type Summary = { tools:number; configuredTools:number; executionsToday:number; successRate:number; avgDuration:string; failedToday:number }
type Credential = { id:string; name:string; kind:string; username:string; group:string; description:string }

const auth = useAuthStore()
const tools = ref<Tool[]>([])
const history = ref<Result[]>([])
const credentials = ref<Credential[]>([])
const summary = ref<Summary>()
const selectedTool = ref<Tool>()
const selectedResult = ref<Result>()
const running = ref(false)
const loading = ref(false)
const error = ref('')
const form = reactive<Record<string, string>>({ target:'', query:'', credentialId:'', namespace:'', name:'', container:'', environment:'' })
const headers = computed(() => ({ Authorization:`Bearer ${auth.token}`, 'Content-Type':'application/json' }))
const iconMap = { Radio, Network, Server, Boxes, Search, ShieldCheck, Activity, FileSearch, Cpu, Database, GitBranch }

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [summaryResponse, toolsResponse, historyResponse] = await Promise.all([
      fetch('/api/v1/toolbox/summary', { headers: headers.value }),
      fetch('/api/v1/toolbox/tools', { headers: headers.value }),
      fetch('/api/v1/toolbox/history?limit=50', { headers: headers.value }),
    ])
    if (!summaryResponse.ok || !toolsResponse.ok || !historyResponse.ok) throw new Error('诊断工具箱数据加载失败')
    summary.value = await summaryResponse.json()
    tools.value = await toolsResponse.json()
    history.value = await historyResponse.json()
    if (!selectedTool.value && tools.value.length) selectTool(tools.value[0])
    else if (selectedTool.value) selectedTool.value = tools.value.find(item => item.id === selectedTool.value?.id) || tools.value[0]
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '诊断工具箱数据加载失败'
  } finally {
    loading.value = false
  }
  fetch('/api/v1/credentials', { headers: headers.value })
    .then(response => response.ok ? response.json() : [])
    .then(items => { credentials.value = Array.isArray(items) ? items : [] })
    .catch(() => { credentials.value = [] })
}

function selectTool(tool: Tool) {
  selectedTool.value = tool
  selectedResult.value = undefined
  error.value = ''
  const next = { ...form, credentialId:form.credentialId || credentials.value[0]?.id || '' }
  Object.assign(form, { target:'', query:'', namespace:'', name:'', container:'', environment:'', credentialId:next.credentialId })
}

async function run() {
  if (!selectedTool.value) return
  if (!selectedTool.value.configured) {
    error.value = '该工具尚未配置或当前不可用'
    return
  }
  const missing = selectedTool.value.fields.find(field => field.required && !String(form[field.name] || '').trim())
  if (missing) {
    error.value = `请填写${missing.label}`
    return
  }
  running.value = true
  error.value = ''
  try {
    const response = await fetch('/api/v1/toolbox/executions', {
      method:'POST',
      headers:headers.value,
      body:JSON.stringify({ toolId:selectedTool.value.id, ...form }),
    })
    const payload = await response.json().catch(() => ({}))
    if (!response.ok) throw new Error(payload.message || (response.status === 422 ? '参数校验失败' : '诊断执行失败'))
    selectedResult.value = payload
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '诊断执行失败'
  } finally {
    running.value = false
  }
}

function useHistory(item: Result) {
  selectedResult.value = item
  if (item.parameters) Object.assign(form, item.parameters)
}

onMounted(load)
</script>

<template>
  <section class="toolbox-page">
    <header class="toolbox-heading">
      <div><span>SAFE DIAGNOSTIC WORKBENCH</span><h1>运维排查工具箱</h1><p>服务端只读执行、结果持久化、全流程审计</p></div>
      <button :disabled="loading" @click="load"><RefreshCw :class="{spin:loading}"/>刷新记录</button>
    </header>

    <div class="toolbox-summary">
      <article><Terminal/><span>已配置工具<strong>{{ summary?.configuredTools || 0 }}/{{ summary?.tools || 0 }}</strong></span></article>
      <article><Play/><span>今日执行<strong>{{ summary?.executionsToday || 0 }}</strong></span></article>
      <article><Radio/><span>成功率<strong>{{ summary?.successRate || 0 }}%</strong></span></article>
      <article><Clock3/><span>平均耗时<strong>{{ summary?.avgDuration || '--' }}</strong></span></article>
    </div>

    <div v-if="error" class="toolbox-error">{{ error }}</div>

    <div class="toolbox-workspace">
      <aside class="tool-menu">
        <h3>诊断工具</h3>
        <button v-for="tool in tools" :key="tool.id" :class="{active:selectedTool?.id===tool.id}" @click="selectTool(tool)">
          <component :is="iconMap[tool.icon as keyof typeof iconMap]" />
          <div><strong>{{ tool.name }}</strong><span>{{ categoryLabel(tool.category) }}</span></div>
          <i v-if="!tool.configured">未配置</i>
        </button>
      </aside>

      <main class="diagnostic-console">
        <header>
          <div><span>ACTIVE TOOL</span><h2>{{ selectedTool?.name || '请选择诊断工具' }}</h2><p>{{ selectedTool?.description }}</p></div>
          <div class="tool-badges"><b :data-risk="selectedTool?.risk">{{ selectedTool?.risk }} risk</b><b :data-ready="selectedTool?.configured">{{ selectedTool?.configured ? 'READY' : 'NOT CONFIGURED' }}</b></div>
        </header>

        <form v-if="selectedTool" class="diagnostic-form" @submit.prevent="run">
          <label v-for="field in selectedTool.fields" :key="field.name" :class="{wide:selectedTool.fields.length>3}">
            <span>{{ field.label }}<b v-if="field.required">*</b></span>
            <select v-if="field.name==='credentialId' && credentials.length" v-model="form.credentialId">
              <option value="">请选择凭据</option>
              <option v-for="credential in credentials" :key="credential.id" :value="credential.id">{{ credential.name }} · {{ credential.kind }} · {{ credential.username }}</option>
            </select>
            <input v-else v-model="form[field.name]" :type="field.secret ? 'password' : 'text'" :placeholder="field.placeholder" autocomplete="off" />
          </label>
          <button class="primary" :disabled="running || !selectedTool.configured"><Play/>{{ running ? '正在执行' : '开始诊断' }}</button>
        </form>

        <div class="terminal-output">
          <header><Terminal/>诊断输出 <span>{{ selectedResult?.id }}</span></header>
          <template v-if="selectedResult">
            <p v-for="line in selectedResult.output" :key="line.time+line.text" :data-level="line.level"><time>{{ line.time }}</time><b>[{{ line.level }}]</b>{{ line.text }}</p>
            <footer :data-tone="resultTone(selectedResult.status)">{{ selectedResult.summary }} · 耗时 {{ selectedResult.duration }}<em v-if="selectedResult.error">{{ selectedResult.error }}</em></footer>
          </template>
          <div v-else class="terminal-empty">等待执行诊断任务...</div>
        </div>
      </main>

      <aside class="diagnostic-history">
        <header><History/><h3>执行记录</h3></header>
        <button v-for="item in history" :key="item.id" :class="{active:selectedResult?.id===item.id}" @click="useHistory(item)">
          <i :data-tone="resultTone(item.status)"></i>
          <div><strong>{{ item.toolName }}</strong><span>{{ item.target }}</span><small>{{ item.operator }} · {{ item.startedAt }} · {{ item.duration }}</small></div>
        </button>
        <div v-if="!history.length" class="history-empty">暂无真实执行记录</div>
      </aside>
    </div>
  </section>
</template>