<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Check, ChevronRight, Clock3, FilePlus2, GitPullRequest, RefreshCw, ShieldAlert, UserRound, X } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'
import { priorityLabel, statusLabel, ticketTypeLabel } from '@/features/tickets/ticketsModel'
import { assignTicket, commentTicket, createTicket, decideTicket, fetchTickets, type Ticket, type TicketInput, type TicketSummary } from '@/api/tickets'

const auth = useAuthStore()
const tickets = ref<Ticket[]>([])
const summary = ref<TicketSummary>({ total: 0, pending: 0, processing: 0, approvedToday: 0, slaWarning: 0 })
const selected = ref<Ticket>()
const filter = ref('all')
const search = ref('')
const loading = ref(false)
const saving = ref(false)
const showCreate = ref(false)
const decisionComment = ref('')
const newComment = ref('')
const assigneeDraft = ref('')
const taskAuthorizationToken = ref('')
const errorMessage = ref('')

const emptyForm = (): TicketInput => ({ title: '', type: 'change', priority: 'medium', description: '', relatedAsset: '', relatedAlert: '', relatedTask: '', relatedRelease: '', projectGroup: '', automationTemplate: '' })
const form = reactive<TicketInput>(emptyForm())

const visible = computed(() => tickets.value.filter(item => {
  const matchesStatus = filter.value === 'all' || item.status === filter.value
  const keyword = search.value.trim().toLowerCase()
  const matchesSearch = !keyword || [item.id, item.title, item.applicant, item.assignee, item.relatedAsset, item.relatedAlert, item.projectGroup]
    .some(value => (value || '').toLowerCase().includes(keyword))
  return matchesStatus && matchesSearch
}))

async function load(preferredId?: string) {
  loading.value = true
  errorMessage.value = ''
  try {
    const [nextSummary, items] = await fetchTickets(auth.token)
    summary.value = nextSummary
    tickets.value = items
    const targetId = preferredId || selected.value?.id
    if (targetId) selected.value = items.find(item => item.id === targetId)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '工单加载失败'
  } finally {
    loading.value = false
  }
}

function choose(item: Ticket) {
  selected.value = item
  taskAuthorizationToken.value = ''
  decisionComment.value = ''
  newComment.value = ''
  assigneeDraft.value = item.assignee
}

function openCreate() {
  Object.assign(form, emptyForm())
  showCreate.value = true
}

async function submitCreate() {
  if (!form.title.trim()) {
    errorMessage.value = '请输入工单标题'
    return
  }
  saving.value = true
  try {
    const item = await createTicket(auth.token, { ...form })
    showCreate.value = false
    await load(item.id)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '工单创建失败'
  } finally {
    saving.value = false
  }
}

async function decide(action: 'approve' | 'reject' | 'close' | 'cancel') {
  if (!selected.value) return
  saving.value = true
  try {
    const item = await decideTicket(auth.token, selected.value.id, action, decisionComment.value)
    if (action === 'approve' && item.taskAuthorizationToken) taskAuthorizationToken.value = item.taskAuthorizationToken
    await load(item.id)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '工单操作失败'
  } finally {
    saving.value = false
  }
}

function copyTaskAuthorization() { if (taskAuthorizationToken.value) void navigator.clipboard.writeText(taskAuthorizationToken.value) }
async function addComment() {
  if (!selected.value || !newComment.value.trim()) return
  saving.value = true
  try {
    const item = await commentTicket(auth.token, selected.value.id, newComment.value)
    newComment.value = ''
    await load(item.id)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '备注提交失败'
  } finally {
    saving.value = false
  }
}

async function updateAssignee() {
  if (!selected.value || !assigneeDraft.value.trim()) return
  saving.value = true
  try {
    const item = await assignTicket(auth.token, selected.value.id, assigneeDraft.value)
    await load(item.id)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '分派失败'
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="tickets-page">
    <header class="tickets-heading">
      <div>
        <span>SERVICE REQUEST &amp; APPROVAL</span>
        <h1>工单与审批中心</h1>
        <p>资源申请、告警转单、变更审批与自动化交付闭环</p>
      </div>
      <div>
        <input v-model="search" class="ticket-search" placeholder="搜索编号、资产、告警或负责人" />
        <button :disabled="loading" @click="load()"><RefreshCw :class="{ spin: loading }" />刷新</button>
        <button class="primary" @click="openCreate"><FilePlus2 />新建工单</button>
      </div>
    </header>

    <div v-if="errorMessage" class="ticket-error">{{ errorMessage }}<button @click="errorMessage=''"><X /></button></div>

    <div class="tickets-summary">
      <article><GitPullRequest /><span>全部工单<strong>{{ summary.total }}</strong></span></article>
      <article><Clock3 /><span>待审批<strong>{{ summary.pending }}</strong></span></article>
      <article><RefreshCw /><span>处理中<strong>{{ summary.processing }}</strong></span></article>
      <article><Check /><span>今日通过<strong>{{ summary.approvedToday }}</strong></span></article>
      <article><ShieldAlert /><span>SLA 预警<strong>{{ summary.slaWarning }}</strong></span></article>
    </div>

    <div class="ticket-workspace">
      <aside class="ticket-filters">
        <h3>工作台</h3>
        <button v-for="item in [{id:'all',name:'全部工单'},{id:'pending',name:'待审批'},{id:'approved',name:'待执行'},{id:'completed',name:'已完成'},{id:'rejected',name:'已驳回'},{id:'cancelled',name:'已取消'}]" :key="item.id" :class="{ active: filter === item.id }" @click="filter = item.id">
          {{ item.name }}<ChevronRight />
        </button>
        <h3>工单类型</h3>
        <span v-for="item in ['incident','change','resource','release','access','inspection']" :key="item">{{ ticketTypeLabel(item) }}</span>
      </aside>

      <main class="ticket-list">
        <header><span>工单信息</span><span>优先级</span><span>状态</span><span>处理人 / SLA</span></header>
        <button v-for="item in visible" :key="item.id" :class="{ selected: selected?.id === item.id }" @click="choose(item)">
          <div>
            <small>{{ item.id }} · {{ ticketTypeLabel(item.type) }}</small>
            <strong>{{ item.title }}</strong>
            <span>{{ item.applicant }} · {{ item.createdAt }}</span>
          </div>
          <b :data-priority="item.priority">{{ priorityLabel(item.priority) }}</b>
          <em :data-status="item.status">{{ statusLabel(item.status) }}</em>
          <div><strong>{{ item.assignee }}</strong><span :class="{ overdue: item.sla.includes('超时') }">{{ item.sla }}</span></div>
        </button>
        <div v-if="!visible.length" class="empty-ticket"><GitPullRequest /><strong>暂无工单</strong><span>可按状态或关键字筛选</span></div>
      </main>

      <aside class="ticket-detail">
        <template v-if="selected">
          <header>
            <div><span>APPROVAL FLOW</span><h2>{{ selected.title }}</h2><small>{{ selected.id }}</small></div>
            <button @click="selected = undefined"><X /></button>
          </header>
          <p>{{ selected.description || '暂无详细说明' }}</p>
          <div class="ticket-context">
            <span>关联资产<b>{{ selected.relatedAsset || '—' }}</b></span>
            <span>关联告警<b>{{ selected.relatedAlert || '—' }}</b></span>
            <span>项目组<b>{{ selected.projectGroup || '—' }}</b></span>
            <span>变更风险<b :data-risk="selected.risk">{{ selected.risk }}</b></span><span v-if="selected.escalationLevel">SLA升级<b>第 {{ selected.escalationLevel }} 级 · {{ selected.escalatedAt }}</b></span>
          </div>

          <div class="assignee-row">
            <UserRound />
            <input v-model="assigneeDraft" placeholder="处理人" />
            <button :disabled="saving || !['pending','approved'].includes(selected.status)" @click="updateAssignee">分派</button>
          </div>

          <h3>审批流程</h3>
          <div class="approval-steps">
            <article v-for="step in selected.steps" :key="step.id" :data-status="step.status">
              <i><Check v-if="step.status === 'completed'" /></i>
              <div><strong>{{ step.name }}</strong><span>{{ step.assignee }} {{ step.completedAt }}</span></div>
            </article>
          </div>

          <h3>处理记录</h3>
          <div class="ticket-timeline">
            <article v-for="event in selected.timeline" :key="`${event.time}-${event.action}-${event.comment}`">
              <time>{{ event.time }}</time>
              <div><strong>{{ event.actor }} · {{ event.action }}</strong><span>{{ event.comment }}</span></div>
            </article>
          </div>

          <div class="ticket-comment">
            <textarea v-model="newComment" rows="2" placeholder="添加处理备注，不改变工单状态" />
            <button :disabled="saving || !newComment.trim()" @click="addComment">提交备注</button>
          </div>

          <template v-if="['pending','approved'].includes(selected.status)">
            <textarea v-model="decisionComment" class="decision-comment" rows="2" placeholder="审批或关闭意见（可选）" />
            <footer>
              <button v-if="selected.status === 'pending'" @click="decide('reject')">驳回</button>
              <button v-if="selected.status === 'pending'" class="primary" @click="decide('approve')">审批通过</button>
              <button v-if="selected.status === 'approved'" class="primary" @click="decide('close')">完成工单</button>
            </footer>
          </template>
          <section v-if="selected.automationTemplate && selected.status === 'approved'" class="ticket-task-authorization"><div><strong>一次性任务执行授权</strong><small>{{taskAuthorizationToken?`有效期至 ${selected.taskAuthorizationExpiresAt}`:'授权码只在审批响应中展示一次，请重新审批获取'}}</small></div><code v-if="taskAuthorizationToken">{{taskAuthorizationToken}}</code><button v-if="taskAuthorizationToken" @click="copyTaskAuthorization">复制授权码</button><a href="/jobs">进入自动化任务中心 →</a></section><a v-else-if="selected.automationTemplate" href="/jobs">使用 {{ selected.automationTemplate }} 进入自动化任务中心 →</a>
        </template>
        <div v-else class="empty-ticket"><GitPullRequest /><strong>选择工单</strong><span>查看审批流、关联资产与处理记录</span></div>
      </aside>
    </div>

    <div v-if="showCreate" class="ticket-modal" @click.self="showCreate = false">
      <form @submit.prevent="submitCreate">
        <header><div><span>NEW SERVICE REQUEST</span><h2>新建工单</h2></div><button type="button" @click="showCreate = false"><X /></button></header>
        <div class="ticket-form-grid">
          <label class="wide">标题<input v-model="form.title" required placeholder="例如：生产订单服务扩容" /></label>
          <label>类型<select v-model="form.type"><option value="incident">故障工单</option><option value="change">变更工单</option><option value="resource">资源申请</option><option value="release">发布工单</option><option value="access">权限申请</option><option value="inspection">巡检工单</option></select></label>
          <label>优先级<select v-model="form.priority"><option value="critical">紧急</option><option value="high">高</option><option value="medium">中</option><option value="low">低</option></select></label>
          <label>项目组<input v-model="form.projectGroup" placeholder="例如：订单平台" /></label>
          <label>关联资产<input v-model="form.relatedAsset" placeholder="资产 ID、IP 或服务名" /></label>
          <label>关联告警<input v-model="form.relatedAlert" placeholder="Alertmanager fingerprint" /></label>
          <label>自动化模板<input v-model="form.automationTemplate" placeholder="例如：tpl-restart" /></label>
          <label class="wide">申请说明<textarea v-model="form.description" rows="4" placeholder="描述申请原因、影响范围和期望时间" /></label>
        </div>
        <footer><button type="button" @click="showCreate = false">取消</button><button class="primary" :disabled="saving">提交申请</button></footer>
      </form>
    </div>
  </section>
</template>