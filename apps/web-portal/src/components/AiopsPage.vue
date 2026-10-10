<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Bot, BrainCircuit, CheckCircle2, Clock3, FileSearch, GitBranch, Lightbulb, Play, RefreshCw, RotateCcw, Search, ShieldCheck, Sparkles, TicketCheck } from 'lucide-vue-next'
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
type Feedback = { status:string; correctedRootCause?:string; note?:string; reviewer:string; reviewedAt:string }
type Analysis = { id:string; alertId:string; alertTitle:string; title:string; severity:string; target:string; rootCause:string; confidence:number; confidenceAdjustment:number; confidenceReason?:string; appliedFeedbackCount:number; summary:string; createdAt:string; evidence:Evidence[]; recommendations:Recommendation[]; relatedAssets:RelatedAsset[]; impact:Impact; timeline:Timeline[]; tickets:RelatedTicket[]; releases:RelatedRelease[]; dataSources:string[]; feedback?:Feedback }
type Knowledge = { id:string; title:string; category:string; summary:string; tags:string[]; helpful:number }
type Execution = { id:string; analysisId:string; status:string; runbookId:string; ticketId?:string; jobId?:string; requestedAt:string; runbookTitle?:string; approvedBy?:string; finishedAt?:string; resultSummary?:string; error?:string; simulated:boolean; rollback:boolean; parentExecutionId?:string }
type Runbook = { runbookId:string; title:string; templateId:string; mode:string; executionMode:string; risk:string; approvalRequired:boolean; enabled:boolean; maxTargets:number; allowedEnvironments:string[]; changeWindow:string; rollbackRunbookId?:string; updatedAt?:string; updatedBy?:string }
type Summary = { openAnomalies:number; analyzedToday:number; averageConfidence:number; accuracy:number; knowledgeCount:number; pendingActions:number; feedbackCount:number; confirmedFeedback:number; rejectedFeedback:number }
type RootCauseFeedbackStat = { rootCause:string; confirmed:number; rejected:number; partial:number; accuracy:number; adjustment:number }
type FeedbackStats = { total:number; confirmed:number; rejected:number; partial:number; accuracy:number; byRootCause:RootCauseFeedbackStat[] }
type RunbookExecutionStat = { runbookId:string; runbookTitle:string; total:number; simulated:number; live:number; success:number; failed:number; inProgress:number; successRate:number }
type ExecutionStats = { total:number; simulated:number; live:number; pendingApproval:number; running:number; success:number; failed:number; rejected:number; successRate:number; byRunbook:RunbookExecutionStat[] }

const auth = useAuthStore()
const summary = ref<Summary>()
const alerts = ref<Alert[]>([])
const analyses = ref<Analysis[]>([])
const knowledge = ref<Knowledge[]>([])
const feedbackStats = ref<FeedbackStats>()
const executionStats = ref<ExecutionStats>()
const executions = ref<Execution[]>([])
const rollbackPendingID = ref('')
const runbooks = ref<Runbook[]>([])
const selectedRunbook = ref<Runbook>()
const selected = ref<Analysis>()
const query = ref('')
const tab = ref<'analysis'|'knowledge'|'runbooks'>('analysis')
const message = ref('')
const error = ref('')
const loading = ref(false)
const generating = ref(false)
const enriching = ref(false)
const selectedAlertID = ref('')
const feedbackStatus = ref('confirmed')
const correctedRootCause = ref('')
const feedbackNote = ref('')
const headers = computed(() => ({ Authorization:`Bearer ${auth.token}`, 'Content-Type':'application/json' }))
const selectedExecutions = computed(() => executions.value.filter(item => item.analysisId === selected.value?.id))

async function load() {
  loading.value = true
  error.value = ''
  try {
    const responses = await Promise.all(['summary','alerts','analyses','knowledge','executions','execution-stats','feedback/stats','runbooks'].map(path => fetch('/api/v1/aiops/'+path, { headers:headers.value })))
    if (responses.some(response => !response.ok)) throw new Error('AIOps 数据加载失败')
    const [summaryData, alertData, analysisData, knowledgeData, executionData, executionStatsData, feedbackStatsData, runbookData] = await Promise.all(responses.map(response => response.json()))
    summary.value = summaryData
    alerts.value = alertData
    analyses.value = analysisData
    knowledge.value = knowledgeData
    executions.value = executionData
    executionStats.value = executionStatsData
    feedbackStats.value = feedbackStatsData
    runbooks.value = runbookData
    selectedRunbook.value = runbooks.value.find(item => item.runbookId===selectedRunbook.value?.runbookId) || runbooks.value[0]
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

async function saveRunbook() {
  if (!selectedRunbook.value) return
  message.value = ''
  error.value = ''
  try {
    const response = await fetch(`/api/v1/aiops/runbooks/${selectedRunbook.value.runbookId}`, { method:'PUT', headers:headers.value, body:JSON.stringify(selectedRunbook.value) })
    const payload = await response.json().catch(() => ({}))
    if (!response.ok) throw new Error(payload.message || 'Runbook 保存失败')
    message.value = `Runbook ${payload.runbookId} 已保存`
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : 'Runbook 保存失败'
  }
}

async function toggleRunbook(item:Runbook) {
  message.value = ''
  error.value = ''
  try {
    const response = await fetch(`/api/v1/aiops/runbooks/${item.runbookId}/toggle`, { method:'POST', headers:headers.value, body:'{}' })
    const payload = await response.json().catch(() => ({}))
    if (!response.ok) throw new Error(payload.message || 'Runbook 状态切换失败')
    message.value = `Runbook ${payload.runbookId} 已${payload.enabled ? '启用' : '禁用'}`
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : 'Runbook 状态切换失败'
  }
}
async function submitFeedback(status:'confirmed'|'rejected'|'partial') {
  if (!selected.value) return
  message.value = ''
  error.value = ''
  try {
    const response = await fetch(`/api/v1/aiops/analyses/${selected.value.id}/feedback`, {
      method:'POST',
      headers:headers.value,
      body:JSON.stringify({ status, correctedRootCause:correctedRootCause.value, note:feedbackNote.value }),
    })
    const payload = await response.json().catch(() => ({}))
    if (!response.ok) throw new Error(payload.message || '分析反馈提交失败')
    selected.value = payload
    message.value = status === 'confirmed' ? '根因已确认，反馈已计入准确率' : '分析反馈已保存'
    correctedRootCause.value = ''
    feedbackNote.value = ''
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '分析反馈提交失败'
  }
}
async function enrichEvidence() {
  if (!selected.value) return
  enriching.value = true
  message.value = ''
  error.value = ''
  try {
    const response = await fetch(`/api/v1/aiops/analyses/${selected.value.id}/evidence`, { method:'POST', headers:headers.value, body:'{}' })
    const payload = await response.json().catch(() => ({}))
    if (!response.ok) throw new Error(payload.message || '证据补充失败')
    selected.value = payload
    message.value = '已补充只读诊断证据'
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '证据补充失败'
  } finally {
    enriching.value = false
  }
}

async function exportReport() {
  if (!selected.value) return
  error.value = ''
  try {
    const response = await fetch(`/api/v1/aiops/analyses/${selected.value.id}/report?format=markdown`, { headers:headers.value })
    if (!response.ok) throw new Error('报告导出失败')
    const blob = await response.blob()
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `${selected.value.id}.md`
    link.click()
    URL.revokeObjectURL(url)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '报告导出失败'
  }
}
function statusLabel(value:string) { return ({ firing:'触发中', acknowledged:'已确认', pending:'待审批', 'pending-approval':'待审批', rejected:'已驳回', approved:'已通过', completed:'已完成', failed:'失败', success:'成功', running:'运行中' } as Record<string,string>)[value] || value }
function rollbackRunbook(item:Execution) { const policy = runbooks.value.find(value => value.runbookId === item.runbookId); return policy?.rollbackRunbookId || '' }
async function requestRollback(item:Execution) {
  rollbackPendingID.value = item.id
  message.value = ''
  error.value = ''
  try {
    const response = await fetch(`/api/v1/aiops/executions/${item.id}/rollback`, { method:'POST', headers:headers.value, body:'{}' })
    const payload = await response.json().catch(() => ({}))
    if (!response.ok) throw new Error(payload.message || '回滚审批申请失败')
    message.value = `回滚申请已转为审批工单 ${payload.ticketId || payload.id}，不会自动执行`
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '回滚审批申请失败'
  } finally {
    rollbackPendingID.value = ''
  }
}
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

    <section v-if="feedbackStats?.total" class="feedback-stats">
      <header><div><h3><ShieldCheck/>反馈学习</h3><p>人工确认和否决结果会调整后续分析置信度。</p></div><b>整体准确率 {{ feedbackStats.accuracy }}% · {{ feedbackStats.total }} 条反馈</b></header>
      <div><article v-for="item in feedbackStats.byRootCause.slice(0,6)" :key="item.rootCause"><strong>{{ item.rootCause }}</strong><span>确认 {{ item.confirmed }} · 否决 {{ item.rejected }} · 部分 {{ item.partial }}</span><b :data-tone="item.adjustment>=0?'up':'down'">置信度调整 {{ item.adjustment>=0?'+':'' }}{{ item.adjustment }}</b></article></div>
    </section>
    <section v-if="executionStats?.total" class="execution-stats">
      <header><div><h3><GitBranch/>执行审计统计</h3><p>演练与真实执行分开统计，回滚申请仍走独立审批。</p></div><b>完成成功率 {{ executionStats.successRate }}% · {{ executionStats.total }} 次</b></header>
      <div><article><span>演练执行</span><strong>{{ executionStats.simulated }}</strong></article><article><span>真实执行</span><strong>{{ executionStats.live }}</strong></article><article><span>待审批</span><strong>{{ executionStats.pendingApproval }}</strong></article><article><span>运行中</span><strong>{{ executionStats.running }}</strong></article><article><span>成功</span><strong>{{ executionStats.success }}</strong></article><article><span>失败</span><strong>{{ executionStats.failed }}</strong></article></div>
    </section>
    <nav class="aiops-tabs"><button :class="{active:tab==='analysis'}" @click="tab='analysis'">智能分析</button><button :class="{active:tab==='runbooks'}" @click="tab='runbooks'">Runbook 策略</button><button :class="{active:tab==='knowledge'}" @click="tab='knowledge'">知识库</button></nav>

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
          <div class="analysis-actions"><button :disabled="enriching" @click="enrichEvidence"><Sparkles :class="{spin:enriching}"/>补充证据</button><button @click="exportReport"><FileSearch/>导出报告</button></div><div class="confidence-ring" :style="{'--score':selected.confidence+'%'}"><strong>{{ selected.confidence }}%</strong><span>置信度</span></div>
        </header>

        <section class="feedback-panel">
          <header><div><h3><ShieldCheck/>根因反馈</h3><p>确认或否定分析结论，用于计算真实准确率并优化后续归因。</p></div><b v-if="selected.feedback" :data-status="selected.feedback.status">{{ statusLabel(selected.feedback.status) }} · {{ selected.feedback.reviewer }} · {{ selected.feedback.reviewedAt }}</b></header>
          <div v-if="selected.feedback" class="feedback-result"><span>{{ selected.feedback.note || '无补充说明' }}</span><em v-if="selected.feedback.correctedRootCause">修正根因：{{ selected.feedback.correctedRootCause }}</em></div>
          <form v-else class="feedback-form" @submit.prevent="submitFeedback(feedbackStatus as 'confirmed')">
            <select v-model="feedbackStatus"><option value="confirmed">确认根因</option><option value="partial">部分正确</option><option value="rejected">否定根因</option></select>
            <input v-model="correctedRootCause" placeholder="修正后的根因（可选）" />
            <input v-model="feedbackNote" placeholder="反馈说明（可选）" />
            <button class="primary" @click.prevent="submitFeedback(feedbackStatus as 'confirmed'|'partial'|'rejected')"><CheckCircle2/>提交反馈</button>
          </form>
        </section>
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

        <section class="execution-panel">
          <header><h3><TicketCheck/>受控执行闭环</h3><p>工单审批通过后，平台只按白名单模板创建任务，并把任务状态回写到 AIOps。</p></header>
          <div v-if="selectedExecutions.length" class="execution-list">
            <article v-for="item in selectedExecutions" :key="item.id">
              <header><div><b>{{ item.runbookTitle || item.runbookId }}</b><span class="execution-kind">{{ item.rollback ? '回滚' : item.simulated ? '演练' : '真实' }}</span></div><span :data-status="item.status">{{ statusLabel(item.status) }}</span></header>
              <dl><div><dt>审批工单</dt><dd>{{ item.ticketId || '—' }}</dd></div><div><dt>任务 ID</dt><dd>{{ item.jobId || '—' }}</dd></div><div><dt>审批人</dt><dd>{{ item.approvedBy || '—' }}</dd></div><div><dt>完成时间</dt><dd>{{ item.finishedAt || '—' }}</dd></div></dl>
              <p>{{ item.resultSummary || '等待工单审批' }}</p><em v-if="item.error">{{ item.error }}</em>
              <button v-if="item.status==='failed' && rollbackRunbook(item)" class="rollback-request" :disabled="rollbackPendingID===item.id" @click="requestRollback(item)"><RotateCcw/>{{ rollbackPendingID===item.id ? '申请中' : '申请回滚审批' }}</button>
            </article>
          </div>
          <div v-else class="execution-empty">尚未提交受控执行申请</div>
        </section>
      </main>
      <main v-else class="analysis-empty detail"><Bot/><strong>选择左侧分析查看证据链</strong><span>也可以先选择一条未恢复告警生成分析</span></main>
    </div>

    <div v-else-if="tab==='runbooks'" class="runbook-page">
      <aside class="runbook-list"><header><h3>Runbook 注册中心</h3><p>变更类默认禁用，启用需要平台全局开关。</p></header><button v-for="item in runbooks" :key="item.runbookId" :class="{active:selectedRunbook?.runbookId===item.runbookId}" @click="selectedRunbook=item"><div><strong>{{ item.title }}</strong><span>{{ item.templateId }} · {{ item.mode }} · {{ item.risk }}</span></div><b :data-enabled="item.enabled">{{ item.enabled ? '已启用' : '已禁用' }}</b></button></aside>
      <main v-if="selectedRunbook" class="runbook-editor">
        <header><div><span>RUNBOOK POLICY</span><h2>{{ selectedRunbook.title }}</h2><p>{{ selectedRunbook.runbookId }}</p></div><button class="primary" @click="saveRunbook">保存策略</button></header>
        <form @submit.prevent="saveRunbook">
          <label>标题<input v-model="selectedRunbook.title" /></label>
          <label>任务模板<input v-model="selectedRunbook.templateId" /></label>
          <label>模式<select v-model="selectedRunbook.mode"><option value="read-only">只读</option><option value="change">变更</option></select></label><label>执行方式<select v-model="selectedRunbook.executionMode"><option value="simulate">演练（不执行）</option><option value="live">真实执行</option></select></label>
          <label>风险<select v-model="selectedRunbook.risk"><option value="low">低</option><option value="medium">中</option><option value="high">高</option></select></label>
          <label>最大目标数<input v-model.number="selectedRunbook.maxTargets" type="number" min="1" max="10" /></label>
          <label>变更窗口<input v-model="selectedRunbook.changeWindow" placeholder="qa=*;production=Sat-Sun" /></label>
          <label v-if="selectedRunbook.mode==='change'">失败回滚 Runbook<select v-model="selectedRunbook.rollbackRunbookId"><option value="">未配置</option><option v-for="item in runbooks.filter(value => value.mode==='change' && value.runbookId!==selectedRunbook?.runbookId)" :key="item.runbookId" :value="item.runbookId">{{ item.title }} · {{ item.enabled ? '已启用' : '未启用' }}</option></select></label>
          <label class="wide">允许环境<input :value="selectedRunbook.allowedEnvironments.join(',')" disabled /></label>
        </form>
        <footer><span><ShieldCheck/>当前阶段所有变更类 Runbook 默认关闭，必须显式开启全局开关。</span><button :class="{danger:selectedRunbook.enabled}" @click="toggleRunbook(selectedRunbook)">{{ selectedRunbook.enabled ? '禁用 Runbook' : '启用 Runbook' }}</button></footer>
      </main>
    </div>
    <div v-else class="knowledge-page">
      <form @submit.prevent="search"><Search/><input v-model="query" placeholder="搜索故障现象、技术栈或关键词"><button>检索知识</button></form>
      <div class="knowledge-grid"><article v-for="item in knowledge" :key="item.id"><b>{{ item.category }}</b><h3>{{ item.title }}</h3><p>{{ item.summary }}</p><div><span v-for="tag in item.tags" :key="tag">{{ tag }}</span></div><footer>{{ item.helpful }} 次解决问题</footer></article></div>
    </div>
  </section>
</template>