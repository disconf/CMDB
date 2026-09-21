<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Bot, BrainCircuit, CheckCircle2, Clock3, FileSearch, GitBranch, Lightbulb, Play, RefreshCw, Search, ShieldCheck, Sparkles, TicketCheck } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'
import { confidenceTone, riskLabel } from '@/features/aiops/aiopsModel'

type Alert = { id:string; title:string; severity:string; status:string; target:string; message:string; startedAt:string; duration:string; owner:string }
type Evidence = { type:string; source?:string; title:string; detail:string; weight:number }
type Recommendation = { runbookId:string; title:string; risk:string; impact:string; duration:string; steps:string[] }
type RelatedAsset = { id:string; name:string; type:string; status:string; ip:string; environment:string; projectGroup:string; owner:string; location:string; relation?:string }
type Impact = { total:number; healthy:number; warning:number; critical:number; items:string[] }
type Timeline = { time:string; type:string; source:string; message:string }
type RelatedTicket = { id:string; title:string; status:string; assignee:string }
type RelatedRelease = { id:string; application:string; version:string; status:string; startedAt:string }
type Analysis = { id:string; alertId:string; alertTitle:string; title:string; severity:string; target:string; rootCause:string; confidence:number; summary:string; createdAt:string; evidence:Evidence[]; recommendations:Recommendation[]; relatedAssets:RelatedAsset[]; impact:Impact; timeline:Timeline[]; tickets:RelatedTicket[]; releases:RelatedRelease[]; dataSources:string[] }
type Knowledge = { id:string; title:string; category:string; summary:string; tags:string[]; helpful:number }
type Execution = { id:string; status:string; ticketId?:string; requestedAt:string; runbookTitle?:string }
type Summary = { openAnomalies:number; analyzedToday:number; averageConfidence:number; accuracy:number; knowledgeCount:number; pendingActions:number }

const auth = useAuthStore()
const summary = ref<Summary>()
const alerts = ref<Alert[]>([])
const analyses = ref<Analysis[]>([])
const knowledge = ref<Knowledge[]>([])
const executions = ref<Execution[]>([])
const selected = ref<Analysis>()
const query = ref('')
const tab = ref<'analysis'|'knowledge'>('analysis')
const message = ref('')
const error = ref('')
const loading = ref(false)
const generating = ref(false)
const selectedAlertID = ref('')
const headers = computed(() => ({ Authorization:`Bearer ${auth.token}`, 'Content-Type':'application/json' }))

async function load() {
  loading.value = true
  error.value = ''
  try {
    const responses = await Promise.all(['summary','alerts','analyses','knowledge','executions'].map(path => fetch('/api/v1/aiops/'+path, { headers:headers.value })))
    if (responses.some(response => !response.ok)) throw new Error('AIOps 数据加载失败')
    const [summaryData, alertData, analysisData, knowledgeData, executionData] = await Promise.all(responses.map(response => response.json()))
    summary.value = summaryData
    alerts.value = alertData
    analyses.value = analysisData
    knowledge.value = knowledgeData
    executions.value = executionData
    selected.value = analyses.value.find(item => item.id === selected.value?.id) || analyses.value[0]
    if (!selectedAlertID.value && alerts.value.length) selectedAlertID.value = alerts.value[0].id
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : 'AIOps 数据加载失败'
  } finally {
    loading.value = false
  }
}

async function generate() {
  generating.value = true
  error.value = ''
  message.value = ''
  try {
    const response = await fetch('/api/v1/aiops/analyses/generate', { method:'POST', headers:headers.value, body:JSON.stringify(selectedAlertID.value ? { alertId:selectedAlertID.value } : {}) })
    const payload = await response.json().catch(() => ({}))
    if (!response.ok) throw new Error(payload.message || '分析生成失败')
    message.value = selectedAlertID.value ? '已生成 1 条真实告警分析' : `已生成 ${Array.isArray(payload) ? payload.length : 0} 条分析`
    await load()
    if (!selectedAlertID.value && analyses.value.length) selected.value = analyses.value[0]
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '分析生成失败'
  } finally {
    generating.value = false
  }
}

async function search() {
  const response = await fetch('/api/v1/aiops/knowledge?q='+encodeURIComponent(query.value), { headers:headers.value })
  knowledge.value = await response.json()
}

async function execute(rec:Recommendation) {
  if (!selected.value) return
  message.value = ''
  error.value = ''
  try {
    const response = await fetch(`/api/v1/aiops/analyses/${selected.value.id}/execute`, { method:'POST', headers:headers.value, body:JSON.stringify({ runbookId:rec.runbookId }) })
    const payload = await response.json().catch(() => ({}))
    if (!response.ok) throw new Error(payload.message || '审批工单创建失败')
    message.value = `执行申请已转为真实审批工单 ${payload.ticketId || payload.id}，平台不会直接操作服务器`
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '审批工单创建失败'
  }
}

function statusLabel(value:string) { return ({ firing:'触发中', acknowledged:'已确认', pending:'待审批', approved:'已通过', completed:'已完成', failed:'失败', success:'成功', running:'运行中' } as Record<string,string>)[value] || value }
onMounted(load)
</script>
<template>
  <section class="aiops-page">
    <header class="aiops-heading">
      <div><span>AI-ASSISTED OPERATIONS</span><h1>AIOps 真实分析中心</h1><p>从告警、指标、CMDB、拓扑、工单和发布记录构建证据链</p></div>
      <div class="aiops-actions">
        <select v-model="selectedAlertID"><option value="">全部未恢复告警</option><option v-for="alert in alerts" :key="alert.id" :value="alert.id">{{ alert.severity }} · {{ alert.title }}</option></select>
        <button class="primary" :disabled="generating || !alerts.length" @click="generate"><Sparkles :class="{spin:generating}"/>{{ generating ? '分析中' : '生成分析' }}</button>
        <button :disabled="loading" @click="load"><RefreshCw :class="{spin:loading}"/>刷新</button>
      </div>
    </header>

    <div class="aiops-summary">
      <article><BrainCircuit/><span>未恢复异常<strong>{{ summary?.openAnomalies || 0 }}</strong></span></article>
      <article><Sparkles/><span>今日分析<strong>{{ summary?.analyzedToday || 0 }}</strong></span></article>
      <article><ShieldCheck/><span>平均置信度<strong>{{ Math.round(summary?.averageConfidence || summary?.accuracy || 0) }}%</strong></span></article>
      <article><FileSearch/><span>知识条目<strong>{{ summary?.knowledgeCount || 0 }}</strong></span></article>
      <article><TicketCheck/><span>待审批动作<strong>{{ summary?.pendingActions || 0 }}</strong></span></article>
    </div>

    <div v-if="error" class="aiops-message error">{{ error }}</div>
    <div v-if="message" class="aiops-message"><CheckCircle2/>{{ message }}</div>

    <nav class="aiops-tabs"><button :class="{active:tab==='analysis'}" @click="tab='analysis'">智能分析</button><button :class="{active:tab==='knowledge'}" @click="tab='knowledge'">知识库</button></nav>

    <div v-if="tab==='analysis'" class="aiops-workspace">
      <aside class="analysis-list">
        <header><span>分析记录</span><b>{{ analyses.length }}</b></header>
        <button v-for="item in analyses" :key="item.id" :class="{active:selected?.id===item.id}" @click="selected=item">
          <i :data-severity="item.severity"></i>
          <div><strong>{{ item.title }}</strong><span>{{ item.target }} · {{ item.createdAt }}</span></div>
          <b :data-tone="confidenceTone(item.confidence)">{{ item.confidence }}%</b>
        </button>
        <div v-if="!analyses.length" class="analysis-empty"><Bot/><strong>暂无真实分析</strong><span>选择未恢复告警后点击“生成分析”</span></div>
      </aside>

      <main v-if="selected" class="analysis-detail">
        <header>
          <div><span>ROOT CAUSE ANALYSIS</span><h2>{{ selected.rootCause }}</h2><p>{{ selected.summary }}</p><div class="analysis-meta"><b :data-severity="selected.severity">{{ selected.severity }}</b><span>{{ selected.alertTitle }}</span><span>{{ selected.target }}</span></div></div>
          <div class="confidence-ring" :style="{'--score':selected.confidence+'%'}"><strong>{{ selected.confidence }}%</strong><span>置信度</span></div>
        </header>

        <section class="context-grid">
          <article><h3><GitBranch/>关联资产</h3><div v-if="selected.relatedAssets.length" class="asset-chips"><span v-for="asset in selected.relatedAssets" :key="asset.id"><b>{{ asset.name }}</b><small>{{ asset.type }} · {{ asset.environment }} · {{ asset.relation }}</small></span></div><p v-else>未匹配到 CMDB 资产</p></article>
          <article><h3><BrainCircuit/>拓扑影响</h3><div class="impact-summary"><span data-status="critical">严重 {{ selected.impact.critical }}</span><span data-status="warning">关注 {{ selected.impact.warning }}</span><span data-status="healthy">健康 {{ selected.impact.healthy }}</span></div><small>共 {{ selected.impact.total }} 个关联节点</small><ul><li v-for="item in selected.impact.items.slice(0,5)" :key="item">{{ item }}</li></ul></article>
        </section>
        <h3><GitBranch/>归因证据链</h3>
        <div class="evidence-grid"><article v-for="evidence in selected.evidence" :key="evidence.title"><header><b>{{ evidence.type }} · {{ evidence.source }}</b><strong>{{ evidence.weight }}%</strong></header><h4>{{ evidence.title }}</h4><p>{{ evidence.detail }}</p><div><i :style="{width:evidence.weight+'%'}"></i></div></article></div>

        <section class="analysis-columns">
          <article><h3><Clock3/>故障时间线</h3><ol class="timeline"><li v-for="item in selected.timeline.slice(0,12)" :key="item.time+item.message"><time>{{ item.time }}</time><b>{{ item.source }}</b><span>{{ item.message }}</span></li></ol></article>
          <article><h3><TicketCheck/>关联工单与发布</h3><div class="related-list"><span v-for="ticket in selected.tickets" :key="ticket.id"><b>{{ ticket.id }}</b>{{ ticket.title }}<small>{{ statusLabel(ticket.status) }} · {{ ticket.assignee }}</small></span><span v-for="release in selected.releases" :key="release.id"><b>{{ release.application }} {{ release.version }}</b>{{ release.id }}<small>{{ statusLabel(release.status) }} · {{ release.startedAt }}</small></span><em v-if="!selected.tickets.length && !selected.releases.length">暂无关联记录</em></div></article>
        </section>

        <h3><Lightbulb/>推荐 Runbook</h3>
        <article v-for="rec in selected.recommendations" :key="rec.runbookId" class="runbook">
          <header><div><strong>{{ rec.title }}</strong><span>{{ riskLabel(rec.risk) }} · {{ rec.duration }}</span></div><b>{{ rec.impact }}</b></header>
          <ol><li v-for="step in rec.steps" :key="step">{{ step }}</li></ol>
          <footer><span><ShieldCheck/>只创建审批工单，不直接执行服务器操作</span><button class="primary" @click="execute(rec)"><Play/>申请审批</button></footer>
        </article>
      </main>
      <main v-else class="analysis-empty detail"><Bot/><strong>选择左侧分析查看证据链</strong><span>也可以先选择一条未恢复告警生成分析</span></main>
    </div>

    <div v-else class="knowledge-page">
      <form @submit.prevent="search"><Search/><input v-model="query" placeholder="搜索故障现象、技术栈或关键词"><button>检索知识</button></form>
      <div class="knowledge-grid"><article v-for="item in knowledge" :key="item.id"><b>{{ item.category }}</b><h3>{{ item.title }}</h3><p>{{ item.summary }}</p><div><span v-for="tag in item.tags" :key="tag">{{ tag }}</span></div><footer>{{ item.helpful }} 次解决问题</footer></article></div>
    </div>
  </section>
</template>