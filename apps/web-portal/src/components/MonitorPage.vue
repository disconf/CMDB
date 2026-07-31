<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { Activity, CheckCircle2, Clock3, ExternalLink, RefreshCw, ShieldCheck, TriangleAlert } from 'lucide-vue-next'
import { assignAlert, bulkDeadLetters, bulkUpdateAlerts, createEscalationPolicy, createNotificationChannel, createRoutingRule, createSilence, deleteDeadLetter, deleteEscalationPolicy, deleteNotificationChannel, deleteRoutingRule, expireSilence, fetchAlertEvents, fetchAlertRules, fetchDeliveryStats, fetchEscalationPolicies, fetchMonitorData, fetchNotificationChannels, fetchNotificationDeadLetters, fetchNotificationDeliveries, fetchNotificationQueueStatus, fetchRoutingRules, fetchSilences, replayDeadLetter, silenceAlert, testNotificationChannel, toggleEscalationPolicy, toggleNotificationChannel, toggleRoutingRule, updateAlert, updateNotificationTemplate, type AlertEvent, type AlertRule, type AlertSilence, type DeliveryStats, type EscalationPolicy, type MonitorAlert, type MonitorCoverage, type MonitorMetric, type MonitorSummary, type NotificationChannel, type NotificationDeadLetter, type NotificationDelivery, type NotificationQueueStatus, type RoutingRule } from '@/api/monitor'
import { useAuthStore } from '@/stores/useAuthStore'
import { metricDirection, metricTone, severityLabel, trendPoints } from '@/features/monitor/monitorModel'
import MonitoringCoverage from './MonitoringCoverage.vue'

const auth = useAuthStore()
const summary = ref<MonitorSummary>()
const metrics = ref<MonitorMetric[]>([])
const alerts = ref<MonitorAlert[]>([])
const coverage = ref<MonitorCoverage>({available:false,total:0,monitored:0,healthy:0,down:0,missing:0,percentage:0,hosts:[]})
const selected = ref<MonitorAlert>()
const events = ref<AlertEvent[]>([])
const selectedIds = ref<string[]>([])
const assignee = ref('')
const rules = ref<AlertRule[]>([])
const showRules = ref(false)
const rulesError = ref('')
const silences = ref<AlertSilence[]>([])
const showSilences = ref(false)
const silenceError = ref('')
const silenceForm = ref({ matcherName: 'instance', matcherValue: '', hours: 2, comment: '' })
const routes = ref<RoutingRule[]>([])
const showRoutes = ref(false)
const routeForm = ref({ name: '', matcherName: 'severity', matcherValue: 'critical', team: '', owner: '', channelId: '' })
const channels = ref<NotificationChannel[]>([])
const showChannels = ref(false)
const channelForm = ref({name:'',endpoint:'',template:'[{{severity}}] {{title}}\n对象: {{target}}\n状态: {{status}}\n负责人: {{owner}}\n详情: {{message}}'})
const deliveries = ref<NotificationDelivery[]>([])
const deliveryStats = ref<DeliveryStats>({total:0,successful:0,failed:0,successRate:0})
const queueStatus = ref<NotificationQueueStatus>({enabled:false,consumer:'',pendingOutbox:0,deadLetters:0,lastConsumedAt:''})
const deadLetters = ref<NotificationDeadLetter[]>([])
const selectedDeadLetters = ref<string[]>([])
const escalations=ref<EscalationPolicy[]>([])
const showEscalations=ref(false)
const escalationForm=ref({name:'',severity:'critical',timeoutMinutes:15,team:'',owner:'',channelId:''})
const loading = ref(false)
const error = ref('')
const lastUpdated = ref('')
let timer: ReturnType<typeof setInterval> | undefined

async function load() {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try {
    const data = await fetchMonitorData(auth.token)
    summary.value = data.summary
    metrics.value = data.metrics
    alerts.value = data.alerts
    coverage.value = data.coverage
	try { rules.value = await fetchAlertRules(auth.token); rulesError.value = '' }
	catch { rulesError.value = 'Prometheus 规则暂时不可用' }
	try { silences.value = await fetchSilences(auth.token); silenceError.value = '' }
	catch { silenceError.value = 'Alertmanager 静默服务暂时不可用' }
	routes.value = await fetchRoutingRules(auth.token)
	;[channels.value, deliveries.value, deliveryStats.value, queueStatus.value, deadLetters.value] = await Promise.all([fetchNotificationChannels(auth.token),fetchNotificationDeliveries(auth.token),fetchDeliveryStats(auth.token),fetchNotificationQueueStatus(auth.token),fetchNotificationDeadLetters(auth.token)])
	escalations.value=await fetchEscalationPolicies(auth.token)
    lastUpdated.value = new Date().toLocaleTimeString('zh-CN', { hour12: false })
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '监控数据加载失败'
  } finally { loading.value = false }
}

async function action(type: 'acknowledge' | 'resolve') {
  if (!selected.value) return
  try {
    await updateAlert(auth.token, selected.value.id, type)
    const selectedId = selected.value.id
    await load()
    selected.value = alerts.value.find((alert) => alert.id === selectedId)
    events.value = await fetchAlertEvents(auth.token, selectedId)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '告警操作失败'
  }
}

async function selectAlert(alert: MonitorAlert) {
  selected.value = alert
  try { events.value = await fetchAlertEvents(auth.token, alert.id) }
  catch { events.value = [] }
}

async function assign() {
  if (!selected.value || !assignee.value.trim()) return
  try { await assignAlert(auth.token, selected.value.id, assignee.value); await selectAlert({ ...selected.value, owner: assignee.value }); await load(); assignee.value = '' }
  catch (reason) { error.value = reason instanceof Error ? reason.message : '分派失败' }
}

async function silence() {
  if (!selected.value) return
  try { await silenceAlert(auth.token, selected.value.id); await load(); selected.value = alerts.value.find((alert) => alert.id === selected.value?.id); if (selected.value) await selectAlert(selected.value) }
  catch (reason) { error.value = reason instanceof Error ? reason.message : '静默失败' }
}

async function bulkAction(type: 'acknowledge' | 'resolve') {
  if (!selectedIds.value.length) return
  try { const result = await bulkUpdateAlerts(auth.token, selectedIds.value, type); selectedIds.value = result.failed; await load(); if (result.failed.length) error.value = `${result.failed.length} 条告警处理失败` }
  catch (reason) { error.value = reason instanceof Error ? reason.message : '批量操作失败' }
}

async function submitSilence() {
  const form = silenceForm.value
  if (!form.matcherName.trim() || !form.matcherValue.trim() || !form.comment.trim() || form.hours <= 0) return
  try { await createSilence(auth.token, form.matcherName, form.matcherValue, form.hours, form.comment); silences.value = await fetchSilences(auth.token); form.matcherValue = ''; form.comment = ''; silenceError.value = '' }
  catch (reason) { silenceError.value = reason instanceof Error ? reason.message : '创建静默失败' }
}

async function removeSilence(id: string) {
  try { await expireSilence(auth.token, id); silences.value = await fetchSilences(auth.token) }
  catch (reason) { silenceError.value = reason instanceof Error ? reason.message : '静默失效操作失败' }
}

async function submitRoute() {
  try { await createRoutingRule(auth.token, routeForm.value); routes.value = await fetchRoutingRules(auth.token); routeForm.value = { name:'', matcherName:'severity', matcherValue:'critical', team:'', owner:'', channelId:'' } }
  catch (reason) { error.value = reason instanceof Error ? reason.message : '创建路由失败' }
}
async function toggleRoute(id: string) { try { await toggleRoutingRule(auth.token,id); routes.value=await fetchRoutingRules(auth.token) } catch { error.value='路由状态更新失败' } }
async function removeRoute(id: string) { try { await deleteRoutingRule(auth.token,id); routes.value=await fetchRoutingRules(auth.token) } catch { error.value='删除路由失败' } }
async function submitChannel(){try{await createNotificationChannel(auth.token,channelForm.value.name,channelForm.value.endpoint,channelForm.value.template);channels.value=await fetchNotificationChannels(auth.token);channelForm.value={name:'',endpoint:'',template:'[{{severity}}] {{title}}\n对象: {{target}}\n状态: {{status}}\n负责人: {{owner}}\n详情: {{message}}'}}catch(reason){error.value=reason instanceof Error?reason.message:'创建渠道失败'}}
async function toggleChannel(id:string){try{await toggleNotificationChannel(auth.token,id);channels.value=await fetchNotificationChannels(auth.token)}catch{error.value='渠道状态更新失败'}}
async function testChannel(id:string){try{await testNotificationChannel(auth.token,id);[channels.value,deliveries.value,deliveryStats.value]=await Promise.all([fetchNotificationChannels(auth.token),fetchNotificationDeliveries(auth.token),fetchDeliveryStats(auth.token)])}catch(reason){error.value=reason instanceof Error?reason.message:'渠道测试失败'}}
async function removeChannel(id:string){try{await deleteNotificationChannel(auth.token,id);channels.value=await fetchNotificationChannels(auth.token)}catch{error.value='删除渠道失败'}}
async function saveTemplate(channel:NotificationChannel){try{await updateNotificationTemplate(auth.token,channel.id,channel.template);channels.value=await fetchNotificationChannels(auth.token)}catch(reason){error.value=reason instanceof Error?reason.message:'模板保存失败'}}
async function refreshQueue(){[queueStatus.value,deadLetters.value]=await Promise.all([fetchNotificationQueueStatus(auth.token),fetchNotificationDeadLetters(auth.token)])}
async function replayDLQ(id:string){try{await replayDeadLetter(auth.token,id);await refreshQueue()}catch{error.value='死信重放失败'}}
async function deleteDLQ(id:string){try{await deleteDeadLetter(auth.token,id);await refreshQueue()}catch{error.value='死信清理失败'}}
async function bulkDLQ(action:'replay'|'delete'){if(!selectedDeadLetters.value.length)return;try{const result=await bulkDeadLetters(auth.token,selectedDeadLetters.value,action);selectedDeadLetters.value=result.failed;await refreshQueue();if(result.failed.length)error.value=`${result.failed.length} 条死信处理失败`}catch{error.value='批量死信操作失败'}}
async function submitEscalation(){try{await createEscalationPolicy(auth.token,escalationForm.value);escalations.value=await fetchEscalationPolicies(auth.token);escalationForm.value={name:'',severity:'critical',timeoutMinutes:15,team:'',owner:'',channelId:''}}catch(reason){error.value=reason instanceof Error?reason.message:'创建升级策略失败'}}
async function toggleEscalation(id:string){try{await toggleEscalationPolicy(auth.token,id);escalations.value=await fetchEscalationPolicies(auth.token)}catch{error.value='升级策略状态更新失败'}}
async function removeEscalation(id:string){try{await deleteEscalationPolicy(auth.token,id);escalations.value=await fetchEscalationPolicies(auth.token)}catch{error.value='删除升级策略失败'}}

onMounted(() => { void load(); timer = setInterval(load, 30_000) })
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <section class="monitor-page">
    <header class="monitor-heading">
      <div><span>OBSERVABILITY & ALERT OPERATIONS</span><h1>监控告警中心</h1><p>实时指标、事件关联与告警闭环处置</p></div>
      <div class="monitor-actions"><small v-if="lastUpdated">更新于 {{ lastUpdated }}</small><button @click="showRules=!showRules;showSilences=false;showRoutes=false;showChannels=false;showEscalations=false">{{showRules?'返回告警':'规则中心'}}</button><button @click="showSilences=!showSilences;showRules=false;showRoutes=false;showChannels=false;showEscalations=false">{{showSilences?'返回告警':'静默策略'}}</button><button @click="showRoutes=!showRoutes;showRules=false;showSilences=false;showChannels=false;showEscalations=false">{{showRoutes?'返回告警':'分派路由'}}</button><button @click="showChannels=!showChannels;showRules=false;showSilences=false;showRoutes=false;showEscalations=false">{{showChannels?'返回告警':'通知渠道'}}</button><button @click="showEscalations=!showEscalations;showRules=false;showSilences=false;showRoutes=false;showChannels=false">{{showEscalations?'返回告警':'升级策略'}}</button><button :disabled="loading" @click="load"><RefreshCw :class="{ spin: loading }" />刷新数据</button></div>
    </header>
    <div v-if="error" class="monitor-error"><TriangleAlert />{{ error }}<button @click="load">重试</button></div>
    <div class="monitor-summary">
      <article><Activity/><div><span>监控目标</span><strong>{{summary?.targets ?? '--'}}</strong><small>{{summary?.healthy ?? '--'}} 个健康</small></div></article>
      <article><TriangleAlert/><div><span>活动告警</span><strong>{{summary?.active ?? '--'}}</strong><small>{{summary?.critical ?? '--'}} 个严重</small></div></article>
      <article><CheckCircle2/><div><span>已确认</span><strong>{{summary?.acknowledged ?? '--'}}</strong><small>处理中事件</small></div></article>
      <article><ShieldCheck/><div><span>可用性</span><strong>{{summary ? summary.availability + '%' : '--'}}</strong><small>当前采集目标</small></div></article>
    </div>
    <MonitoringCoverage :coverage="coverage"/>
    <section v-if="showRules" class="rule-center"><header><div><span>PROMETHEUS ALERT RULES</span><h2>告警规则中心</h2></div><b>{{rules.length}} 条规则 · {{rules.filter(rule=>rule.state==='firing').length}} 条触发中</b></header><div v-if="rulesError" class="rule-error">{{rulesError}}</div><div class="rule-table"><div class="rule-head"><span>规则 / 规则组</span><span>级别</span><span>持续时间</span><span>健康</span><span>状态</span></div><article v-for="rule in rules" :key="`${rule.group}-${rule.name}`"><div><strong>{{rule.name}}</strong><small>{{rule.group}}</small><code>{{rule.query}}</code><em v-if="rule.lastError">{{rule.lastError}}</em></div><b :data-severity="rule.severity">{{rule.severity || 'warning'}}</b><span>{{rule.duration}} 秒</span><span :data-health="rule.health">{{rule.health}}</span><span :data-state="rule.state">{{rule.state==='firing'?`${rule.firingCount} 个触发`:'正常'}}</span></article><div v-if="!rules.length && !rulesError" class="rule-empty">Prometheus 当前没有加载告警规则</div></div></section>
    <section v-else-if="showSilences" class="silence-center"><header><div><span>ALERTMANAGER SILENCES</span><h2>静默策略中心</h2></div><b>{{silences.filter(item=>item.status.state==='active').length}} 条生效中</b></header><div class="silence-create"><input v-model="silenceForm.matcherName" placeholder="标签名，例如 instance"><input v-model="silenceForm.matcherValue" placeholder="标签值，例如 host-01"><input v-model.number="silenceForm.hours" type="number" min="1" max="720" title="静默小时数"><input v-model="silenceForm.comment" placeholder="静默原因（必填）"><button @click="submitSilence">创建静默</button></div><div v-if="silenceError" class="rule-error">{{silenceError}}</div><div class="silence-list"><article v-for="item in silences" :key="item.id"><div><strong>{{item.matchers.map(m=>`${m.name}${m.isRegex?'=~':'='}${m.value}`).join(', ')}}</strong><span>{{item.comment}}</span></div><div><small>创建人</small><b>{{item.createdBy}}</b></div><div><small>失效时间</small><b>{{new Date(item.endsAt).toLocaleString('zh-CN',{hour12:false})}}</b></div><em :data-state="item.status.state">{{item.status.state}}</em><button v-if="item.status.state==='active' || item.status.state==='pending'" @click="removeSilence(item.id)">立即失效</button></article><div v-if="!silences.length && !silenceError" class="rule-empty">当前没有静默策略</div></div></section>
    <section v-else-if="showRoutes" class="routing-center"><header><div><span>ON-CALL ROUTING</span><h2>值班分派路由</h2></div><b>{{routes.filter(route=>route.enabled).length}} 条启用</b></header><div class="route-create"><input v-model="routeForm.name" placeholder="规则名称"><input v-model="routeForm.matcherName" placeholder="标签名"><input v-model="routeForm.matcherValue" placeholder="标签值"><input v-model="routeForm.team" placeholder="值班组"><input v-model="routeForm.owner" placeholder="负责人账号"><select v-model="routeForm.channelId"><option value="">全部启用渠道</option><option v-for="channel in channels" :key="channel.id" :value="channel.id">{{channel.name}}</option></select><button @click="submitRoute">新增路由</button></div><div class="route-table"><article v-for="route in routes" :key="route.id" :class="{disabled:!route.enabled}"><div><strong>{{route.name}}</strong><code>{{route.matcherName}}={{route.matcherValue}}</code></div><div><small>值班组</small><b>{{route.team}}</b></div><div><small>负责人</small><b>{{route.owner}}</b></div><div><small>通知渠道</small><b>{{channels.find(channel=>channel.id===route.channelId)?.name||'全部渠道'}}</b></div><em>{{route.enabled?'已启用':'已停用'}}</em><button @click="toggleRoute(route.id)">{{route.enabled?'停用':'启用'}}</button><button @click="removeRoute(route.id)">删除</button></article><div v-if="!routes.length" class="rule-empty">暂无自动分派路由</div></div></section>
    <section v-else-if="showChannels" class="channel-center"><header><div><span>OUTBOUND WEBHOOKS</span><h2>通知渠道</h2></div><b>{{channels.filter(channel=>channel.enabled).length}} 个已启用</b></header><div class="channel-create"><input v-model="channelForm.name" placeholder="渠道名称"><input v-model="channelForm.endpoint" placeholder="Webhook URL"><button @click="submitChannel">新增渠道</button></div><div class="channel-list"><article v-for="channel in channels" :key="channel.id" :class="{disabled:!channel.enabled}"><div><strong>{{channel.name}}</strong><code>{{channel.displayUrl}}</code><small v-if="channel.lastError">{{channel.lastError}}</small></div><span :data-status="channel.lastStatus">{{channel.lastStatus||'未测试'}}</span><time>{{channel.lastSentAt||'--'}}</time><button @click="testChannel(channel.id)">测试</button><button @click="toggleChannel(channel.id)">{{channel.enabled?'停用':'启用'}}</button><button @click="removeChannel(channel.id)">删除</button></article><div v-if="!channels.length" class="rule-empty">暂无通知渠道</div></div></section>
    <section v-if="showChannels" class="delivery-panel"><div class="delivery-stats"><article><span>投递尝试</span><strong>{{deliveryStats.total}}</strong></article><article><span>成功</span><strong>{{deliveryStats.successful}}</strong></article><article><span>失败</span><strong>{{deliveryStats.failed}}</strong></article><article><span>成功率</span><strong>{{deliveryStats.successRate.toFixed(1)}}%</strong></article></div><h3>最近投递记录</h3><div class="delivery-list"><article v-for="item in deliveries.slice(0,10)" :key="item.id"><span :data-status="item.status">{{item.status}}</span><div><strong>{{item.alertTitle||'渠道测试'}}</strong><small>{{item.channelName}}</small></div><b>第 {{item.attempt}} 次</b><code>{{item.httpStatus||'--'}}</code><em>{{item.error||'投递成功'}}</em><time>{{item.createdAt}}</time></article><p v-if="!deliveries.length">暂无投递记录</p></div></section>
    <section v-if="showChannels" class="queue-panel"><header><h3>Kafka 通知队列</h3><span :data-status="queueStatus.consumer">{{queueStatus.enabled?(queueStatus.consumer||'等待启动'):'本地降级模式'}}</span></header><div class="queue-stats"><article><span>Outbox 待发布</span><strong>{{queueStatus.pendingOutbox}}</strong></article><article><span>死信消息</span><strong>{{queueStatus.deadLetters}}</strong></article><article><span>最近消费</span><strong>{{queueStatus.lastConsumedAt||'--'}}</strong></article><article><span>消费 Topic</span><strong>monitor.notification.requested.v1</strong></article></div><h3 v-if="deadLetters.length">最近死信</h3><div class="dead-letter-list"><article v-for="item in deadLetters.slice(0,5)" :key="item.id"><strong>{{item.alertId||item.eventId}}</strong><span>{{item.error}}</span><b>{{item.attempts}} 轮</b><time>{{item.createdAt}}</time></article></div></section>
    <section v-if="showChannels&&deadLetters.length" class="dlq-ops"><header><h3>死信运维</h3><div><span>已选择 {{selectedDeadLetters.length}} 条</span><button :disabled="!selectedDeadLetters.length" @click="bulkDLQ('replay')">批量重放</button><button :disabled="!selectedDeadLetters.length" @click="bulkDLQ('delete')">批量清理</button></div></header><article v-for="item in deadLetters" :key="item.id"><input v-model="selectedDeadLetters" type="checkbox" :value="item.id"><div><strong>{{item.alertTitle||item.alertId||item.eventId}}</strong><small>{{item.eventId}}</small></div><span>{{channels.find(channel=>channel.id===item.channelId)?.name||item.channelId||'全部渠道'}}</span><em>{{item.error}}</em><b>{{item.attempts}} 轮</b><time>{{item.createdAt}}</time><button @click="replayDLQ(item.id)">重放</button><button @click="deleteDLQ(item.id)">清理</button></article></section>
    <section v-if="showChannels" class="template-center"><header><h3>通知模板</h3><span>支持变量：title · severity · target · owner · rule · status · message</span></header><article v-for="channel in channels" :key="`template-${channel.id}`"><div><strong>{{channel.name}}</strong><small>{{channel.displayUrl}}</small></div><textarea v-model="channel.template" maxlength="4000" rows="4"></textarea><button @click="saveTemplate(channel)">保存模板</button></article></section>
    <section v-if="showEscalations" class="escalation-center"><header><div><span>AUTOMATIC ESCALATION</span><h2>告警升级策略</h2></div><b>{{escalations.filter(item=>item.enabled).length}} 条启用</b></header><div class="escalation-create"><input v-model="escalationForm.name" placeholder="策略名称"><select v-model="escalationForm.severity"><option value="critical">严重告警</option><option value="warning">警告告警</option></select><label><input v-model.number="escalationForm.timeoutMinutes" type="number" min="1" max="10080"> 分钟未确认</label><input v-model="escalationForm.team" placeholder="升级至值班组"><input v-model="escalationForm.owner" placeholder="升级负责人账号"><select v-model="escalationForm.channelId"><option value="">全部启用渠道</option><option v-for="channel in channels" :key="channel.id" :value="channel.id">{{channel.name}}</option></select><button @click="submitEscalation">新增策略</button></div><div class="escalation-list"><article v-for="item in escalations" :key="item.id" :class="{disabled:!item.enabled}"><div><strong>{{item.name}}</strong><small>{{item.severity}} · {{item.timeoutMinutes}} 分钟未确认</small></div><div><small>升级值班组</small><b>{{item.team}}</b></div><div><small>负责人</small><b>{{item.owner}}</b></div><div><small>通知渠道</small><b>{{channels.find(channel=>channel.id===item.channelId)?.name||'全部渠道'}}</b></div><em>{{item.enabled?'自动执行中':'已停用'}}</em><button @click="toggleEscalation(item.id)">{{item.enabled?'停用':'启用'}}</button><button @click="removeEscalation(item.id)">删除</button></article><div v-if="!escalations.length" class="rule-empty">暂无自动升级策略</div></div></section>
    <div v-if="!showRules&&!showSilences&&!showRoutes&&!showChannels&&!showEscalations" class="monitor-layout">
      <main>
        <section class="metric-section">
          <header><h2>实时资源指标</h2><span>最近 30 分钟 · 每 30 秒刷新</span></header>
          <div class="metric-grid">
            <article v-for="metric in metrics" :key="metric.id" :data-tone="metricTone(metric.value, metric.warning, metric.critical, metricDirection(metric.id))">
              <header><div><strong>{{metric.name}}</strong><span>{{metric.target}}</span></div><b>{{metric.value}}<small>{{metric.unit}}</small></b></header>
              <svg viewBox="0 0 200 55" preserveAspectRatio="none"><polyline :points="trendPoints(metric.trend)"/></svg>
              <footer><span>警告 {{metric.warning}}{{metric.unit}}</span><span>严重 {{metric.critical}}{{metric.unit}}</span></footer>
            </article>
            <div v-if="!loading && !metrics.length" class="metric-empty">Prometheus 暂无可展示的实时指标</div>
          </div>
        </section>
        <section class="alert-section">
          <header><h2>告警事件</h2><div class="bulk-actions"><span>{{selectedIds.length ? `已选择 ${selectedIds.length} 条` : `${alerts.filter(a=>a.status!=='resolved').length} 个活动事件`}}</span><button :disabled="!selectedIds.length" @click="bulkAction('acknowledge')">批量确认</button><button :disabled="!selectedIds.length" @click="bulkAction('resolve')">批量恢复</button></div></header>
          <button v-for="alert in alerts" :key="alert.id" class="alert-row" :class="{selected:selected?.id===alert.id}" @click="selectAlert(alert)"><input v-model="selectedIds" type="checkbox" :value="alert.id" aria-label="选择告警" @click.stop><b :data-severity="alert.severity">{{severityLabel(alert.severity)}}</b><div><strong>{{alert.title}}</strong><span>{{alert.target}} · {{alert.rule}}</span></div><time><Clock3/>{{alert.duration}}</time><em :data-status="alert.status">{{alert.status==='firing'?'触发中':alert.status==='acknowledged'?'已确认':alert.status==='silenced'?'已静默':'已恢复'}}</em></button>
        </section>
      </main>
      <aside class="alert-detail">
        <template v-if="selected"><span>ALERT DETAIL</span><h2>{{selected.title}}</h2><p>{{selected.message}}</p><dl><div><dt>关联资产</dt><dd>{{selected.target}}</dd></div><div><dt>负责人</dt><dd>{{selected.owner || '待分派'}}</dd></div><div><dt>触发规则</dt><dd>{{selected.rule}}</dd></div><div><dt>开始时间</dt><dd>{{selected.startedAt}}</dd></div></dl><div class="assign-control"><input v-model="assignee" placeholder="输入负责人账号" @keyup.enter="assign"><button :disabled="!assignee.trim()" @click="assign">分派</button></div><section class="event-timeline"><h3>处置时间线 <small>{{events.length}} 条</small></h3><div v-for="event in events" :key="event.id"><i :data-type="event.type"></i><p><strong>{{event.type==='firing'?'首次触发':event.type==='repeated'?'重复通知':event.type==='acknowledged'?'人工确认':event.type==='assigned'?'负责人分派':event.type==='silenced'?'告警静默':'告警恢复'}}</strong><span>{{event.operator}} · {{event.createdAt}}</span><small v-if="event.message">{{event.message}}</small></p></div><p v-if="!events.length" class="event-empty">暂无处置记录</p></section><a :href="`/topology?focus=${selected.targetId}`"><ExternalLink/>查看关联拓扑</a><footer><button v-if="selected.status==='firing'" @click="action('acknowledge')">确认告警</button><button v-if="selected.status!=='resolved' && selected.status!=='silenced'" @click="silence">静默告警</button><button class="primary" :disabled="selected.status==='resolved'" @click="action('resolve')">标记恢复</button></footer></template>
        <div v-else class="empty-alert"><TriangleAlert/><strong>选择告警事件</strong><span>查看详情并执行处置</span></div>
      </aside>
    </div>
  </section>
</template>

<style scoped>
.monitor-actions { display: flex; align-items: center; gap: 12px; color: #668198; }
.monitor-actions small { white-space: nowrap; }
.monitor-error { display: flex; align-items: center; gap: 10px; margin-top: 16px; padding: 10px 12px; border: 1px solid #7c3142; background: #351928; color: #ff9cac; }
.monitor-error svg { width: 17px; }
.monitor-error button { margin-left: auto; padding: 5px 10px; }
.metric-empty { grid-column: 1 / -1; padding: 32px; text-align: center; border: 1px dashed #19415d; color: #668198; }
button:disabled { cursor: not-allowed; opacity: .55; }
.event-timeline { margin-top: 18px; max-height: 260px; overflow: auto; }
.event-timeline h3 { display: flex; justify-content: space-between; font-size: 13px; }
.event-timeline h3 small { color: #668198; font-weight: normal; }
.event-timeline > div { display: flex; gap: 10px; padding: 8px 0; border-bottom: 1px solid #15364e; }
.event-timeline i { width: 8px; height: 8px; margin-top: 5px; border-radius: 50%; background: #21d4ee; flex: none; }
.event-timeline i[data-type='resolved'] { background: #24d2a2; }
.event-timeline i[data-type='acknowledged'] { background: #f2b841; }
.event-timeline p { margin: 0; padding: 0; background: none; }
.event-timeline p span, .event-timeline p small { display: block; margin-top: 3px; color: #668198; font-size: 10px; }
.event-timeline .event-empty { color: #668198; }
.bulk-actions { display: flex; align-items: center; gap: 7px; }
.bulk-actions button { padding: 5px 8px; font-size: 10px; }
.alert-row { grid-template-columns: 24px 58px 1fr 100px 76px; }
.alert-row input { accent-color: #21d4ee; }
.assign-control { display: flex; gap: 7px; margin: 12px 0; }
.assign-control input { min-width: 0; flex: 1; border: 1px solid #22445f; background: #071b2c; color: #dcecff; padding: 8px; outline: none; }
.assign-control input:focus { border-color: #21d4ee; }
.rule-center { margin-top: 18px; border: 1px solid #173b55; background: #081e31; padding: 18px; }
.rule-center > header { display: flex; justify-content: space-between; align-items: center; }
.rule-center > header span { color: #21d4ee; font-size: 10px; letter-spacing: 2px; }
.rule-center > header h2 { margin: 5px 0 14px; }
.rule-center > header b { color: #7894a9; font-size: 12px; }
.rule-error, .rule-empty { padding: 24px; text-align: center; color: #ff9cac; }
.rule-head, .rule-table article { display: grid; grid-template-columns: minmax(360px, 1fr) 90px 90px 90px 110px; gap: 12px; align-items: center; }
.rule-head { padding: 9px 12px; background: #0d293f; color: #668198; font-size: 10px; }
.rule-table article { padding: 12px; border-bottom: 1px solid #15364e; }
.rule-table article strong, .rule-table article small, .rule-table article code, .rule-table article em { display: block; }
.rule-table article small { margin-top: 3px; color: #668198; }
.rule-table article code { max-width: 650px; margin-top: 7px; overflow: hidden; color: #8ab5ca; text-overflow: ellipsis; white-space: nowrap; }
.rule-table article em { margin-top: 4px; color: #ff7e91; font-size: 10px; }
.rule-table b[data-severity='critical'], .rule-table span[data-state='firing'] { color: #fa6078; }
.rule-table span[data-health='ok'] { color: #24d2a2; }
.silence-center { margin-top: 18px; border: 1px solid #173b55; background: #081e31; padding: 18px; }
.silence-center > header { display: flex; justify-content: space-between; align-items: center; }
.silence-center > header span { color: #21d4ee; font-size: 10px; letter-spacing: 2px; }
.silence-center > header h2 { margin: 5px 0 14px; }
.silence-center > header b { color: #7894a9; font-size: 12px; }
.silence-create { display: grid; grid-template-columns: 150px 1fr 90px 1.5fr 100px; gap: 8px; padding: 12px; background: #0d293f; }
.silence-create input { min-width: 0; border: 1px solid #22445f; background: #071b2c; color: #dcecff; padding: 9px; outline: none; }
.silence-list article { display: grid; grid-template-columns: 1fr 120px 220px 80px 90px; gap: 14px; align-items: center; padding: 13px 12px; border-bottom: 1px solid #15364e; }
.silence-list article span, .silence-list article small { display: block; margin-top: 4px; color: #668198; font-size: 10px; }
.silence-list article em { color: #7894a9; font-style: normal; }
.silence-list article em[data-state='active'] { color: #f2b841; }
.silence-list article button { padding: 6px 8px; }
.routing-center { margin-top: 18px; border: 1px solid #173b55; background: #081e31; padding: 18px; }
.routing-center > header { display: flex; justify-content: space-between; align-items: center; }
.routing-center > header span { color: #21d4ee; font-size: 10px; letter-spacing: 2px; }
.routing-center > header h2 { margin: 5px 0 14px; }
.routing-center > header b { color: #7894a9; font-size: 12px; }
.route-create { display: grid; grid-template-columns: 1.2fr 105px 115px 115px 125px 150px 85px; gap: 8px; padding: 12px; background: #0d293f; }
.route-create input,.route-create select { min-width: 0; border: 1px solid #22445f; background: #071b2c; color: #dcecff; padding: 9px; outline: none; }
.route-table article { display: grid; grid-template-columns: 1fr 120px 140px 140px 65px 60px 55px; gap: 12px; align-items: center; padding: 13px 12px; border-bottom: 1px solid #15364e; }
.route-table article.disabled { opacity: .5; }
.route-table article code, .route-table article small { display: block; margin-top: 4px; color: #668198; font-size: 10px; }
.route-table article em { color: #24d2a2; font-style: normal; }
.route-table article button { padding: 6px 8px; }
.channel-center { margin-top: 18px; border: 1px solid #173b55; background: #081e31; padding: 18px; }
.channel-center > header { display:flex;justify-content:space-between;align-items:center }.channel-center > header span{color:#21d4ee;font-size:10px;letter-spacing:2px}.channel-center > header h2{margin:5px 0 14px}.channel-center > header b{color:#7894a9;font-size:12px}
.channel-create{display:grid;grid-template-columns:180px 1fr 100px;gap:8px;padding:12px;background:#0d293f}.channel-create input{min-width:0;border:1px solid #22445f;background:#071b2c;color:#dcecff;padding:9px;outline:none}
.channel-list article{display:grid;grid-template-columns:1fr 90px 150px 60px 60px 60px;gap:10px;align-items:center;padding:13px 12px;border-bottom:1px solid #15364e}.channel-list article.disabled{opacity:.5}.channel-list code,.channel-list small{display:block;margin-top:4px;color:#668198}.channel-list small{color:#ff7e91}.channel-list span[data-status='success']{color:#24d2a2}.channel-list span[data-status='failed']{color:#fa6078}.channel-list time{color:#668198;font-size:10px}.channel-list button{padding:6px}
.delivery-panel{border:1px solid #173b55;border-top:0;background:#081e31;padding:0 18px 18px}.delivery-stats{display:grid;grid-template-columns:repeat(4,1fr);gap:10px;padding:14px 0}.delivery-stats article{padding:12px;background:#0d293f;border:1px solid #19415d}.delivery-stats span{display:block;color:#668198;font-size:10px}.delivery-stats strong{display:block;margin-top:4px;font-size:20px}.delivery-panel h3{font-size:13px}.delivery-list article{display:grid;grid-template-columns:70px 1fr 70px 55px 1fr 145px;gap:10px;align-items:center;padding:9px;border-bottom:1px solid #15364e}.delivery-list span[data-status='success']{color:#24d2a2}.delivery-list span[data-status='failed']{color:#fa6078}.delivery-list small{display:block;color:#668198}.delivery-list code,.delivery-list em,.delivery-list time{color:#668198;font-size:10px;font-style:normal}.delivery-list p{color:#668198;text-align:center}
.queue-panel{border:1px solid #173b55;border-top:0;background:#081e31;padding:0 18px 18px}.queue-panel>header{display:flex;justify-content:space-between;align-items:center}.queue-panel>header span{color:#f2b841}.queue-panel>header span[data-status='running']{color:#24d2a2}.queue-stats{display:grid;grid-template-columns:150px 130px 200px 1fr;gap:10px}.queue-stats article{padding:11px;background:#0d293f}.queue-stats span{display:block;color:#668198;font-size:10px}.queue-stats strong{display:block;margin-top:5px;font-size:12px}.dead-letter-list article{display:grid;grid-template-columns:160px 1fr 70px 150px;gap:10px;padding:9px;border-bottom:1px solid #15364e}.dead-letter-list span{color:#fa6078}.dead-letter-list time,.dead-letter-list b{color:#668198;font-size:10px}
.dlq-ops{border:1px solid #173b55;border-top:0;background:#081e31;padding:0 18px 18px}.dlq-ops>header{display:flex;justify-content:space-between;align-items:center}.dlq-ops>header>div{display:flex;align-items:center;gap:7px;color:#668198}.dlq-ops>header button,.dlq-ops>article button{padding:6px 8px}.dlq-ops>article{display:grid;grid-template-columns:22px 1fr 130px 1.2fr 55px 145px 55px 55px;gap:10px;align-items:center;padding:9px;border-bottom:1px solid #15364e}.dlq-ops>article input{accent-color:#21d4ee}.dlq-ops>article small{display:block;color:#668198}.dlq-ops>article span,.dlq-ops>article time,.dlq-ops>article b{color:#668198;font-size:10px}.dlq-ops>article em{color:#fa6078;font-size:10px;font-style:normal;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.template-center{border:1px solid #173b55;border-top:0;background:#081e31;padding:0 18px 18px}.template-center>header{display:flex;justify-content:space-between;align-items:center}.template-center>header span{color:#668198;font-size:10px}.template-center>article{display:grid;grid-template-columns:180px 1fr 90px;gap:10px;align-items:center;padding:10px;border-bottom:1px solid #15364e}.template-center small{display:block;margin-top:4px;color:#668198}.template-center textarea{resize:vertical;border:1px solid #22445f;background:#071b2c;color:#dcecff;padding:9px;font-family:monospace;outline:none}.template-center button{padding:7px 10px}
.escalation-center{margin-top:18px;border:1px solid #25465e;background:linear-gradient(135deg,#081e31,#0a263a);padding:18px}.escalation-center>header{display:flex;justify-content:space-between;align-items:center}.escalation-center>header span{color:#f2b841;font-size:10px;letter-spacing:2px}.escalation-center>header h2{margin:5px 0 14px}.escalation-center>header b{color:#7894a9;font-size:12px}.escalation-create{display:grid;grid-template-columns:1.2fr 110px 135px 130px 150px 150px 85px;gap:8px;padding:12px;background:#0d293f}.escalation-create input,.escalation-create select,.escalation-create label{min-width:0;border:1px solid #22445f;background:#071b2c;color:#dcecff;padding:9px;outline:none}.escalation-create label{display:flex;align-items:center;gap:5px;white-space:nowrap}.escalation-create label input{width:48px;border:0;padding:0}.escalation-list article{display:grid;grid-template-columns:1fr 130px 140px 140px 75px 60px 55px;gap:12px;align-items:center;padding:13px 12px;border-bottom:1px solid #15364e}.escalation-list article.disabled{opacity:.5}.escalation-list article small{display:block;margin-top:4px;color:#668198;font-size:10px}.escalation-list article em{color:#f2b841;font-style:normal}.escalation-list article button{padding:6px 8px}
</style>
