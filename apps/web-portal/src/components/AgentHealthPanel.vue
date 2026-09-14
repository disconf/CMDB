<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Activity, CheckCircle2, KeyRound, RefreshCw, Server, ShieldAlert, X } from 'lucide-vue-next'
import { listCredentials, type Credential } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'

type HealthIssue = { severity: string; code: string; message: string; suggestion: string }
type HealthResult = {
  host: string; hostname: string; ok: boolean; status: string; score: number
  agentBinary: boolean; agentServiceActive: boolean; agentServiceEnabled: boolean
  agentVersion: string; restartCount: number; uptimeSeconds: number; cpuCount: number
  diskFreeKb: number; diskUsagePercent: number; memoryUsagePercent: number; load1: number
  gatewayOk: boolean; gatewayCode: string; nodeExporterActive: boolean; nodeExporterPorts: number[]
  nodeExporterPort: number; journal: string; issues: HealthIssue[]; message: string
}
type RemediationResult = { host: string; action: string; ok: boolean; beforeStatus: string; afterStatus: string; info: string; createdAt: string }
type HealthCheck = {
  id: string; status: string; credentialId: string; sshPort: number; gatewayUrl: string
  hosts: string[]; results: HealthResult[]; remediation: RemediationResult[]
  total: number; healthy: number; warning: number; critical: number
  requestedBy: string; createdAt: string; finishedAt: string
}

const auth = useAuthStore()
const checks = ref<HealthCheck[]>([])
const selected = ref<HealthCheck | null>(null)
const credentials = ref<Credential[]>([])
const hosts = ref('172.28.69.161\n172.28.69.162\n172.28.69.163')
const sshPort = ref(22)
const credentialId = ref('')
const gatewayUrl = ref(window.location.protocol === 'https:' ? 'http://172.28.69.161:30080' : window.location.origin)
const loading = ref(false)
const busy = ref('')
const error = ref('')
const notice = ref('')

const headers = computed(() => ({ Authorization: `Bearer ${auth.token}`, 'Content-Type': 'application/json' }))
const sshCredentials = computed(() => credentials.value.filter((item) => item.kind === 'ssh'))
const current = computed(() => selected.value || checks.value[0] || null)
const summaryTone = computed(() => current.value?.critical ? 'critical' : current.value?.warning ? 'warning' : 'healthy')

async function request<T>(url: string, options?: RequestInit) {
  const response = await fetch(url, { ...options, headers: headers.value })
  if (!response.ok) {
    let message = `请求失败 (${response.status})`
    try {
      const body = await response.json() as { message?: string; code?: string }
      message = body.message || body.code || message
    } catch {}
    throw new Error(message)
  }
  return response.json() as Promise<T>
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [history, credentialList] = await Promise.all([
      request<HealthCheck[]>('/api/v1/discovery/agent-health-checks?limit=50'),
      listCredentials(auth.token),
    ])
    checks.value = history
    credentials.value = credentialList
    if (selected.value) {
      selected.value = history.find((item) => item.id === selected.value?.id) || null
    }
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '健康巡检数据加载失败'
  } finally {
    loading.value = false
  }
}

async function runCheck() {
  busy.value = 'check'
  error.value = ''
  notice.value = ''
  try {
    const targetHosts = hosts.value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean)
    const item = await request<HealthCheck>('/api/v1/discovery/agent-health-checks', {
      method: 'POST',
      body: JSON.stringify({ hosts: targetHosts, port: sshPort.value, credentialId: credentialId.value, gatewayUrl: gatewayUrl.value }),
    })
    checks.value = [item, ...checks.value.filter((entry) => entry.id !== item.id)]
    selected.value = item
    notice.value = `巡检完成：正常 ${item.healthy} 台，警告 ${item.warning} 台，严重 ${item.critical} 台`
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '巡检失败'
  } finally {
    busy.value = ''
  }
}

async function remediate(host: string, action: string) {
  if (!selected.value) return
  busy.value = `${host}:${action}`
  error.value = ''
  notice.value = ''
  try {
    const item = await request<HealthCheck>(`/api/v1/discovery/agent-health-checks/${encodeURIComponent(selected.value.id)}/remediate`, {
      method: 'POST',
      body: JSON.stringify({ action, hosts: [host] }),
    })
    selected.value = item
    checks.value = checks.value.map((entry) => entry.id === item.id ? item : entry)
    notice.value = `${host} 已执行安全自愈，当前状态：${statusLabel(resultByHost(item, host)?.status || '')}`
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '自愈失败'
  } finally {
    busy.value = ''
  }
}

function resultByHost(item: HealthCheck, host: string) {
  return item.results.find((result) => result.host === host)
}

function statusLabel(status: string) {
  return ({ healthy: '健康', warning: '警告', critical: '严重', running: '执行中' } as Record<string, string>)[status] || status || '未知'
}

function severityLabel(severity: string) {
  return ({ critical: '严重', warning: '警告', info: '提示' } as Record<string, string>)[severity] || severity
}

function actionLabel(action: string) {
  return ({
    'restart-agent': '重启 Agent',
    'enable-agent': '启用 Agent',
    'reset-agent': '重置失败状态',
    'restart-node-exporter': '重启 Exporter',
    'reinstall-agent': '重装 Agent',
  } as Record<string, string>)[action] || action
}

function formatTime(value: string) {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

function formatDuration(seconds: number) {
  if (seconds <= 0) return '-'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  return days > 0 ? `${days}天 ${hours}小时` : `${hours}小时`
}

function formatDisk(kb: number) {
  if (kb <= 0) return '-'
  return kb >= 1024 * 1024 ? `${(kb / 1024 / 1024).toFixed(1)} GB` : `${(kb / 1024).toFixed(0)} MB`
}

function remediations(item: HealthCheck) {
  return [...item.remediation].reverse()
}

function open(item: HealthCheck) {
  selected.value = item
  notice.value = ''
}

onMounted(load)
</script>

<template>
  <section class="agent-health-panel">
    <header class="health-header">
      <div>
        <span>AGENT HEALTH & SELF-HEALING</span>
        <h2>Agent 健康巡检与异常自愈</h2>
        <p>巡检默认只读；自愈仅执行平台白名单动作，并完整记录前后状态、执行结果和操作审计。</p>
      </div>
      <button :disabled="loading" @click="load"><RefreshCw :class="{ spin: loading }" />{{ loading ? '刷新中' : '刷新历史' }}</button>
    </header>

    <div v-if="error" class="health-error">{{ error }}</div>
    <div v-if="notice" class="health-notice"><CheckCircle2 />{{ notice }}</div>

    <div class="health-metrics" :data-tone="summaryTone">
      <article><Server /><div><span>最近巡检主机</span><strong>{{ current?.total || 0 }}</strong><small>{{ current ? formatTime(current.finishedAt || current.createdAt) : '暂无巡检记录' }}</small></div></article>
      <article data-tone="healthy"><CheckCircle2 /><div><span>健康</span><strong>{{ current?.healthy || 0 }}</strong><small>Agent、Gateway 与系统资源正常</small></div></article>
      <article data-tone="warning"><ShieldAlert /><div><span>警告</span><strong>{{ current?.warning || 0 }}</strong><small>需要优化或持续观察</small></div></article>
      <article data-tone="critical"><Activity /><div><span>严重</span><strong>{{ current?.critical || 0 }}</strong><small>建议立即执行安全自愈</small></div></article>
    </div>

    <section class="health-runner">
      <header><div><h3>发起巡检</h3><span>支持单 IP、CIDR 网段（每行一个），网段会自动展开并限制规模。</span></div><KeyRound /></header>
      <div class="runner-grid">
        <label><span>目标主机 / CIDR</span><textarea v-model="hosts" rows="4" placeholder="172.28.69.161&#10;172.28.69.0/24"></textarea></label>
        <label><span>SSH 端口</span><input v-model.number="sshPort" type="number" min="1" max="65535"></label>
        <label><span>SSH 凭据</span><select v-model="credentialId"><option value="">使用服务端默认凭据</option><option v-for="item in sshCredentials" :key="item.id" :value="item.id">{{ item.name }} · {{ item.username || '默认用户' }}</option></select></label>
        <label><span>Gateway 地址</span><input v-model="gatewayUrl" placeholder="http://172.28.69.161:30080"></label>
        <button class="primary" :disabled="Boolean(busy)" @click="runCheck"><Activity />{{ busy === 'check' ? '巡检中…' : '开始健康巡检' }}</button>
      </div>
    </section>

    <section class="health-history">
      <header><div><h3>巡检历史</h3><span>点击某次巡检查看逐台指标、问题与自愈记录。</span></div><b>{{ checks.length }} 条</b></header>
      <div class="health-table-scroll">
        <table>
          <thead><tr><th>时间</th><th>状态</th><th>目标</th><th>正常 / 警告 / 严重</th><th>发起人</th><th>操作</th></tr></thead>
          <tbody>
            <tr v-for="item in checks" :key="item.id">
              <td><strong>{{ formatTime(item.createdAt) }}</strong><small>{{ item.id }}</small></td>
              <td><span class="health-status" :data-status="item.status">{{ statusLabel(item.status) }}</span></td>
              <td>{{ item.total }} 台<small>{{ item.hosts.slice(0, 3).join('、') }}{{ item.hosts.length > 3 ? ' …' : '' }}</small></td>
              <td><b class="healthy-count">{{ item.healthy }}</b> / <b class="warning-count">{{ item.warning }}</b> / <b class="critical-count">{{ item.critical }}</b></td>
              <td>{{ item.requestedBy || '-' }}</td>
              <td><button @click="open(item)">查看详情</button></td>
            </tr>
            <tr v-if="!checks.length"><td colspan="6" class="health-empty">暂无巡检记录，请先发起一次只读巡检。</td></tr>
          </tbody>
        </table>
      </div>
    </section>

    <div v-if="selected" class="health-detail-backdrop" @click.self="selected = null">
      <section class="health-detail">
        <header>
          <div><span>HEALTH CHECK DETAIL</span><h2>{{ selected.id }}</h2><small>{{ formatTime(selected.createdAt) }} · {{ selected.requestedBy || 'system' }}</small></div>
          <button aria-label="关闭" @click="selected = null"><X /></button>
        </header>

        <div class="detail-summary">
          <span :data-status="selected.status">{{ statusLabel(selected.status) }}</span>
          <span>Total {{ selected.total }}</span><span class="healthy-count">Healthy {{ selected.healthy }}</span><span class="warning-count">Warning {{ selected.warning }}</span><span class="critical-count">Critical {{ selected.critical }}</span>
        </div>

        <article v-for="item in selected.results" :key="item.host" class="host-health-card" :data-status="item.status">
          <header>
            <div><strong>{{ item.hostname || item.host }}</strong><span>{{ item.host }} · Agent {{ item.agentVersion || 'unknown' }} · 评分 {{ item.score }}</span></div>
            <b :data-status="item.status">{{ statusLabel(item.status) }}</b>
          </header>
          <div class="host-metrics">
            <span>Agent 服务<strong>{{ item.agentServiceActive ? '运行中' : '未运行' }}</strong></span>
            <span>开机自启<strong>{{ item.agentServiceEnabled ? '已启用' : '未启用' }}</strong></span>
            <span>Gateway<strong>{{ item.gatewayOk ? `正常 ${item.gatewayCode}` : item.gatewayCode || '不可达' }}</strong></span>
            <span>Exporter<strong>{{ item.nodeExporterActive ? `运行中 :${item.nodeExporterPort || '-'}` : '未检测到' }}</strong></span>
            <span>磁盘使用<strong>{{ item.diskUsagePercent.toFixed(1) }}%</strong></span>
            <span>内存使用<strong>{{ item.memoryUsagePercent.toFixed(1) }}%</strong></span>
            <span>负载 1m<strong>{{ item.load1.toFixed(2) }}</strong></span>
            <span>系统运行<strong>{{ formatDuration(item.uptimeSeconds) }}</strong></span>
            <span>重启次数<strong>{{ item.restartCount }}</strong></span>
            <span>根分区剩余<strong>{{ formatDisk(item.diskFreeKb) }}</strong></span>
          </div>
          <div v-if="item.issues.length" class="health-issues">
            <article v-for="issue in item.issues" :key="issue.code">
              <i :data-severity="issue.severity">{{ severityLabel(issue.severity) }}</i>
              <div><strong>{{ issue.message }}</strong><span>{{ issue.suggestion }}</span></div>
            </article>
          </div>
          <div v-else class="health-ok"><CheckCircle2 />该主机未发现异常问题。</div>
          <details v-if="item.journal"><summary>查看最近 Agent 日志</summary><pre>{{ item.journal }}</pre></details>
          <footer>
            <span>安全自愈（白名单）</span>
            <button :disabled="Boolean(busy)" @click="remediate(item.host, 'restart-agent')">{{ busy === `${item.host}:restart-agent` ? '执行中…' : '重启 Agent' }}</button>
            <button :disabled="Boolean(busy)" @click="remediate(item.host, 'enable-agent')">{{ busy === `${item.host}:enable-agent` ? '执行中…' : '启用 Agent' }}</button>
            <button :disabled="Boolean(busy)" @click="remediate(item.host, 'reset-agent')">{{ busy === `${item.host}:reset-agent` ? '执行中…' : '重置失败状态' }}</button>
            <button :disabled="Boolean(busy)" @click="remediate(item.host, 'restart-node-exporter')">{{ busy === `${item.host}:restart-node-exporter` ? '执行中…' : '重启 Exporter' }}</button>
            <button class="danger" :disabled="Boolean(busy)" @click="remediate(item.host, 'reinstall-agent')">{{ busy === `${item.host}:reinstall-agent` ? '执行中…' : '重装 Agent' }}</button>
          </footer>
        </article>

        <section v-if="selected.remediation.length" class="remediation-history">
          <h3>自愈执行历史</h3>
          <article v-for="(item, index) in remediations(selected)" :key="`${item.host}-${item.action}-${index}`">
            <b :data-ok="item.ok">{{ item.ok ? '成功' : '失败' }}</b>
            <span>{{ item.host }} · {{ actionLabel(item.action) }}</span>
            <span>{{ statusLabel(item.beforeStatus) }} → {{ statusLabel(item.afterStatus) }}</span>
            <time>{{ formatTime(item.createdAt) }}</time>
            <small>{{ item.info || '-' }}</small>
          </article>
        </section>
      </section>
    </div>
  </section>
</template>

<style scoped>
.agent-health-panel{grid-column:1/-1;display:grid;gap:16px;padding-bottom:18px}.health-header{display:flex;align-items:flex-end;justify-content:space-between;gap:20px;padding:18px 20px;border:1px solid #17445b;border-radius:14px;background:linear-gradient(135deg,#071923,#092b3d)}.health-header>div{display:grid;gap:6px}.health-header>div>span{color:#22d3ee;font:10px/1.2 monospace;letter-spacing:.18em}.health-header h2{margin:0;color:#e6f7ff;font-size:22px}.health-header p{max-width:820px;margin:0;color:#7894a8;font-size:12px;line-height:1.7}.health-header button{display:inline-flex;align-items:center;gap:7px;border:1px solid #24617c;border-radius:8px;background:#0a2635;color:#bcecff;padding:9px 12px;cursor:pointer}.health-error,.health-notice{display:flex;align-items:center;gap:8px;padding:12px 14px;border-radius:10px;font-size:12px}.health-error{border:1px solid #7f3041;background:#35131e;color:#ffc0cb}.health-notice{border:1px solid #176b5d;background:#082a29;color:#99f6e4}.health-notice svg{width:16px}.health-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px}.health-metrics article{display:flex;gap:11px;align-items:center;padding:15px;border:1px solid #17445b;border-radius:12px;background:#081d29}.health-metrics article>svg{width:22px;color:#22d3ee}.health-metrics article[data-tone="warning"]{border-color:#80652d}.health-metrics article[data-tone="warning"]>svg{color:#fbbf24}.health-metrics article[data-tone="critical"]{border-color:#84384d}.health-metrics article[data-tone="critical"]>svg{color:#fb7185}.health-metrics article>div{display:grid;gap:3px;min-width:0}.health-metrics span{color:#7e9db0;font-size:11px}.health-metrics strong{color:#e6f7ff;font-size:23px}.health-metrics small{color:#5f8195;font-size:10px}.health-runner,.health-history{border:1px solid #17445b;border-radius:12px;background:#081b27}.health-runner>header,.health-history>header{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:13px 15px;border-bottom:1px solid #12364a}.health-runner h3,.health-history h3{margin:0;color:#d8f3ff;font-size:14px}.health-runner header span,.health-history header span{display:block;margin-top:3px;color:#66879a;font-size:10px}.health-runner header>svg{color:#22d3ee}.runner-grid{display:grid;grid-template-columns:minmax(300px,1.4fr) 120px minmax(220px,.8fr) minmax(240px,1fr) auto;gap:12px;align-items:end;padding:15px}.runner-grid label,.health-runner label{display:grid;gap:6px}.runner-grid label>span{color:#7e9db0;font-size:11px}.runner-grid input,.runner-grid select,.runner-grid textarea{width:100%;box-sizing:border-box;border:1px solid #1d4c65;border-radius:7px;background:#061722;color:#d9f4ff;padding:9px 10px}.runner-grid textarea{resize:vertical}.runner-grid .primary{display:inline-flex;align-items:center;gap:7px;justify-content:center;white-space:nowrap;border:1px solid #06a9d8;border-radius:7px;background:linear-gradient(135deg,#08799d,#0a4b78);color:#fff;padding:10px 13px;cursor:pointer}.runner-grid button:disabled,.health-detail button:disabled{opacity:.45;cursor:not-allowed}.health-table-scroll{overflow:auto}.health-history table{width:100%;min-width:880px;border-collapse:collapse}.health-history th,.health-history td{padding:11px 15px;border-top:1px solid #102f40;text-align:left;color:#b5ccd9;font-size:11px;vertical-align:middle}.health-history th{color:#63869b;font-size:10px;background:#071721}.health-history td>strong,.health-history td>small{display:block}.health-history td>small{max-width:300px;overflow:hidden;text-overflow:ellipsis;color:#5f8195;margin-top:3px}.health-status,.host-health-card>header>b{display:inline-block;padding:3px 7px;border-radius:5px;font-size:10px;font-weight:600}.health-status[data-status="healthy"],.host-health-card>header>b[data-status="healthy"]{background:#064e3b;color:#6ee7b7}.health-status[data-status="warning"],.host-health-card>header>b[data-status="warning"]{background:#59430e;color:#fde68a}.health-status[data-status="critical"],.host-health-card>header>b[data-status="critical"]{background:#5b1d2e;color:#fecdd3}.health-history button,.health-detail button{cursor:pointer}.healthy-count{color:#34d399}.warning-count{color:#fbbf24}.critical-count{color:#fb7185}.health-empty{text-align:center!important;color:#64869a!important;padding:28px!important}.health-detail-backdrop{position:fixed;inset:0;z-index:80;display:grid;place-items:center;padding:24px;background:rgba(2,10,18,.78);backdrop-filter:blur(5px)}.health-detail{width:min(1180px,96vw);max-height:92vh;overflow:auto;border:1px solid #1a526c;border-radius:14px;background:#071923;box-shadow:0 28px 80px rgba(0,0,0,.5)}.health-detail>header{position:sticky;top:0;z-index:2;display:flex;align-items:center;justify-content:space-between;padding:16px 18px;border-bottom:1px solid #16405a;background:#081d29}.health-detail>header span{color:#22d3ee;font:10px monospace;letter-spacing:.16em}.health-detail>header h2{margin:4px 0;color:#e6f7ff;font-size:18px}.health-detail>header small{color:#66879a}.health-detail>header button{display:grid;place-items:center;border:1px solid #24415f;border-radius:7px;background:#10243a;color:#b9d6ee;padding:7px}.health-detail>header button svg{width:15px}.detail-summary{display:flex;align-items:center;gap:12px;flex-wrap:wrap;padding:12px 18px;border-bottom:1px solid #12364a;color:#8ba8ba;font-size:11px}.detail-summary>span:first-child{padding:4px 8px;border-radius:6px;font-weight:700}.detail-summary>span:first-child[data-status="healthy"]{background:#064e3b;color:#6ee7b7}.detail-summary>span:first-child[data-status="warning"]{background:#59430e;color:#fde68a}.detail-summary>span:first-child[data-status="critical"]{background:#5b1d2e;color:#fecdd3}.host-health-card{margin:14px 18px;border:1px solid #173e55;border-radius:11px;background:#081d29}.host-health-card[data-status="warning"]{border-color:#725a23}.host-health-card[data-status="critical"]{border-color:#7b3145}.host-health-card>header{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:13px 14px;border-bottom:1px solid #12364a}.host-health-card>header strong,.host-health-card>header span{display:block}.host-health-card>header strong{color:#e4f6ff}.host-health-card>header span{margin-top:3px;color:#66879a;font-size:10px}.host-metrics{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:8px;padding:12px 14px}.host-metrics>span{display:grid;gap:4px;padding:8px;border:1px solid #12394e;border-radius:7px;background:#061923;color:#6d8da1;font-size:10px}.host-metrics strong{color:#cceeff;font-size:12px}.health-issues{display:grid;gap:6px;padding:0 14px 12px}.health-issues article{display:flex;gap:8px;align-items:center;padding:8px;border-radius:7px;background:#20151b}.health-issues i{flex:0 0 auto;padding:3px 6px;border-radius:5px;font-style:normal;font-size:9px}.health-issues i[data-severity="critical"]{background:#5b1d2e;color:#fecdd3}.health-issues i[data-severity="warning"]{background:#59430e;color:#fde68a}.health-issues i[data-severity="info"]{background:#123f53;color:#a5f3fc}.health-issues div{display:grid;gap:2px}.health-issues strong{color:#f4d5dc;font-size:11px}.health-issues span{color:#8a7780;font-size:10px}.health-ok{display:flex;align-items:center;gap:7px;margin:0 14px 12px;color:#6ee7b7;font-size:11px}.health-ok svg{width:14px}.host-health-card details{margin:0 14px 12px;color:#7894a8;font-size:10px}.host-health-card pre{max-height:130px;overflow:auto;padding:9px;border-radius:7px;background:#020b11;color:#9fb8c8;white-space:pre-wrap}.host-health-card footer{display:flex;align-items:center;gap:7px;flex-wrap:wrap;padding:11px 14px;border-top:1px solid #12364a}.host-health-card footer>span{margin-right:auto;color:#66879a;font-size:10px}.host-health-card footer button{border:1px solid #24536b;border-radius:6px;background:#0b2a39;color:#bcecff;padding:7px 9px;font-size:10px}.host-health-card footer button.danger{border-color:#713046;background:#31121c;color:#ffb3c1}.remediation-history{margin:14px 18px 18px;border:1px solid #173e55;border-radius:11px;background:#081b27}.remediation-history h3{margin:0;padding:12px 14px;border-bottom:1px solid #12364a;color:#d8f3ff;font-size:13px}.remediation-history article{display:grid;grid-template-columns:60px 1fr 160px 150px;gap:8px;align-items:center;padding:10px 14px;border-top:1px solid #102f40;color:#b5ccd9;font-size:10px}.remediation-history b[data-ok="true"]{color:#34d399}.remediation-history b[data-ok="false"]{color:#fb7185}.remediation-history small{grid-column:2/-1;color:#66879a}.spin{animation:health-spin 1s linear infinite}@keyframes health-spin{to{transform:rotate(360deg)}}@media(max-width:1100px){.health-header{align-items:flex-start;flex-direction:column}.health-metrics{grid-template-columns:repeat(2,minmax(0,1fr))}.runner-grid{grid-template-columns:1fr 1fr}.runner-grid label:first-child,.runner-grid button{grid-column:1/-1}.host-metrics{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:720px){.health-metrics,.runner-grid{grid-template-columns:1fr}.runner-grid label:first-child,.runner-grid button{grid-column:auto}.health-detail-backdrop{padding:8px}.remediation-history article{grid-template-columns:60px 1fr}.remediation-history article span:nth-of-type(2),.remediation-history time{grid-column:2}.remediation-history small{grid-column:2}}
</style>
