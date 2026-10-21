<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import {
  Activity,
  BellRing,
  Box,
  ChevronRight,
  CircleDot,
  ClipboardCheck,
  Clock3,
  Database,
  Gauge,
  Layers3,
  Maximize2,
  Network,
  RadioTower,
  RefreshCw,
  Rocket,
  Server,
  ShieldCheck,
  Signal,
  TicketCheck,
  TriangleAlert,
  Workflow,
} from 'lucide-vue-next'
import TrendChart from '@/components/TrendChart.vue'
import { buildMetricCards, formatCompactTime, formatCount, formatPercent, formatUpdateTime, statusTone } from '@/features/dashboard/dashboardModel'
import type { DashboardOverview } from '@/features/dashboard/types'

const props = defineProps<{ overview: DashboardOverview; loading?: boolean }>()
defineEmits<{ refresh: [] }>()

const router = useRouter()
const cards = computed(() => buildMetricCards(props.overview))
const metricIcons = { Database, RadioTower, BellRing, Activity, ClipboardCheck, Rocket }
const sourceIcons = { cmdb: Database, agents: RadioTower, prometheus: Activity, jobs: Workflow, tickets: TicketCheck, releases: Rocket }
const status = computed(() => statusTone(props.overview.command.platformStatus))
const statusText = computed(() => props.overview.command.platformStatus === 'attention' ? '存在需关注事件' : '全域运行稳定')
const activeSources = computed(() => props.overview.sources.filter(item => item.status === 'live').length)
const assetTotal = computed(() => Math.max(props.overview.assets.total, 1))
const assetSegments = computed(() => [
  { label: '在线', value: props.overview.assets.online, tone: 'healthy' },
  { label: '关注', value: props.overview.assets.warning, tone: 'warning' },
  { label: '离线', value: props.overview.assets.offline, tone: 'critical' },
  { label: '未知', value: props.overview.assets.unknown, tone: 'muted' },
])
const ringMetrics = computed(() => [
  { label: '资产健康', value: props.overview.command.assetHealth, tone: 'cyan' },
  { label: 'Agent 覆盖', value: props.overview.command.agentCoverage, tone: 'blue' },
  { label: '监控覆盖', value: props.overview.command.monitorCoverage, tone: 'green' },
])

function go(path: string) {
  void router.push(path)
}

async function fullscreen() {
  if (!document.fullscreenElement) await document.documentElement.requestFullscreen?.()
  else await document.exitFullscreen?.()
}

function deploymentLabel(value: string) {
  return ({
    success: '成功',
    running: '进行中',
    failed: '失败',
    pending: '待开始',
    approved: '待部署',
    rejected: '已驳回',
    awaiting_approval: '待审批',
    rollback_awaiting_approval: '回滚待审',
    rollback_approved: '回滚待执行',
    rollback_rejected: '回滚已驳回',
    'rolling-back': '回滚中',
    cancelled: '已取消',
  } as Record<string, string>)[value] || value
}

function taskLabel(value: string) {
  return ({ running: '执行中', queued: '排队中', pending: '待执行', awaiting_approval: '待审批', 'waiting-approval': '待审批' } as Record<string, string>)[value] || value
}

function alertLabel(value: string) {
  return value === 'critical' ? '严重' : value === 'warning' ? '警告' : '高危'
}
</script>

<template>
  <section class="gcc-shell">
    <header class="gcc-head">
      <div class="gcc-title">
        <div class="gcc-signal"><Signal /></div>
        <div>
          <span class="gcc-kicker">GLOBAL OPERATIONS COMMAND</span>
          <h1>全局态势指挥中心</h1>
          <p>以 CMDB 为主数据源，聚合 Agent、Prometheus、任务、工单和发布流水线。</p>
        </div>
      </div>
      <div class="gcc-head-actions">
        <div class="gcc-live"><i></i><span>{{activeSources}} / {{overview.sources.length}} 数据源在线</span></div>
        <div class="gcc-clock"><Clock3 /><span>{{formatUpdateTime(overview.updatedAt)}}</span></div>
        <button type="button" :class="{spinning: loading}" aria-label="刷新态势数据" @click="$emit('refresh')"><RefreshCw /></button>
        <button type="button" aria-label="全屏展示" @click="fullscreen"><Maximize2 /></button>
      </div>
    </header>

    <div class="gcc-hero">
      <section class="gcc-core" :data-status="status">
        <div class="gcc-core-grid"></div>
        <div class="gcc-orbit orbit-outer"></div>
        <div class="gcc-orbit orbit-middle"></div>
        <div class="gcc-orbit orbit-inner"></div>
        <span class="orbit-node node-a"></span><span class="orbit-node node-b"></span><span class="orbit-node node-c"></span>
        <div class="gcc-core-center">
          <span class="core-eyebrow">PLATFORM HEALTH</span>
          <strong>{{formatPercent(overview.command.assetHealth)}}</strong>
          <b>{{statusText}}</b>
          <small>{{overview.command.criticalAlerts}} 个严重告警 · {{overview.command.activeIncidents}} 个活跃事件</small>
        </div>
        <div class="gcc-ring-metrics">
          <div v-for="item in ringMetrics" :key="item.label" :data-tone="item.tone" :style="{'--value':item.value+'%'}">
            <span class="mini-ring"><i></i></span>
            <p><b>{{formatPercent(item.value)}}</b><small>{{item.label}}</small></p>
          </div>
        </div>
        <div class="gcc-corner corner-tl"></div><div class="gcc-corner corner-tr"></div>
        <div class="gcc-corner corner-bl"></div><div class="gcc-corner corner-br"></div>
      </section>

      <div class="gcc-kpi-grid">
        <article v-for="(card, index) in cards" :key="card.label" class="gcc-kpi" :data-tone="card.tone" :style="{'--delay':index*70+'ms'}">
          <div class="kpi-top">
            <span class="kpi-icon"><component :is="metricIcons[card.icon as keyof typeof metricIcons]" /></span>
            <small>0{{index + 1}}</small>
          </div>
          <span>{{card.label}}</span>
          <strong>{{card.value}}</strong>
          <p>{{card.note}}</p>
          <i class="kpi-scan"></i>
        </article>
      </div>
    </div>

    <section class="gcc-source-strip" aria-label="实时数据源">
      <div class="source-strip-label"><ShieldCheck /><span><b>REAL DATA LINK</b><small>真实数据源链路</small></span></div>
      <div v-for="source in overview.sources" :key="source.code" class="gcc-source" :data-status="source.status">
        <span class="source-icon"><component :is="sourceIcons[source.code as keyof typeof sourceIcons] || CircleDot" /></span>
        <div><strong>{{source.name}}</strong><small>{{source.detail}}</small></div>
        <div class="source-state"><i></i><b>{{source.status === 'live' ? 'LIVE' : 'IDLE'}}</b><time>{{source.updatedAt}}</time></div>
      </div>
    </section>

    <div class="gcc-grid">
      <section class="gcc-panel gcc-trend">
        <header><div><span>ALERT SIGNAL</span><h2>近 24 小时告警脉冲</h2></div><button @click="go('/monitor')">告警中心 <ChevronRight /></button></header>
        <div class="gcc-chart-wrap"><TrendChart :points="overview.alertTrend" /><div class="chart-axis-label">告警总数 / 严重告警</div></div>
      </section>

      <section class="gcc-panel gcc-alerts">
        <header><div><span>INCIDENT FEED</span><h2>高优先级告警</h2></div><button @click="go('/monitor')">全部告警 <ChevronRight /></button></header>
        <div v-if="overview.alerts.length" class="gcc-alert-list">
          <button v-for="alert in overview.alerts.slice(0, 6)" :key="alert.id" @click="go('/monitor')">
            <span class="alert-level" :data-level="alert.level"><TriangleAlert /></span>
            <span class="alert-copy"><strong>{{alert.title}}</strong><small>{{alert.target}} · {{formatCompactTime(alert.occurredAt)}}</small></span>
            <b>{{alertLabel(alert.level)}}</b>
          </button>
        </div>
        <div v-else class="gcc-empty"><ShieldCheck /><strong>当前无高优先级告警</strong><span>数据来自 Prometheus / Alertmanager</span></div>
      </section>

      <section class="gcc-panel gcc-health">
        <header><div><span>ASSET DISTRIBUTION</span><h2>资源健康分布</h2></div><b class="panel-total">{{formatCount(overview.assets.total)}}</b></header>
        <div class="health-orbit">
          <div class="health-ring" :style="{'--health':overview.command.assetHealth+'%'}">
            <div><strong>{{formatPercent(overview.command.assetHealth)}}</strong><span>资产健康率</span></div>
          </div>
          <div class="health-legend">
            <div v-for="item in assetSegments" :key="item.label" :data-tone="item.tone">
              <i></i><span>{{item.label}}</span><b>{{formatCount(item.value)}}</b>
              <small>{{item.value ? Math.round(item.value / assetTotal * 100) : 0}}%</small>
            </div>
          </div>
        </div>
      </section>

      <section class="gcc-panel gcc-topology">
        <header><div><span>INFRASTRUCTURE MAP</span><h2>基础设施关系图谱</h2></div><button @click="go('/topology')">拓扑中心 <ChevronRight /></button></header>
        <div v-if="overview.topology.layers.length" class="gcc-topology-flow">
          <div v-for="(layer, index) in overview.topology.layers" :key="layer.name" class="topology-layer">
            <div class="layer-name"><span>0{{index + 1}}</span><strong>{{layer.name}}</strong></div>
            <div class="layer-nodes">
              <div v-for="node in layer.nodes.slice(0, 5)" :key="node.id" :data-status="node.status">
                <Server v-if="layer.name === '机房位置'" /><Layers3 v-else-if="index === 1" /><Box v-else /><span>{{node.name}}</span>
              </div>
            </div>
          </div>
        </div>
        <div v-else class="gcc-empty"><Network /><strong>当前范围暂无资产关系</strong><span>资产采集后将自动呈现实体链路</span></div>
      </section>

      <section class="gcc-panel gcc-tasks">
        <header><div><span>AUTOMATION QUEUE</span><h2>活动任务</h2></div><button @click="go('/jobs')">任务中心 <ChevronRight /></button></header>
        <div v-if="overview.jobs.length" class="task-list">
          <button v-for="job in overview.jobs.slice(0, 5)" :key="job.id" @click="go('/jobs')">
            <span class="task-index">{{job.id.replace(/[^0-9]/g, '').slice(-2) || '--'}}</span>
            <div><strong>{{job.name}}</strong><small>{{job.owner || '系统任务'}} · {{taskLabel(job.status)}}</small><span class="task-track"><i :style="{width:job.progress+'%'}"></i></span></div>
            <b>{{job.progress}}%</b>
          </button>
        </div>
        <div v-else class="gcc-empty compact"><ClipboardCheck /><strong>当前无活动任务</strong><span>任务执行后实时显示进度</span></div>
      </section>

      <section class="gcc-panel gcc-capacity">
        <header><div><span>CMDB CAPACITY</span><h2>资产构成</h2></div><small>{{overview.assets.environments}} 个环境</small></header>
        <div class="capacity-list">
          <div v-for="item in overview.capacity" :key="item.name">
            <p><span>{{item.name}}</span><b>{{item.detail}}</b></p>
            <div class="capacity-track"><i :style="{width:item.value+'%'}"></i></div>
            <small>{{formatPercent(item.value)}} 占比</small>
          </div>
        </div>
      </section>

      <section class="gcc-panel gcc-release">
        <header><div><span>DELIVERY PIPELINE</span><h2>发布流水线</h2></div><button @click="go('/releases')">发布中心 <ChevronRight /></button></header>
        <div class="release-summary">
          <div class="release-rate"><span>成功率</span><strong>{{formatPercent(overview.releases.successRate)}}</strong><small>今日 {{overview.releases.today}} 次发布</small></div>
          <div class="release-counters"><span><i class="success"></i>成功 <b>{{overview.releases.success}}</b></span><span><i class="running"></i>进行中 <b>{{overview.releases.running}}</b></span><span><i class="failed"></i>失败 <b>{{overview.releases.failed}}</b></span></div>
        </div>
        <div v-if="overview.releases.recent.length" class="release-list">
          <button v-for="item in overview.releases.recent.slice(0, 4)" :key="item.name+item.version" @click="go('/releases')">
            <span><strong>{{item.name}}</strong><small>{{item.version}}</small></span>
            <time>{{formatCompactTime(item.updatedAt)}}</time>
            <b :data-status="item.status">{{deploymentLabel(item.status)}}</b>
          </button>
        </div>
        <div v-else class="gcc-empty compact"><Rocket /><strong>暂无发布记录</strong><span>流水线事件将被实时聚合</span></div>
      </section>

      <section class="gcc-panel gcc-operations">
        <header><div><span>OPERATIONS TIMELINE</span><h2>运维事件轴</h2></div><small>审计实时落盘</small></header>
        <div v-if="overview.timeline.length" class="gcc-timeline">
          <div v-for="event in overview.timeline.slice(0, 5)" :key="event.id">
            <time>{{formatCompactTime(event.time)}}</time><i :data-type="event.type"></i>
            <span><strong>{{event.message}}</strong><small>{{event.source}}</small></span>
          </div>
        </div>
        <div v-else class="gcc-empty compact"><Clock3 /><strong>当前范围暂无操作事件</strong><span>权限范围内的审计记录会显示在这里</span></div>
      </section>
    </div>
  </section>
</template>
