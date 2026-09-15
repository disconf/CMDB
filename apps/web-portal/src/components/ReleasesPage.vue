<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Box, Check, Clock3, GitCommit, KeyRound, Play, RefreshCw, RotateCcw, Rocket, ShieldAlert, ShieldCheck, XCircle } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'
import { releaseStatusLabel } from '@/features/releases/releasesModel'

type Stage = { id:string; name:string; status:string; duration:string; message:string }
type Release = { id:string; application:string; version:string; previousVersion:string; environment:string; status:string; progress:number; operator:string; startedAt:string; strategy:string; approval:string; approvalType?:string; approvalRequester?:string; approvalComment?:string; approvedBy?:string; approvedAt?:string; authorizationExpiresAt?:string; authorizationUsedAt?:string; ticketId:string; source:string; externalId:string; commit:string; stages:Stage[] }
type Artifact = { id:string; application:string; version:string; commit:string; size:string; createdAt:string; security:string; source:string; url:string }
type Summary = { today:number; running:number; success:number; failed:number; successRate:number }
type Executor = { mode:string; ready:boolean; endpoint?:string }
type ApprovalResponse = { release:Release; authorizationToken:string; expiresAt:string }

const auth = useAuthStore()
const releases = ref<Release[]>([])
const artifacts = ref<Artifact[]>([])
const summary = ref<Summary>()
const executor = ref<Executor>()
const selected = ref<Release>()
const tab = ref<'releases'|'artifacts'>('releases')
const loading = ref(false)
const busy = ref(false)
const message = ref('')
const error = ref('')
const approvalComment = ref('')
const authorizationToken = ref('')
const executionToken = ref('')
const headers = computed(() => ({ Authorization:`Bearer ${auth.token}`, 'Content-Type':'application/json' }))

async function request<T>(path:string, init?:RequestInit):Promise<T> {
  const response = await fetch(path, { ...init, headers: headers.value })
  const body = await response.json().catch(() => ({ message:'请求失败' }))
  if (!response.ok) throw new Error((body as {message?:string}).message || `请求失败 (${response.status})`)
  return body as T
}

async function load(preferredId?:string) {
  loading.value = true
  error.value = ''
  try {
    const [nextSummary, nextReleases, nextArtifacts, nextExecutor] = await Promise.all([
      request<Summary>('/api/v1/releases/summary'),
      request<Release[]>('/api/v1/releases'),
      request<Artifact[]>('/api/v1/releases/artifacts'),
      request<Executor>('/api/v1/releases/executor'),
    ])
    summary.value = nextSummary
    releases.value = nextReleases
    artifacts.value = nextArtifacts
    executor.value = nextExecutor
    const id = preferredId || selected.value?.id
    if (id) selected.value = releases.value.find(item => item.id === id)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '发布数据加载失败'
  } finally {
    loading.value = false
  }
}

async function start() {
  busy.value = true
  error.value = ''
  try {
    const item = await request<Release>('/api/v1/releases', { method:'POST', body:JSON.stringify({ application:'订单服务', version:'v2.8.0', environment:'staging' }) })
    message.value = `${item.id} 已创建，等待其他管理员审批`
    await load(item.id)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '创建发布失败'
  } finally {
    busy.value = false
  }
}

async function decide(action:'approve'|'reject') {
  if (!selected.value) return
  busy.value = true
  error.value = ''
  try {
    if (action === 'approve') {
      const result = await request<ApprovalResponse>(`/api/v1/releases/${selected.value.id}/approve`, { method:'POST', body:JSON.stringify({ comment:approvalComment.value }) })
      authorizationToken.value = result.authorizationToken
      executionToken.value = result.authorizationToken
      message.value = result.release.status === 'rollback_approved' ? '回滚审批通过，一次性回滚授权已签发' : '审批通过，一次性执行授权已签发；授权有效期 10 分钟且只能使用一次'
      await load(result.release.id)
    } else {
      const item = await request<Release>(`/api/v1/releases/${selected.value.id}/reject`, { method:'POST', body:JSON.stringify({ comment:approvalComment.value }) })
      message.value = '发布申请已驳回'
      await load(item.id)
    }
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '发布审批失败'
  } finally {
    busy.value = false
  }
}

async function authorizeExecution() {
  if (!selected.value) return
  busy.value = true
  error.value = ''
  try {
    const item = await request<Release>(`/api/v1/releases/${selected.value.id}/execute`, { method:'POST', body:JSON.stringify({ authorizationToken:executionToken.value }) })
    message.value = item.status === 'rolling-back' ? '回滚授权已消费，正在执行回滚' : '一次性授权已消费，发布进入执行态并等待流水线事件回写'
    await load(item.id)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '执行授权失败'
  } finally {
    busy.value = false
  }
}

async function rollback() {
  if (!selected.value) return
  busy.value = true
  error.value = ''
  try {
    const item = await request<Release>(`/api/v1/releases/${selected.value.id}/rollback`, { method:'POST' })
    message.value = '回滚申请已提交，等待其他管理员审批'
    await load(item.id)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '回滚申请失败'
  } finally {
    busy.value = false
  }
}

function gateTitle(item:Release) {
  return ({awaiting_approval:'等待发布审批',approved:'审批通过，等待消费执行授权',rejected:'发布申请已驳回',rollback_awaiting_approval:'等待回滚审批',rollback_approved:'回滚审批通过，等待消费授权',rollback_rejected:'回滚申请已驳回','rolling-back':'正在执行回滚'}[item.status] || '发布门禁状态')
}
function gateDetail(item:Release) {
  return ({awaiting_approval:'审批通过后系统才会签发一次性执行授权',approved:'授权有效期 10 分钟，使用后立即失效',rejected:'驳回后的发布不能继续执行',rollback_awaiting_approval:'回滚申请需由其他管理员审批',rollback_approved:'回滚授权有效期 10 分钟，使用后立即失效',rollback_rejected:'回滚申请未通过',['rolling-back']:'回滚授权已消费，正在恢复流量'}[item.status] || '流水线状态由审批与外部事件共同驱动')
}
onMounted(load)
</script>

<template>
  <section class="releases-page">
    <header class="releases-heading">
      <div><span>CONTINUOUS DELIVERY CONTROL</span><h1>发布管理中心</h1><p>审批门禁、一次性授权、制品、发布与回滚全链路控制</p></div>
      <div><span class="release-executor" :data-ready="executor?.ready">执行器：{{executor?.mode||'manual'}}</span><button :disabled="busy" @click="load()"><RefreshCw :class="{spin:loading}"/>刷新</button><button class="primary" :disabled="busy" @click="start"><Rocket/>创建发布</button></div>
    </header>
    <div v-if="error" class="release-message error">{{error}}</div>
    <div v-if="message" class="release-message success">{{message}}</div>
    <div class="release-summary">
      <article><Rocket/><span>今日发布<strong>{{summary?.today||0}}</strong></span></article>
      <article><Clock3/><span>发布中<strong>{{summary?.running||0}}</strong></span></article>
      <article><Check/><span>成功<strong>{{summary?.success||0}}</strong></span></article>
      <article><RotateCcw/><span>失败<strong>{{summary?.failed||0}}</strong></span></article>
      <article><ShieldCheck/><span>成功率<strong>{{summary?.successRate||0}}%</strong></span></article>
    </div>
    <div class="release-workspace">
      <main>
        <nav><button :class="{active:tab==='releases'}" @click="tab='releases'">发布流水线</button><button :class="{active:tab==='artifacts'}" @click="tab='artifacts'">制品仓库</button></nav>
        <template v-if="tab==='releases'">
          <article v-for="item in releases" :key="item.id" class="release-card" :class="{selected:selected?.id===item.id}" @click="selected=item">
            <header><div><small>{{item.id}} · {{item.environment}}</small><h3>{{item.application}} <b>{{item.version}}</b></h3><span>{{item.strategy}} · {{item.source||'manual'}} · {{item.operator}} · {{item.startedAt}}</span></div><em :data-status="item.status">{{releaseStatusLabel(item.status)}}</em></header>
            <div class="pipeline"><div v-for="(stage,index) in item.stages" :key="stage.id" :data-status="stage.status"><i><Check v-if="stage.status==='success'"/><Play v-else-if="stage.status==='running'"/></i><span>{{stage.name}}<small>{{stage.duration||stage.message}}</small></span><b v-if="index<item.stages.length-1"></b></div></div>
            <footer><div><i :style="{width:item.progress+'%'}"></i></div><span>{{item.progress}}%</span></footer>
          </article>
          <div v-if="!releases.length" class="empty-release">暂无发布流水线</div>
        </template>
        <template v-else>
          <article v-for="artifact in artifacts" :key="artifact.id" class="artifact-row"><Box/><div><strong>{{artifact.application}}:{{artifact.version}}</strong><span><GitCommit/>{{artifact.commit}} · {{artifact.size}} · {{artifact.source}}</span></div><time>{{artifact.createdAt}}</time><b><ShieldCheck/>安全扫描通过</b></article>
          <div v-if="!artifacts.length" class="empty-release">暂无制品记录</div>
        </template>
      </main>
      <aside class="release-detail">
        <template v-if="selected">
          <span>RELEASE DETAIL</span><h2>{{selected.application}} {{selected.version}}</h2>
          <div class="release-gate" :data-status="selected.status"><ShieldAlert v-if="['awaiting_approval','rollback_awaiting_approval'].includes(selected.status)"/><ShieldCheck v-else/><div><strong>{{gateTitle(selected)}}</strong><span>{{gateDetail(selected)}}</span></div></div>
          <dl><div><dt>目标环境</dt><dd>{{selected.environment}}</dd></div><div><dt>发布策略</dt><dd>{{selected.strategy}}</dd></div><div><dt>关联工单</dt><dd>{{selected.ticketId||'—'}}</dd></div><div><dt>回滚版本</dt><dd>{{selected.previousVersion}}</dd></div><div><dt>事件来源</dt><dd>{{selected.source||'manual'}}</dd></div><div v-if="selected.externalId"><dt>外部发布 ID</dt><dd>{{selected.externalId}}</dd></div><div v-if="selected.approvedBy"><dt>审批人</dt><dd>{{selected.approvedBy}} · {{selected.approvedAt}}</dd></div><div v-if="selected.authorizationExpiresAt"><dt>授权有效期</dt><dd>{{selected.authorizationExpiresAt}}</dd></div></dl>
          <h3>阶段日志</h3><div class="stage-logs"><article v-for="stage in selected.stages" :key="stage.id" :data-status="stage.status"><i></i><div><strong>{{stage.name}}</strong><span>{{stage.message}} {{stage.duration}}</span></div></article></div>
          <section v-if="selected.status==='awaiting_approval'" class="release-approval">
            <textarea v-model="approvalComment" rows="2" placeholder="审批意见（可选）"/>
            <div><button :disabled="busy" @click="decide('reject')"><XCircle/>驳回</button><button class="primary" :disabled="busy" @click="decide('approve')"><ShieldCheck/>{{selected.approvalType==='rollback'?'审批回滚并签发授权':'审批并签发授权'}}</button></div>
          </section>
          <section v-if="['approved','rollback_approved'].includes(selected.status)" class="release-authorization">
            <div><KeyRound/><span><strong>{{selected.approvalType==='rollback'?'一次性回滚授权':'一次性执行授权'}}</strong><small>{{authorizationToken?'请在 10 分钟内消费，离开页面后无法再次查看':'重新审批后才会签发新的授权'}}</small></span></div>
            <input v-model="executionToken" type="password" placeholder="输入一次性授权码">
            <button class="primary" :disabled="busy||!executionToken.trim()" @click="authorizeExecution"><Play/>{{selected.approvalType==='rollback'?'消费授权并回滚':'消费授权并开始执行'}}</button>
          </section>
          <footer v-if="['success','failed'].includes(selected.status)"><button :disabled="busy" @click="rollback"><RotateCcw/>申请回滚至 {{selected.previousVersion}}</button></footer>
        </template>
        <div v-else class="empty-release"><Rocket/><strong>选择发布流水线</strong><span>查看审批门禁、阶段日志与执行授权</span></div>
      </aside>
    </div>
  </section>
</template>