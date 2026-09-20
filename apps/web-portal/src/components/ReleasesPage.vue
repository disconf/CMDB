<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Box, Check, Clock3, GitCommit, KeyRound, Play, RefreshCw, RotateCcw, Rocket, ShieldAlert, ShieldCheck, XCircle } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'
import { releaseStatusLabel } from '@/features/releases/releasesModel'
import { argoHealthLabel, argoSyncLabel, integrationDetailLabel, integrationStatusLabel, integrationTypeLabel, pipelineStatusLabel, summarizeIntegrations, type CICDIntegration, type CICDResources } from '@/features/releases/cicdModel'

type Stage = { id:string; name:string; status:string; duration:string; message:string }
type Release = { id:string; application:string; version:string; previousVersion:string; environment:string; status:string; progress:number; operator:string; startedAt:string; strategy:string; approval:string; approvalType?:string; approvalRequester?:string; approvalComment?:string; approvedBy?:string; approvedAt?:string; authorizationExpiresAt?:string; authorizationUsedAt?:string; ticketId:string; source:string; externalId:string; commit:string; stages:Stage[] }
type Artifact = { id:string; application:string; version:string; commit:string; size:string; createdAt:string; security:string; source:string; url:string }
type Summary = { today:number; running:number; success:number; failed:number; successRate:number }
type Executor = { mode:string; ready:boolean; endpoint?:string }
type ApprovalResponse = { release:Release; authorizationToken:string; expiresAt:string }
type Tab = 'releases' | 'artifacts' | 'integrations' | 'resources'

const auth = useAuthStore()
const releases = ref<Release[]>([])
const artifacts = ref<Artifact[]>([])
const integrations = ref<CICDIntegration[]>([])
const resources = ref<CICDResources>()
const summary = ref<Summary>()
const executor = ref<Executor>()
const selected = ref<Release>()
const tab = ref<Tab>('releases')
const loading = ref(false)
const busy = ref(false)
const integrationTesting = ref('')
const resourcesLoading = ref(false)
const message = ref('')
const error = ref('')
const approvalComment = ref('')
const authorizationToken = ref('')
const executionToken = ref('')
const headers = computed(() => ({ Authorization:`Bearer ${auth.token}`, 'Content-Type':'application/json' }))
const integrationSummary = computed(() => summarizeIntegrations(integrations.value))

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
    const [nextSummary, nextReleases, nextArtifacts, nextExecutor, nextIntegrations] = await Promise.all([
      request<Summary>('/api/v1/releases/summary'),
      request<Release[]>('/api/v1/releases'),
      request<Artifact[]>('/api/v1/releases/artifacts'),
      request<Executor>('/api/v1/releases/executor'),
      request<CICDIntegration[]>('/api/v1/cicd/integrations'),
    ])
    summary.value = nextSummary
    releases.value = nextReleases
    artifacts.value = nextArtifacts
    executor.value = nextExecutor
    integrations.value = nextIntegrations
    const id = preferredId || selected.value?.id
    if (id) selected.value = releases.value.find(item => item.id === id)
    if (!resources.value) void loadResources()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '发布数据加载失败'
  } finally {
    loading.value = false
  }
}

function replaceIntegration(updated:CICDIntegration) {
  const index = integrations.value.findIndex(item => item.id === updated.id)
  if (index >= 0) integrations.value.splice(index, 1, updated)
}

async function testIntegration(id:string) {
  integrationTesting.value = id
  error.value = ''
  try {
    const updated = await request<CICDIntegration>(`/api/v1/cicd/integrations/${id}/test`, { method:'POST' })
    replaceIntegration(updated)
    message.value = updated.status === 'connected' ? `${updated.name}连接正常` : `${updated.name}检测完成：${updated.message}`
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '集成连接检测失败'
  } finally {
    integrationTesting.value = ''
  }
}

async function testAllIntegrations() {
  const targets = integrations.value.filter(item => item.enabled)
  if (!targets.length) {
    message.value = '当前没有已配置的集成连接器'
    return
  }
  integrationTesting.value = 'all'
  error.value = ''
  try {
    const updated = await Promise.all(targets.map(item => request<CICDIntegration>(`/api/v1/cicd/integrations/${item.id}/test`, { method:'POST' })))
    updated.forEach(replaceIntegration)
    message.value = `已检测 ${updated.length} 个连接器，${updated.filter(item => item.status === 'connected').length} 个连接正常`
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '批量连接检测失败'
  } finally {
    integrationTesting.value = ''
  }
}

async function loadResources(force=false) {
  resourcesLoading.value = true
  error.value = ''
  try {
    resources.value = force
      ? await request<CICDResources>('/api/v1/cicd/resources/refresh', { method:'POST' })
      : await request<CICDResources>('/api/v1/cicd/resources')
    message.value = `CI/CD 资源数据已更新：${formatCheckedAt(resources.value.generatedAt)}`
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : 'CI/CD 资源数据加载失败'
  } finally {
    resourcesLoading.value = false
  }
}

function openTab(next:Tab) {
  tab.value = next
  if (next === 'resources' && !resources.value) void loadResources()
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
function integrationDetails(item:CICDIntegration) {
  return Object.entries(item.details || {}).filter(([key, value]) => key !== 'requiredEnv' && value).slice(0, 4)
}
function formatCheckedAt(value?:string) {
  if (!value) return '尚未检测'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12:false })
}
onMounted(load)
</script>

<template>
  <section class="releases-page">
    <header class="releases-heading">
      <div><span>CONTINUOUS DELIVERY CONTROL</span><h1>CI/CD 与发布管理</h1><p>统一管理工具链接、审批门禁、一次性授权、制品、发布与回滚全链路</p></div>
      <div><span class="release-executor" :data-ready="executor?.ready">执行器：{{executor?.mode||'manual'}}</span><button :disabled="busy" @click="load()"><RefreshCw :class="{spin:loading}"/>刷新</button><button class="primary" :disabled="busy" @click="start"><Rocket/>创建发布</button></div>
    </header>
    <div v-if="error" class="release-message error">{{error}}</div>
    <div v-if="message" class="release-message success">{{message}}</div>

    <div v-if="tab==='integrations'" class="release-summary">
      <article><Box/><span>连接器<strong>{{integrationSummary.total}}</strong></span></article>
      <article><Check/><span>已连通<strong>{{integrationSummary.connected}}</strong></span></article>
      <article><Clock3/><span>待检测<strong>{{integrationSummary.pending}}</strong></span></article>
      <article><XCircle/><span>连接异常<strong>{{integrationSummary.errors}}</strong></span></article>
      <article><ShieldAlert/><span>未配置<strong>{{integrationSummary.notConfigured}}</strong></span></article>
    </div>
    <div v-else-if="tab==='resources'" class="release-summary">
      <article><Box/><span>镜像仓库<strong>{{resources?.harbor.repositories||0}}</strong></span></article>
      <article><Check/><span>Argo 应用<strong>{{resources?.argoCD.applications||0}}</strong></span></article>
      <article><ShieldCheck/><span>已同步<strong>{{resources?.argoCD.synced||0}}</strong></span></article>
      <article><ShieldAlert/><span>异常应用<strong>{{resources?.argoCD.degraded||0}}</strong></span></article>
      <article><GitCommit/><span>代码项目<strong>{{resources?.gitlab.projects||0}}</strong></span></article>
    </div>
    <div v-else class="release-summary">
      <article><Rocket/><span>今日发布<strong>{{summary?.today||0}}</strong></span></article>
      <article><Clock3/><span>发布中<strong>{{summary?.running||0}}</strong></span></article>
      <article><Check/><span>成功<strong>{{summary?.success||0}}</strong></span></article>
      <article><RotateCcw/><span>失败<strong>{{summary?.failed||0}}</strong></span></article>
      <article><ShieldCheck/><span>成功率<strong>{{summary?.successRate||0}}%</strong></span></article>
    </div>

    <div class="release-workspace">
      <main>
        <nav><button :class="{active:tab==='releases'}" @click="openTab('releases')">发布流水线</button><button :class="{active:tab==='artifacts'}" @click="openTab('artifacts')">制品仓库</button><button :class="{active:tab==='integrations'}" @click="openTab('integrations')">CI/CD 集成</button><button :class="{active:tab==='resources'}" @click="openTab('resources')">资源总览</button></nav>

        <template v-if="tab==='releases'">
          <article v-for="item in releases" :key="item.id" class="release-card" :class="{selected:selected?.id===item.id}" @click="selected=item">
            <header><div><small>{{item.id}} · {{item.environment}}</small><h3>{{item.application}} <b>{{item.version}}</b></h3><span>{{item.strategy}} · {{item.source||'manual'}} · {{item.operator}} · {{item.startedAt}}</span></div><em :data-status="item.status">{{releaseStatusLabel(item.status)}}</em></header>
            <div class="pipeline"><div v-for="(stage,index) in item.stages" :key="stage.id" :data-status="stage.status"><i><Check v-if="stage.status==='success'"/><Play v-else-if="stage.status==='running'"/></i><span>{{stage.name}}<small>{{stage.duration||stage.message}}</small></span><b v-if="index<item.stages.length-1"></b></div></div>
            <footer><div><i :style="{width:item.progress+'%'}"></i></div><span>{{item.progress}}%</span></footer>
          </article>
          <div v-if="!releases.length" class="empty-release">暂无发布流水线</div>
        </template>

        <template v-else-if="tab==='artifacts'">
          <article v-for="artifact in artifacts" :key="artifact.id" class="artifact-row"><Box/><div><strong>{{artifact.application}}:{{artifact.version}}</strong><span><GitCommit/>{{artifact.commit}} · {{artifact.size}} · {{artifact.source}}</span></div><time>{{artifact.createdAt}}</time><b><ShieldCheck/>安全扫描通过</b></article>
          <div v-if="!artifacts.length" class="empty-release">暂无制品记录</div>
        </template>

        <template v-else-if="tab==='resources'">
          <div class="integration-toolbar">
            <div><strong>CI/CD 资源总览</strong><span>展示 Harbor 仓库、Argo CD 应用同步状态和 GitLab 项目链路，数据缓存 30 秒。</span></div>
            <button :disabled="resourcesLoading" @click="loadResources(true)"><RefreshCw :class="{spin:resourcesLoading}"/>刷新资源</button>
          </div>
          <div v-if="resources" class="resource-grid">
            <article class="resource-card">
              <header><i><Box/></i><div><small>CONTAINER REGISTRY</small><h3>Harbor 镜像仓库</h3></div><em :data-status="resources.harbor.status">{{resources.harbor.message}}</em></header>
              <div class="resource-kpis"><span>项目<strong>{{resources.harbor.projects}}</strong></span><span>仓库<strong>{{resources.harbor.repositories}}</strong></span></div>
              <div class="resource-list">
                <article v-for="repo in resources.harbor.recentRepositories" :key="`${repo.project}/${repo.name}`"><div><strong>{{repo.project}}/{{repo.name}}</strong><small>更新 {{formatCheckedAt(repo.updatedAt)}}</small></div><span>制品 {{repo.artifactCount}} · 拉取 {{repo.pullCount}}</span></article>
                <div v-if="!resources.harbor.recentRepositories.length" class="resource-empty">暂无可见仓库数据</div>
              </div>
            </article>
            <article class="resource-card resource-card-wide">
              <header><i><Rocket/></i><div><small>GITOPS DELIVERY</small><h3>Argo CD 应用状态</h3></div><em :data-status="resources.argoCD.status">{{resources.argoCD.message}}</em></header>
              <div class="resource-kpis"><span>应用<strong>{{resources.argoCD.applications}}</strong></span><span>健康<strong>{{resources.argoCD.healthy}}</strong></span><span>已同步<strong>{{resources.argoCD.synced}}</strong></span><span>待同步<strong>{{resources.argoCD.outOfSync}}</strong></span><span>异常<strong>{{resources.argoCD.degraded}}</strong></span></div>
              <div class="resource-list">
                <article v-for="app in resources.argoCD.items" :key="`${app.namespace}/${app.name}`"><div><strong>{{app.name}}</strong><small>{{app.project}} · {{app.destination}} · {{app.revision||'无版本信息'}}</small></div><div class="resource-badges"><span :data-status="app.syncStatus">{{argoSyncLabel(app.syncStatus)}}</span><span :data-status="app.healthStatus">{{argoHealthLabel(app.healthStatus)}}</span></div></article>
                <div v-if="!resources.argoCD.items.length" class="resource-empty">暂无 Argo CD 应用数据</div>
              </div>
            </article>
            <article class="resource-card resource-card-wide">
              <header><i><GitCommit/></i><div><small>SOURCE CONTROL</small><h3>GitLab 项目与 Pipeline</h3></div><em :data-status="resources.gitlab.status">{{resources.gitlab.message}}</em></header>
              <div v-if="resources.gitlab.available" class="resource-list">
                <article v-for="project in resources.gitlab.recentProjects" :key="project.id"><div><strong>{{project.name}}</strong><small>{{project.path}} · 默认分支 {{project.defaultBranch||'—'}}</small></div><div class="resource-badges"><span v-if="project.latestPipeline" :data-status="project.latestPipeline.status">{{pipelineStatusLabel(project.latestPipeline.status)}}</span><a v-if="project.webUrl" :href="project.webUrl" target="_blank" rel="noreferrer">打开项目</a></div></article>
                <div v-if="!resources.gitlab.recentProjects.length" class="resource-empty">暂无项目数据</div>
              </div>
              <div v-else class="resource-empty">配置 GitLab 地址与 Token 后，这里会展示项目、默认分支和最近 Pipeline 状态。</div>
            </article>
          </div>
          <div v-else class="empty-release"><RefreshCw :class="{spin:resourcesLoading}"/><strong>正在读取 CI/CD 资源</strong><span>请稍候</span></div>
        </template>

        <template v-else-if="tab==='integrations'">
          <div class="integration-toolbar">
            <div><strong>CI/CD 工具链接</strong><span>统一检测 Harbor、Argo CD、GitLab 的连接与认证状态</span></div>
            <button :disabled="integrationTesting!==''" @click="testAllIntegrations"><RefreshCw :class="{spin:integrationTesting==='all'}"/>检测全部</button>
          </div>
          <div class="integration-grid">
            <article v-for="item in integrations" :key="item.id" class="integration-card" :data-status="item.status">
              <header>
                <i><Box v-if="item.id==='harbor'"/><Rocket v-else-if="item.id==='argocd'"/><GitCommit v-else/></i>
                <div><small>{{integrationTypeLabel(item.type)}}</small><h3>{{item.name}}</h3></div>
                <em>{{integrationStatusLabel(item.status)}}</em>
              </header>
              <p>{{item.message}}</p>
              <dl>
                <div><dt>服务地址</dt><dd>{{item.endpoint||'未配置'}}</dd></div>
                <div v-for="[key,value] in integrationDetails(item)" :key="key"><dt>{{integrationDetailLabel(key)}}</dt><dd>{{value}}</dd></div>
              </dl>
              <footer>
                <span>最后检测：{{formatCheckedAt(item.lastCheckedAt)}}</span>
                <button class="primary" :disabled="!item.enabled || integrationTesting!==''" @click="testIntegration(item.id)"><RefreshCw :class="{spin:integrationTesting===item.id}"/>检测连接</button>
              </footer>
            </article>
          </div>
          <div v-if="!integrations.length" class="empty-release">暂无集成连接器</div>
        </template>
      </main>

      <aside class="release-detail">
        <template v-if="tab==='resources'">
          <span>RESOURCE OVERVIEW</span><h2>资源同步视图</h2>
          <p class="resource-aside-note">Harbor 展示项目与最近更新的镜像仓库；Argo CD 展示应用同步与健康状态；GitLab 配置后展示项目、默认分支和 Pipeline。</p>
          <div class="integration-principle"><Clock3/><span>资源数据为实时读取并在 Gateway 内存缓存 30 秒；点击刷新资源可立即重新读取。</span></div>
        </template>

        <template v-else-if="tab==='integrations'">
          <span>INTEGRATION GUIDE</span><h2>连接器使用说明</h2>
          <div class="integration-guide"><Box/><div><strong>Harbor</strong><span>同步镜像、版本和安全扫描结果，作为发布制品来源。</span></div></div>
          <div class="integration-guide"><Rocket/><div><strong>Argo CD</strong><span>读取应用同步状态，并把部署结果回写到发布流水线。</span></div></div>
          <div class="integration-guide"><GitCommit/><div><strong>GitLab</strong><span>关联提交、分支和 Pipeline，形成代码到发布的完整链路。</span></div></div>
          <div class="integration-principle"><ShieldCheck/><span>连接检测只验证地址、认证和读取权限，不会修改外部系统。凭据仅保存在 Gateway Secret 中，前端不返回敏感信息。</span></div>
        </template>

        <template v-else-if="selected">
          <span>RELEASE DETAIL</span><h2>{{selected.application}} {{selected.version}}</h2>
          <div class="release-gate" :data-status="selected.status"><ShieldAlert v-if="['awaiting_approval','rollback_awaiting_approval'].includes(selected.status)"/><ShieldCheck v-else/><div><strong>{{gateTitle(selected)}}</strong><span>{{gateDetail(selected)}}</span></div></div>
          <dl><div><dt>目标环境</dt><dd>{{selected.environment}}</dd></div><div><dt>发布策略</dt><dd>{{selected.strategy}}</dd></div><div><dt>关联工单</dt><dd>{{selected.ticketId||'—'}}</dd></div><div><dt>回滚版本</dt><dd>{{selected.previousVersion}}</dd></div><div><dt>事件来源</dt><dd>{{selected.source||'manual'}}</dd></div><div v-if="selected.externalId"><dt>外部发布 ID</dt><dd>{{selected.externalId}}</dd></div><div v-if="selected.approvedBy"><dt>审批人</dt><dd>{{selected.approvedBy}} · {{selected.approvedAt}}</dd></div><div v-if="selected.authorizationExpiresAt"><dt>授权有效期</dt><dd>{{selected.authorizationExpiresAt}}</dd></div></dl>
          <h3>阶段日志</h3><div class="stage-logs"><article v-for="stage in selected.stages" :key="stage.id" :data-status="stage.status"><i></i><div><strong>{{stage.name}}</strong><span>{{stage.message}} {{stage.duration}}</span></div></article></div>
          <section v-if="selected.status==='awaiting_approval' || selected.status==='rollback_awaiting_approval'" class="release-approval">
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
