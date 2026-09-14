<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Activity, AlertTriangle, ArrowRight, CheckCircle2, Clock3, Gauge, RefreshCw, Server, ShieldCheck } from 'lucide-vue-next'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/useAuthStore'

type Count = { label: string; count: number }
type QualityIssue = { severity: string; kind: string; target: string; message: string; recommendation: string }
type QualityReport = {
  generatedAt: string
  agents: { total: number; online: number; offline: number; pending: number; onlineRate: number; versions: Count[] }
  exporter: { total: number; active: number; inactive: number; unknown: number; coverageRate: number; ports: Count[] }
  tasks: { total: number; completed: number; failed: number; running: number; pending: number; successRate: number }
  schedules: { total: number; enabled: number; running: number; failed: number; successRate: number }
  issues: QualityIssue[]
}

const auth = useAuthStore()
const router = useRouter()
const report = ref<QualityReport | null>(null)
const loading = ref(false)
const error = ref('')

const healthTone = computed(() => {
  if (!report.value) return 'neutral'
  if (report.value.issues.some((item) => item.severity === 'critical')) return 'critical'
  if (report.value.issues.some((item) => item.severity === 'warning')) return 'warning'
  return 'healthy'
})

function rateText(value: number, total: number) {
  return total > 0 ? `${value.toFixed(1)}%` : '--'
}

function severityLabel(value: string) {
  if (value === 'critical') return '严重'
  if (value === 'warning') return '警告'
  return '提示'
}

function kindLabel(value: string) {
  const labels: Record<string, string> = {
    agent_offline: 'Agent 离线',
    exporter_status: 'Exporter 异常',
    task_failed: '采集任务失败',
    schedule_failed: '计划执行失败',
  }
  return labels[value] || value
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const response = await fetch('/api/v1/discovery/quality', { headers: { Authorization: `Bearer ${auth.token}` } })
    if (!response.ok) {
      let message = `采集质量数据加载失败（${response.status}）`
      try {
        const body = await response.json() as { message?: string; code?: string }
        message = body.message || body.code || message
      } catch {}
      throw new Error(message)
    }
    report.value = await response.json() as QualityReport
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '采集质量数据加载失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="quality-panel">
    <header class="quality-header">
      <div>
        <span>COLLECTION QUALITY</span>
        <h2>采集质量与离线风险</h2>
        <p>把 Agent 在线率、Exporter 覆盖率、任务成功率和计划异常集中到一个视图，优先处理会影响 CMDB 与监控的数据缺口。</p>
      </div>
      <div class="quality-actions">
        <button :disabled="loading" @click="load"><RefreshCw :class="{ spin: loading }" />{{ loading ? '刷新中' : '刷新' }}</button>
        <button class="monitor-link" @click="router.push('/monitor')"><ShieldCheck />打开监控告警<ArrowRight /></button>
      </div>
    </header>

    <div v-if="error" class="quality-error">{{ error }}</div>
    <div v-if="!report && loading" class="quality-loading"><Activity class="spin" />正在汇总采集质量…</div>

    <template v-if="report">
      <div class="quality-metrics">
        <article :data-tone="healthTone"><Gauge /><div><span>Agent 在线率</span><strong>{{ rateText(report.agents.onlineRate, report.agents.total) }}</strong><small>{{ report.agents.online }}/{{ report.agents.total }} 在线 · {{ report.agents.offline }} 离线</small></div></article>
        <article data-tone="healthy"><Server /><div><span>Exporter 覆盖率</span><strong>{{ rateText(report.exporter.coverageRate, report.exporter.total) }}</strong><small>{{ report.exporter.active }}/{{ report.exporter.total }} 已激活 · {{ report.exporter.unknown }} 未检测</small></div></article>
        <article :data-tone="report.tasks.failed ? 'warning' : 'healthy'"><CheckCircle2 /><div><span>任务成功率</span><strong>{{ rateText(report.tasks.successRate, report.tasks.completed + report.tasks.failed) }}</strong><small>完成 {{ report.tasks.completed }} · 失败 {{ report.tasks.failed }} · 运行中 {{ report.tasks.running }}</small></div></article>
        <article :data-tone="report.schedules.failed ? 'warning' : 'healthy'"><Clock3 /><div><span>计划执行健康度</span><strong>{{ rateText(report.schedules.successRate, report.schedules.total) }}</strong><small>计划 {{ report.schedules.total }} · 启用 {{ report.schedules.enabled }} · 失败 {{ report.schedules.failed }}</small></div></article>
      </div>

      <div class="quality-breakdown">
        <article>
          <header><div><h3>Agent 版本分布</h3><span>用于识别需要批量升级的节点</span></div></header>
          <div class="count-list"><span v-for="item in report.agents.versions" :key="item.label"><b>{{ item.count }}</b>{{ item.label }}</span><small v-if="!report.agents.versions.length">暂无 Agent 数据</small></div>
        </article>
        <article>
          <header><div><h3>Exporter 端口分布</h3><span>兼容 9100、19100 等自定义端口</span></div></header>
          <div class="count-list"><span v-for="item in report.exporter.ports" :key="item.label"><b>{{ item.count }}</b>{{ item.label }}</span><small v-if="!report.exporter.ports.length">暂无 Exporter 资产</small></div>
        </article>
      </div>

      <article class="quality-issues">
        <header><div><h3>待处理问题</h3><span>按严重程度排序，优先修复会阻断采集的问题</span></div><b>{{ report.issues.length }} 项</b></header>
        <div v-if="report.issues.length" class="issue-table">
          <div class="issue-head"><span>级别</span><span>类型</span><span>对象</span><span>问题说明</span><span>建议</span></div>
          <div v-for="issue in report.issues" :key="issue.kind + issue.target" class="issue-row">
            <span><i :data-severity="issue.severity">{{ severityLabel(issue.severity) }}</i></span>
            <span>{{ kindLabel(issue.kind) }}</span>
            <span>{{ issue.target }}</span>
            <span>{{ issue.message }}</span>
            <span>{{ issue.recommendation }}</span>
          </div>
        </div>
        <div v-else class="quality-empty"><CheckCircle2 /><div><strong>当前没有发现采集质量问题</strong><span>Agent、Exporter、任务和采集计划均处于健康状态。</span></div></div>
      </article>

      <footer class="quality-footer"><AlertTriangle />数据生成时间：{{ report.generatedAt }} · 离线告警规则可在监控告警中使用 Prometheus up == 0 和 absent(up) 进行联动。</footer>
    </template>
  </section>
</template>
<style scoped>
.quality-panel{display:grid;gap:16px;padding:2px 0 18px}.quality-header{display:flex;justify-content:space-between;gap:20px;align-items:flex-end;padding:18px 20px;border:1px solid #17445b;border-radius:14px;background:linear-gradient(135deg,#071923,#092b3d)}.quality-header>div:first-child{display:grid;gap:6px}.quality-header>div:first-child>span{color:#22d3ee;font:10px/1.2 monospace;letter-spacing:.18em}.quality-header h2{margin:0;color:#e6f7ff;font-size:22px}.quality-header p{max-width:780px;margin:0;color:#7894a8;font-size:12px;line-height:1.7}.quality-actions{display:flex;gap:8px}.quality-actions button{display:inline-flex;align-items:center;gap:6px;white-space:nowrap;border:1px solid #24617c;border-radius:8px;background:#0a2635;color:#bcecff;padding:9px 12px;cursor:pointer}.quality-actions button:hover{border-color:#22d3ee}.quality-actions svg{width:15px;height:15px}.quality-actions .monitor-link{background:#0d3a4a;color:#d9fbff}.quality-error{padding:12px 14px;border:1px solid #7f3041;border-radius:10px;background:#35131e;color:#ffc0cb}.quality-loading{display:flex;align-items:center;justify-content:center;gap:8px;padding:60px;color:#6f95aa}.quality-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px}.quality-metrics article{display:flex;gap:11px;align-items:center;padding:15px;border:1px solid #17445b;border-radius:12px;background:#081d29}.quality-metrics article>svg{width:22px;color:#22d3ee}.quality-metrics article[data-tone="critical"]{border-color:#84384d}.quality-metrics article[data-tone="critical"]>svg{color:#fb7185}.quality-metrics article[data-tone="warning"]{border-color:#80652d}.quality-metrics article[data-tone="warning"]>svg{color:#fbbf24}.quality-metrics article>div{display:grid;gap:3px;min-width:0}.quality-metrics span{color:#7e9db0;font-size:11px}.quality-metrics strong{color:#e6f7ff;font-size:23px}.quality-metrics small{color:#5f8195;font-size:10px}.quality-breakdown{display:grid;grid-template-columns:1fr 1fr;gap:12px}.quality-breakdown article,.quality-issues{border:1px solid #17445b;border-radius:12px;background:#081b27}.quality-breakdown header,.quality-issues>header{display:flex;justify-content:space-between;align-items:center;padding:13px 15px;border-bottom:1px solid #12364a}.quality-breakdown h3,.quality-issues h3{margin:0;color:#d8f3ff;font-size:14px}.quality-breakdown header span,.quality-issues header span{display:block;margin-top:3px;color:#66879a;font-size:10px}.quality-issues>header>b{color:#22d3ee;font-size:12px}.count-list{display:flex;flex-wrap:wrap;gap:8px;padding:14px 15px}.count-list span{display:inline-flex;align-items:center;gap:6px;padding:7px 9px;border:1px solid #1b536b;border-radius:8px;background:#0b2837;color:#bcecff;font-size:11px}.count-list b{color:#fff;font-size:14px}.count-list small{padding:8px;color:#5f8195;font-size:11px}.issue-table{overflow:auto}.issue-head,.issue-row{display:grid;grid-template-columns:70px 110px 170px 1.2fr 1.4fr;gap:10px;align-items:center;min-width:840px;padding:11px 15px}.issue-head{color:#63869b;font-size:10px;background:#071721}.issue-row{border-top:1px solid #102f40;color:#b5ccd9;font-size:11px}.issue-row i{display:inline-block;padding:3px 6px;border-radius:5px;font-style:normal;font-size:10px}.issue-row i[data-severity="critical"]{background:#5b1d2e;color:#fecdd3}.issue-row i[data-severity="warning"]{background:#59430e;color:#fde68a}.issue-row i[data-severity="info"]{background:#123f53;color:#a5f3fc}.quality-empty{display:flex;align-items:center;gap:10px;padding:28px 18px;color:#8fd8b0}.quality-empty svg{width:20px}.quality-empty div{display:grid;gap:3px}.quality-empty strong{font-size:13px}.quality-empty span{color:#64869a;font-size:11px}.quality-footer{display:flex;align-items:center;gap:7px;color:#6f95aa;font-size:11px}.quality-footer svg{width:14px;color:#fbbf24}.spin{animation:quality-spin 1s linear infinite}@keyframes quality-spin{to{transform:rotate(360deg)}}@media(max-width:1100px){.quality-header{align-items:flex-start;flex-direction:column}.quality-metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:720px){.quality-metrics,.quality-breakdown{grid-template-columns:1fr}.quality-actions{flex-wrap:wrap}}
</style>
