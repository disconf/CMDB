<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ArrowUpCircle, CheckCircle2, Eye, KeyRound, Plus, RefreshCw, RotateCcw, Server, ShieldAlert, Trash2, Wifi, X } from 'lucide-vue-next'
import { listCredentials, type Credential } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'

type AgentVersionItem = { id: string; name: string; hostname: string; ip: string; version: string; status: string; lastHeartbeat: string; current: boolean; outdated: boolean; unknown: boolean; source: string }
type AgentVersionBucket = { version: string; count: number }
type AgentVersionReport = { currentVersion: string; nodeExporterVersion: string; total: number; online: number; offline: number; current: number; outdated: number; unknown: number; versions: AgentVersionBucket[]; agents: AgentVersionItem[] }
type TargetOptions = { projectGroups: string[]; tags: string[] }
type TargetAsset = { id: string; name: string; ip: string; status: string; projectGroup: string; environment: string; tags: string[] }
type TargetResolve = { targets: string[]; assets: TargetAsset[] }
type LifecycleResult = { host: string; ok: boolean; info: string }
type AgentDeployment = {
  id: string
  action: 'install' | 'upgrade' | 'uninstall'
  status: 'running' | 'success' | 'partial' | 'failed'
  targetVersion: string
  credentialId: string
  sshPort: number
  gatewayUrl: string
  assetType: string
  hosts: string[]
  results: LifecycleResult[]
  total: number
  succeeded: number
  failed: number
  precheck: boolean
  requestedBy: string
  message: string
  retryOf: string
  createdAt: string
  startedAt: string
  finishedAt: string
}
type PrecheckResult = { host: string; ok: boolean; os: string; kernel: string; architecture: string; root: boolean; systemd: boolean; curl: boolean; diskFreeKb: number; gatewayOk: boolean; gatewayCode: string; message: string }
type TargetMode = 'outdated' | 'ip' | 'projectGroup' | 'tag'

const auth = useAuthStore()
const report = ref<AgentVersionReport | null>(null)
const credentials = ref<Credential[]>([])
const targetOptions = ref<TargetOptions>({ projectGroups: [], tags: [] })
const deployments = ref<AgentDeployment[]>([])
const targetMode = ref<TargetMode>('outdated')
const manualHosts = ref('')
const selectedGroups = ref<string[]>([])
const selectedTags = ref<string[]>([])
const selectedAgentIPs = ref<string[]>([])
const resolvedHosts = ref<string[]>([])
const resolvedAssets = ref<TargetAsset[]>([])
const sshPort = ref(22)
const assetType = ref('')
const gatewayUrl = ref(window.location.protocol === 'https:' ? 'http://172.28.69.161:30080' : window.location.origin)
const credentialId = ref('')
const loading = ref(false)
const historyLoading = ref(false)
const busy = ref('')
const error = ref('')
const message = ref('')
const results = ref<LifecycleResult[]>([])
const precheckResults = ref<PrecheckResult[]>([])
const selectedDeployment = ref<AgentDeployment | null>(null)

const headers = computed(() => ({ Authorization: `Bearer ${auth.token}`, 'Content-Type': 'application/json' }))
const sshCredentials = computed(() => credentials.value.filter((item) => item.kind === 'ssh'))
const outdatedAgents = computed(() => report.value?.agents.filter((item) => item.outdated && item.ip) ?? [])
const targetLabel = computed(() => ({ outdated: '过期 Agent', ip: '手工 IP / CIDR', projectGroup: '项目组', tag: '标签' }[targetMode.value]))
const resolvedSummary = computed(() => resolvedHosts.value.length ? `已解析 ${resolvedHosts.value.length} 台目标主机` : '尚未解析目标主机')

async function request<T>(url: string, options?: RequestInit) {
  const response = await fetch(url, { ...options, headers: headers.value })
  if (!response.ok) {
    let detail = `请求失败 (${response.status})`
    try {
      const body = await response.json() as { message?: string; code?: string }
      detail = body.message || body.code || detail
    } catch {}
    throw new Error(detail)
  }
  return response.json() as Promise<T>
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [versionReport, credentialList, options, deploymentList] = await Promise.all([
      request<AgentVersionReport>('/api/v1/discovery/agent-versions'),
      listCredentials(auth.token),
      request<TargetOptions>('/api/v1/discovery/remote-target-options'),
      request<AgentDeployment[]>('/api/v1/discovery/agent-deployments?limit=50'),
    ])
    report.value = versionReport
    credentials.value = credentialList
    targetOptions.value = options
    deployments.value = deploymentList
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : 'Agent 数据加载失败'
  } finally {
    loading.value = false
  }
}

async function refreshDeployments() {
  historyLoading.value = true
  try {
    deployments.value = await request<AgentDeployment[]>('/api/v1/discovery/agent-deployments?limit=50')
  } finally {
    historyLoading.value = false
  }
}

function toggleValue(values: string[], value: string) {
  const index = values.indexOf(value)
  if (index >= 0) values.splice(index, 1)
  else values.push(value)
}

function unique(values: string[]) {
  return Array.from(new Set(values.map((item) => item.trim()).filter(Boolean)))
}

async function resolveTargets(showMessage = true) {
  error.value = ''
  if (showMessage) message.value = ''
  let selected: string[] = []
  if (targetMode.value === 'outdated') {
    selected = outdatedAgents.value.map((item) => item.ip)
    resolvedAssets.value = []
  } else if (targetMode.value === 'ip') {
    selected = unique(manualHosts.value.split(/[\s,]+/))
    resolvedAssets.value = []
  } else {
    const response = await request<TargetResolve>('/api/v1/discovery/remote-targets/resolve', {
      method: 'POST',
      body: JSON.stringify({
        projectGroups: targetMode.value === 'projectGroup' ? selectedGroups.value : [],
        tags: targetMode.value === 'tag' ? selectedTags.value : [],
        ips: [],
        assetIds: [],
      }),
    })
    selected = response.targets
    resolvedAssets.value = response.assets
  }
  resolvedHosts.value = unique([...selected, ...selectedAgentIPs.value])
  if (!resolvedHosts.value.length) throw new Error('没有可执行的目标主机，请先选择目标或勾选 Agent')
  if (showMessage) message.value = resolvedSummary.value
  return resolvedHosts.value
}

async function precheck() {
  busy.value = 'precheck'
  error.value = ''
  message.value = ''
  precheckResults.value = []
  try {
    const hosts = await resolveTargets(false)
    precheckResults.value = await request<PrecheckResult[]>('/api/v1/discovery/agent-precheck', {
      method: 'POST',
      body: JSON.stringify({ hosts, port: sshPort.value, credentialId: credentialId.value, gatewayUrl: gatewayUrl.value }),
    })
    const passed = precheckResults.value.filter((item) => item.ok).length
    message.value = `预检查完成：通过 ${passed} 台，需处理 ${precheckResults.value.length - passed} 台`
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : 'Agent 预检查失败'
  } finally {
    busy.value = ''
  }
}

async function run(action: 'install' | 'upgrade' | 'uninstall') {
  error.value = ''
  message.value = ''
  try {
    const hosts = await resolveTargets(false)
    const actionLabel = action === 'install' ? '安装' : action === 'upgrade' ? '升级' : '卸载'
    if (action === 'uninstall' && !window.confirm(`确认在 ${hosts.length} 台主机上卸载 cmdb-agent？`)) return
    if (action !== 'uninstall' && !window.confirm(`确认在 ${hosts.length} 台主机上${actionLabel} cmdb-agent？`)) return
    busy.value = action
    const endpoint = action === 'install' ? '/api/v1/discovery/agent-install' : action === 'upgrade' ? '/api/v1/discovery/agent-upgrade' : '/api/v1/discovery/agent-uninstall'
    const payload: Record<string, unknown> = { hosts, port: sshPort.value, credentialId: credentialId.value }
    if (action !== 'uninstall') {
      payload.assetType = assetType.value
      payload.gatewayUrl = gatewayUrl.value
    }
    if (action === 'upgrade') payload.targetVersion = report.value?.currentVersion || ''
    results.value = await request<LifecycleResult[]>(endpoint, { method: 'POST', body: JSON.stringify(payload) })
    const succeeded = results.value.filter((item) => item.ok).length
    message.value = `${actionLabel}完成：成功 ${succeeded} 台，失败 ${results.value.length - succeeded} 台`
    await Promise.all([load(), refreshDeployments()])
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : 'Agent 批量操作失败'
  } finally {
    busy.value = ''
  }
}

async function retryDeployment(item: AgentDeployment) {
  if (!window.confirm(`确认重试任务 ${item.id} 中的失败主机？`)) return
  busy.value = `retry-${item.id}`
  error.value = ''
  message.value = ''
  try {
    const retried = await request<AgentDeployment>(`/api/v1/discovery/agent-deployments/${item.id}/retry`, { method: 'POST' })
    selectedDeployment.value = retried
    results.value = retried.results
    message.value = `重试完成：成功 ${retried.succeeded} 台，失败 ${retried.failed} 台`
    await Promise.all([load(), refreshDeployments()])
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : 'Agent 重试失败'
  } finally {
    busy.value = ''
  }
}

function assessment(item: AgentVersionItem) {
  if (item.unknown) return '版本未知'
  if (item.current) return '最新版本'
  return '需要升级'
}

function actionLabel(action: string) {
  return ({ install: '安装', upgrade: '升级', uninstall: '卸载' } as Record<string, string>)[action] || action
}

function statusLabel(status: string) {
  return ({ running: '执行中', success: '成功', partial: '部分成功', failed: '失败' } as Record<string, string>)[status] || status
}

function formatDisk(kb: number) {
  if (!kb) return '-'
  if (kb >= 1024 * 1024) return `${(kb / 1024 / 1024).toFixed(1)} GB`
  if (kb >= 1024) return `${(kb / 1024).toFixed(1)} MB`
  return `${kb} KB`
}

function formatTime(value: string) {
  return value ? value.replace('T', ' ').replace('Z', '') : '-'
}

onMounted(load)
</script>

<template>
  <section class="discovery-panel agent-lifecycle">
    <header>
      <div><h2>Agent 部署任务中心</h2><span>预检查目标主机，批量安装、升级或卸载 Agent，并保留逐台执行历史和失败重试记录。</span></div>
      <button :disabled="loading" @click="load"><RefreshCw :class="{spin:loading}"/>刷新数据</button>
    </header>

    <div class="lifecycle-summary">
      <article><Server/><span>Agent 总数<strong>{{ report?.total ?? 0 }}</strong></span></article>
      <article><Wifi/><span>在线<strong>{{ report?.online ?? 0 }}</strong></span></article>
      <article><CheckCircle2/><span>当前版本<strong>{{ report?.current ?? 0 }}</strong></span></article>
      <article data-tone="warning"><ArrowUpCircle/><span>待升级<strong>{{ report?.outdated ?? 0 }}</strong></span></article>
      <article data-tone="muted"><ShieldAlert/><span>版本未知<strong>{{ report?.unknown ?? 0 }}</strong></span></article>
      <article><RotateCcw/><span>服务端版本<strong>v{{ report?.currentVersion || '-' }}</strong></span></article>
    </div>

    <div class="version-strip">
      <div><strong>版本分布</strong><span>node_exporter v{{ report?.nodeExporterVersion || '-' }}</span></div>
      <span v-for="item in report?.versions || []" :key="item.version" :class="{current:item.version === report?.currentVersion}">{{ item.version || '未知' }} × {{ item.count }}</span>
    </div>

    <div class="lifecycle-builder">
      <section class="builder-column target-column">
        <header><h3>1. 选择目标</h3><span>{{ targetLabel }}</span></header>
        <div class="mode-tabs">
          <button :class="{active:targetMode==='outdated'}" @click="targetMode='outdated'">过期 Agent ({{ outdatedAgents.length }})</button>
          <button :class="{active:targetMode==='ip'}" @click="targetMode='ip'">IP / CIDR</button>
          <button :class="{active:targetMode==='projectGroup'}" @click="targetMode='projectGroup'">项目组</button>
          <button :class="{active:targetMode==='tag'}" @click="targetMode='tag'">标签</button>
        </div>
        <textarea v-if="targetMode==='ip'" v-model="manualHosts" rows="5" placeholder="每行一个 IP 或 CIDR，例如：&#10;172.28.68.56&#10;172.28.69.0/24"></textarea>
        <div v-else-if="targetMode==='outdated'" class="selector-note"><ArrowUpCircle/><span>自动选择版本台账中所有低于或不同于服务端 v{{ report?.currentVersion }} 的 Agent。</span></div>
        <div v-else class="choice-cloud">
          <span v-if="targetMode==='projectGroup' && !targetOptions.projectGroups.length">暂无项目组选项</span>
          <button v-for="item in targetMode==='projectGroup' ? targetOptions.projectGroups : targetOptions.tags" :key="item" :class="{active:(targetMode==='projectGroup'?selectedGroups:selectedTags).includes(item)}" @click="toggleValue(targetMode==='projectGroup'?selectedGroups:selectedTags,item)">{{ item }}</button>
        </div>
        <div v-if="resolvedHosts.length" class="resolved-preview"><strong>{{ resolvedSummary }}</strong><span>{{ resolvedHosts.slice(0,8).join('、') }}{{ resolvedHosts.length > 8 ? ' …' : '' }}</span></div>
        <button class="secondary" :disabled="loading || Boolean(busy)" @click="resolveTargets()">解析目标</button>
      </section>

      <section class="builder-column connection-column">
        <header><h3>2. SSH 与安装参数</h3><span>凭据仅从 Vault 引用</span></header>
        <label><span>SSH 端口</span><input v-model.number="sshPort" type="number" min="1" max="65535"></label>
        <label><span>SSH 凭据</span><select v-model="credentialId"><option value="">使用服务端默认凭据</option><option v-for="item in sshCredentials" :key="item.id" :value="item.id">{{ item.name }} · {{ item.username || '默认用户' }} · {{ item.group || '未分组' }}</option></select></label>
        <label><span>资产类型（安装时）</span><select v-model="assetType"><option value="">自动识别物理机/虚拟机</option><option value="physical-server">物理服务器</option><option value="virtual-machine">虚拟机</option></select></label>
        <label><span>网关地址（二进制下发）</span><input v-model="gatewayUrl" placeholder="http://172.28.69.161:30080"></label>
        <div class="credential-hint"><KeyRound/>密码不回显，新增或轮换请前往“凭据中心”。</div>
      </section>

      <section class="builder-column action-column">
        <header><h3>3. 预检查与执行</h3><span>升级即重装当前服务端版本</span></header>
        <div class="current-bundle"><ArrowUpCircle/><div><span>可升级目标版本</span><strong>v{{ report?.currentVersion || '-' }}</strong><small>当前网关只发布可校验和可服务的二进制版本，不伪造历史版本。</small></div></div>
        <button class="secondary" :disabled="Boolean(busy)" @click="precheck"><CheckCircle2/>{{ busy==='precheck' ? '预检查中...' : '预检查目标主机' }}</button>
        <button class="primary" :disabled="Boolean(busy)" @click="run('install')"><Plus/>{{ busy==='install' ? '安装中...' : '安装 / 重装 Agent' }}</button>
        <button class="primary upgrade" :disabled="Boolean(busy) || !report?.currentVersion" @click="run('upgrade')"><ArrowUpCircle/>{{ busy==='upgrade' ? '升级中...' : `批量升级到 v${report?.currentVersion || '-'}` }}</button>
        <button class="danger" :disabled="Boolean(busy)" @click="run('uninstall')"><Trash2/>{{ busy==='uninstall' ? '卸载中...' : '批量卸载 Agent' }}</button>
      </section>
    </div>

    <div v-if="message" class="lifecycle-message">{{ message }}</div>
    <div v-if="error" class="discovery-error lifecycle-error">{{ error }}</div>

    <section v-if="precheckResults.length" class="result-panel precheck-panel">
      <header><div><h3>预检查结果</h3><span>只读检查，不会修改目标主机</span></div><span>通过 {{ precheckResults.filter(item => item.ok).length }} / {{ precheckResults.length }}</span></header>
      <div class="table-scroll">
        <table><thead><tr><th>主机</th><th>系统 / 架构</th><th>Root</th><th>systemd</th><th>curl</th><th>可用磁盘</th><th>网关</th><th>结论</th></tr></thead><tbody>
          <tr v-for="item in precheckResults" :key="item.host" :class="{failed:!item.ok}">
            <td><strong>{{ item.host }}</strong><small>{{ item.kernel || '-' }}</small></td>
            <td>{{ item.os || '-' }} / {{ item.architecture || '-' }}</td>
            <td>{{ item.root ? '是' : '否' }}</td>
            <td>{{ item.systemd ? '可用' : '不可用' }}</td>
            <td>{{ item.curl ? '可用' : '不可用' }}</td>
            <td>{{ formatDisk(item.diskFreeKb) }}</td>
            <td>{{ item.gatewayOk ? `正常 (${item.gatewayCode})` : item.gatewayCode || '无响应' }}</td>
            <td><span :class="['assessment', item.ok ? 'current' : 'failed']">{{ item.message }}</span></td>
          </tr>
        </tbody></table>
      </div>
    </section>

    <section class="result-panel deployment-history">
      <header><div><h3>部署任务历史</h3><span>安装、升级、卸载和失败重试都会记录在这条时间线中</span></div><span v-if="historyLoading">刷新中...</span></header>
      <div class="table-scroll">
        <table><thead><tr><th>创建时间</th><th>操作</th><th>状态</th><th>目标</th><th>成功 / 失败</th><th>版本</th><th>发起人</th><th>结果</th></tr></thead><tbody>
          <tr v-for="item in deployments" :key="item.id">
            <td><strong>{{ formatTime(item.createdAt) }}</strong><small>{{ item.retryOf ? `重试自 ${item.retryOf}` : item.id }}</small></td>
            <td>{{ actionLabel(item.action) }}</td>
            <td><span :class="['deployment-status', item.status]">{{ statusLabel(item.status) }}</span></td>
            <td>{{ item.total }} 台</td>
            <td><b class="success-count">{{ item.succeeded }}</b> / <b class="failed-count">{{ item.failed }}</b></td>
            <td>{{ item.targetVersion || '-' }}</td>
            <td>{{ item.requestedBy || '-' }}</td>
            <td class="history-actions"><button class="icon-button" title="查看逐台结果" @click="selectedDeployment=item"><Eye/>详情</button><button v-if="item.failed > 0 && item.status !== 'running'" class="icon-button retry" title="重试失败主机" :disabled="Boolean(busy)" @click="retryDeployment(item)"><RotateCcw/>重试</button></td>
          </tr>
          <tr v-if="!deployments.length"><td colspan="8" class="empty-cell">暂无部署任务，先完成一次预检查或批量操作。</td></tr>
        </tbody></table>
      </div>
    </section>

    <section class="result-panel version-ledger">
      <header><div><h3>Agent 版本台账</h3><span>勾选主机后，可覆盖上面的目标方式，仅操作被勾选主机</span></div><span v-if="selectedAgentIPs.length">已勾选 {{ selectedAgentIPs.length }} 台</span></header>
      <div class="table-scroll">
        <table><thead><tr><th></th><th>主机</th><th>IP</th><th>版本</th><th>判定</th><th>状态</th><th>最近心跳/上报</th><th>数据来源</th></tr></thead><tbody>
          <tr v-for="item in report?.agents || []" :key="item.id"><td><input v-model="selectedAgentIPs" type="checkbox" :value="item.ip" :disabled="!item.ip"></td><td><strong>{{ item.name || item.hostname }}</strong><small>{{ item.id }}</small></td><td>{{ item.ip || '-' }}</td><td>v{{ item.version || '-' }}</td><td><span :class="['assessment',{current:item.current,outdated:item.outdated,unknown:item.unknown}]">{{ assessment(item) }}</span></td><td><span :class="['agent-state',item.status]">{{ item.status }}</span></td><td>{{ item.lastHeartbeat || '-' }}</td><td>{{ item.source==='heartbeat'?'实时心跳':'CMDB 持久化' }}</td></tr>
          <tr v-if="!report?.agents?.length"><td colspan="8" class="empty-cell">暂无 Agent 版本数据，请先安装 Agent 或等待心跳上报。</td></tr>
        </tbody></table>
      </div>
    </section>

    <div v-if="results.length" class="lifecycle-results"><article v-for="item in results" :key="item.host" :class="{ok:item.ok}"><strong>{{ item.host }}</strong><b>{{ item.ok ? '成功' : '失败' }}</b><span>{{ item.info }}</span></article></div>

    <div v-if="selectedDeployment" class="modal-backdrop" @click.self="selectedDeployment=null">
      <section class="deployment-modal">
        <header><div><span>AGENT DEPLOYMENT</span><h2>{{ actionLabel(selectedDeployment.action) }} · {{ statusLabel(selectedDeployment.status) }}</h2><small>{{ selectedDeployment.id }}</small></div><button @click="selectedDeployment=null"><X/></button></header>
        <div class="deployment-meta"><span>创建时间<strong>{{ formatTime(selectedDeployment.createdAt) }}</strong></span><span>目标主机<strong>{{ selectedDeployment.total }} 台</strong></span><span>成功 / 失败<strong>{{ selectedDeployment.succeeded }} / {{ selectedDeployment.failed }}</strong></span><span>发起人<strong>{{ selectedDeployment.requestedBy || '-' }}</strong></span><span>目标版本<strong>{{ selectedDeployment.targetVersion || '-' }}</strong></span><span>SSH 端口<strong>{{ selectedDeployment.sshPort }}</strong></span></div>
        <div v-if="selectedDeployment.message" class="deployment-message">{{ selectedDeployment.message }}</div>
        <div class="deployment-detail-list"><article v-for="item in selectedDeployment.results" :key="item.host" :class="{ok:item.ok}"><strong>{{ item.host }}</strong><b>{{ item.ok ? '成功' : '失败' }}</b><span>{{ item.info }}</span></article><div v-if="!selectedDeployment.results.length" class="empty-cell">该任务在目标主机执行前失败，没有逐台结果。</div></div>
        <footer><button @click="selectedDeployment=null">关闭</button><button v-if="selectedDeployment.failed > 0 && selectedDeployment.status !== 'running'" class="primary" :disabled="Boolean(busy)" @click="retryDeployment(selectedDeployment)"><RotateCcw/>重试失败主机</button></footer>
      </section>
    </div>
  </section>
</template>

<style scoped>
.agent-lifecycle{display:flex;flex-direction:column}.lifecycle-summary{display:grid;grid-template-columns:repeat(6,1fr);gap:10px;padding:18px 20px 12px}.lifecycle-summary article{display:flex;align-items:center;gap:10px;padding:12px;border:1px solid #18384a;border-radius:10px;background:#091b29}.lifecycle-summary article svg{color:#22d3ee}.lifecycle-summary article[data-tone=warning] svg{color:#f59e0b}.lifecycle-summary article[data-tone=muted] svg{color:#94a3b8}.lifecycle-summary span{display:flex;flex-direction:column;color:#7890aa;font-size:11px}.lifecycle-summary strong{margin-top:4px;color:#e5f4ff;font-size:17px}.version-strip{display:flex;align-items:center;flex-wrap:wrap;gap:8px;margin:0 20px 16px;padding:10px 12px;border:1px solid #123043;background:#071824;color:#8fb3c7;font-size:11px}.version-strip>div{display:flex;flex-direction:column;min-width:150px;margin-right:auto}.version-strip strong{color:#dcecff}.version-strip span{padding:4px 7px;border-radius:5px;background:#0d2a3b}.version-strip span.current{background:#0b3a3b;color:#5eead4}.lifecycle-builder{display:grid;grid-template-columns:1.05fr 1fr .9fr;gap:12px;padding:0 20px 16px}.builder-column{display:flex;flex-direction:column;gap:10px;padding:14px;border:1px solid #18384a;border-radius:12px;background:#081a27}.builder-column>header{display:flex;align-items:center;justify-content:space-between;gap:8px}.builder-column h3{margin:0;color:#dcecff;font-size:13px}.builder-column header span{color:#66879b;font-size:10px}.builder-column label{display:grid;gap:6px;color:#7894a8;font-size:11px}.builder-column select,.builder-column input,.builder-column textarea{width:100%;box-sizing:border-box;border:1px solid #22465f;background:#061725;color:#dcecff;padding:9px;outline:none}.mode-tabs{display:grid;grid-template-columns:1fr 1fr;gap:5px}.mode-tabs button{padding:7px;font-size:10px}.mode-tabs button.active{border-color:#22d3ee;color:#67e8f9}.selector-note,.credential-hint{display:flex;align-items:flex-start;gap:8px;padding:10px;background:#0d2a3b;color:#8fb3c7;font-size:11px;line-height:1.55}.selector-note svg,.credential-hint svg{width:15px;flex:none;color:#22d3ee}.choice-cloud{display:flex;flex-wrap:wrap;gap:6px;min-height:72px;align-content:flex-start;padding:9px;border:1px solid #17384d;background:#061725}.choice-cloud button{font-size:10px}.choice-cloud button.active{border-color:#22d3ee;color:#67e8f9}.resolved-preview{display:flex;flex-direction:column;gap:4px;padding:9px;background:#0b2b25;color:#8dd6c9;font-size:11px}.resolved-preview span{word-break:break-all}.current-bundle{display:flex;gap:9px;padding:11px;background:#0d2a3b}.current-bundle svg{color:#22d3ee;flex:none}.current-bundle div{display:flex;flex-direction:column}.current-bundle span,.current-bundle small{color:#7894a8;font-size:10px;line-height:1.45}.current-bundle strong{color:#e5f4ff;margin:2px 0}.action-column button{display:flex;justify-content:center;align-items:center;gap:7px}.action-column button svg{width:15px}.action-column button.upgrade{border-color:#0e7490;background:#0e7490}.lifecycle-message{margin:0 20px 14px;padding:10px 12px;border:1px solid #155e75;background:#082b38;color:#a5f3fc}.lifecycle-error{margin:0 20px 14px}.result-panel{margin:0 20px 18px;border:1px solid #18384a;border-radius:12px;background:#081a27;overflow:hidden}.result-panel>header{display:flex;align-items:center;justify-content:space-between;padding:13px 15px;border-bottom:1px solid #123043}.result-panel h3{margin:0;color:#dcecff;font-size:13px}.result-panel header span{color:#6f91a5;font-size:11px}.table-scroll{overflow:auto}.result-panel table{width:100%;min-width:900px;border-collapse:collapse}.result-panel th,.result-panel td{padding:10px 12px;border-bottom:1px solid #123043;text-align:left;color:#9ab4c5;font-size:11px;vertical-align:middle}.result-panel th{background:#071824;color:#66879b;font-weight:500}.result-panel td strong,.result-panel td small{display:block}.result-panel td strong{color:#dcecff;font-size:12px}.result-panel td small{color:#59778c;margin-top:2px}.deployment-status{display:inline-block;padding:3px 7px;border-radius:5px;background:#26303b;color:#94a3b8}.deployment-status.success{background:#0b3a3b;color:#5eead4}.deployment-status.partial{background:#3c2a0c;color:#fbbf24}.deployment-status.failed{background:#3b1520;color:#fb7185}.success-count{color:#5eead4}.failed-count{color:#fb7185}.history-actions{display:flex;gap:6px}.icon-button{display:inline-flex;align-items:center;gap:4px;padding:5px 7px;font-size:10px}.icon-button svg{width:13px}.icon-button.retry{border-color:#a16207;color:#fbbf24}.assessment{padding:3px 6px;background:#26303b;color:#94a3b8}.assessment.current{background:#0b3a3b;color:#5eead4}.assessment.outdated{background:#3c2a0c;color:#fbbf24}.assessment.unknown{background:#312e3d;color:#c4b5fd}.assessment.failed{background:#3b1520;color:#fb7185}.agent-state{text-transform:uppercase}.agent-state.online{color:#2dd4bf}.agent-state.offline{color:#fb7185}.agent-state.pending{color:#fbbf24}.empty-cell{text-align:center!important;padding:28px!important;color:#64869a!important}.precheck-panel tr.failed{background:#21121a}.lifecycle-results{display:grid;gap:8px;padding:0 20px 20px}.lifecycle-results article,.deployment-detail-list article{display:grid;grid-template-columns:160px 70px 1fr;gap:10px;padding:11px 13px;border:1px solid #5b2230;border-radius:8px;background:#2d121a}.lifecycle-results article.ok,.deployment-detail-list article.ok{border-color:#155e75;background:#082b38}.lifecycle-results b,.deployment-detail-list b{color:#fb7185}.lifecycle-results article.ok b,.deployment-detail-list article.ok b{color:#2dd4bf}.lifecycle-results span,.deployment-detail-list span{color:#91adbd;word-break:break-all}.modal-backdrop{position:fixed;inset:0;z-index:120;display:flex;align-items:center;justify-content:center;padding:28px;background:rgba(2,10,18,.82)}.deployment-modal{display:flex;flex-direction:column;width:min(980px,96vw);max-height:88vh;border:1px solid #24506a;background:#07131f;box-shadow:0 24px 80px rgba(0,0,0,.55)}.deployment-modal>header{display:flex;justify-content:space-between;padding:16px 18px;border-bottom:1px solid #16384d}.deployment-modal h2{margin:4px 0;color:#e5f4ff}.deployment-modal header span,.deployment-modal header small{color:#6788a0}.deployment-modal header button{border:0;background:transparent;color:#9db6c7;cursor:pointer}.deployment-modal header svg{width:18px}.deployment-meta{display:grid;grid-template-columns:repeat(3,1fr);gap:8px;padding:14px 18px}.deployment-meta span{display:flex;flex-direction:column;gap:4px;padding:9px;background:#0a1c2a;color:#6f91a5;font-size:10px}.deployment-meta strong{color:#dcecff;font-size:12px}.deployment-message{margin:0 18px 12px;padding:10px;background:#35151f;color:#fda4af}.deployment-detail-list{display:grid;gap:8px;overflow:auto;padding:0 18px 16px}.deployment-modal footer{display:flex;justify-content:flex-end;gap:8px;padding:12px 18px;border-top:1px solid #16384d}.deployment-modal footer button{display:inline-flex;align-items:center;gap:6px}.deployment-modal footer svg{width:14px}@media(max-width:1250px){.lifecycle-summary{grid-template-columns:repeat(3,1fr)}.lifecycle-builder{grid-template-columns:1fr}.deployment-meta{grid-template-columns:repeat(2,1fr)}}@media(max-width:720px){.lifecycle-summary{grid-template-columns:repeat(2,1fr)}.mode-tabs{grid-template-columns:1fr}.lifecycle-results article,.deployment-detail-list article{grid-template-columns:1fr}.deployment-meta{grid-template-columns:1fr}}
</style>