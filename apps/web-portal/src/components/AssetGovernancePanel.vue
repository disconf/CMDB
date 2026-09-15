<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { AlertTriangle, CheckCircle2, ClipboardCheck, Clock3, RefreshCw, Search, ShieldCheck, Ticket, UserCheck, X } from 'lucide-vue-next'
import { assignAssetGovernance, confirmAssetGovernance, evaluateAssetGovernance, fetchAssetGovernanceIssues, retireAssetGovernance } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'
import type { AssetGovernanceIssue, AssetGovernanceReport } from '@/features/cmdb/types'

const emit = defineEmits<{ changed: []; select: [assetId: string] }>()
const auth = useAuthStore()
const issues = ref<AssetGovernanceIssue[]>([])
const report = ref<AssetGovernanceReport>()
const busy = ref(false)
const error = ref('')
const message = ref('')
const selected = ref<AssetGovernanceIssue>()
const action = ref<'assign' | 'confirm' | 'retire' | ''>('')
const form = reactive({ owner: '', projectGroup: '', location: '', note: '' })
const filters = reactive({ query: '', issueType: '', severity: '', reviewStatus: '' })

const filtered = computed(() => issues.value.filter(item => {
  const query = filters.query.trim().toLowerCase()
  const queryMatch = !query || [item.assetName, item.assetId, item.ip, item.owner, item.projectGroup, item.location].some(value => (value || '').toLowerCase().includes(query))
  return queryMatch && (!filters.issueType || item.issueType === filters.issueType) && (!filters.severity || item.severity === filters.severity) && (!filters.reviewStatus || item.reviewStatus === filters.reviewStatus)
}))
const criticalCount = computed(() => issues.value.filter(item => item.severity === 'critical').length)
const warningCount = computed(() => issues.value.filter(item => item.severity === 'warning').length)
const pendingCount = computed(() => issues.value.filter(item => item.reviewStatus === 'pending').length)
const ticketCount = computed(() => issues.value.filter(item => item.ticketId).length)
const issueTypes = computed(() => Array.from(new Set(issues.value.map(item => item.issueType))))

function issueLabel(value: string) { return ({ unowned: '未分配负责人', unassigned: '项目组待确认', 'missing-location': '缺少机房位置', 'long-offline': '长期离线', stale: '长期未更新' } as Record<string, string>)[value] || value }
function severityLabel(value: string) { return ({ critical: '严重', warning: '关注' } as Record<string, string>)[value] || value }
function reviewLabel(value: string) { return ({ pending: '待确认', confirmed: '已确认', retired: '已退役' } as Record<string, string>)[value] || value }
function actionLabel(value: typeof action.value) { return ({ assign: '分派负责人', confirm: '确认责任信息', retire: '资产退役' } as Record<string, string>)[value] || '责任治理' }
function openAction(item: AssetGovernanceIssue, next: typeof action.value) {
  selected.value = item
  action.value = next
  form.owner = item.owner && item.owner !== '待分配' ? item.owner : ''
  form.projectGroup = item.projectGroup === '待确认' || item.projectGroup === '自动发现' ? '' : item.projectGroup
  form.location = item.location
  form.note = ''
}
function closeAction() { selected.value = undefined; action.value = '' }
async function load() {
  busy.value = true; error.value = ''
  try { issues.value = await fetchAssetGovernanceIssues(auth.token) } catch (reason) { error.value = reason instanceof Error ? reason.message : '责任治理数据加载失败' } finally { busy.value = false }
}
async function evaluate() {
  busy.value = true; error.value = ''; message.value = ''
  try {
    report.value = await evaluateAssetGovernance(auth.token)
    message.value = `已评估 ${report.value.issues} 项，新增工单 ${report.value.tickets} 个，同步告警 ${report.value.alerts} 条`
    await load()
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '责任治理评估失败' } finally { busy.value = false }
}
async function submitAction() {
  if (!selected.value || !action.value) return
  if (action.value === 'assign' && !form.owner.trim()) { error.value = '负责人不能为空'; return }
  busy.value = true; error.value = ''; message.value = ''
  try {
    if (action.value === 'assign') {
      await assignAssetGovernance(auth.token, selected.value.assetId, { owner: form.owner.trim(), projectGroup: form.projectGroup.trim(), location: form.location.trim(), note: form.note.trim() })
      message.value = `已将 ${selected.value.assetName} 分派给 ${form.owner.trim()}`
    } else if (action.value === 'confirm') {
      await confirmAssetGovernance(auth.token, selected.value.assetId, form.note.trim())
      message.value = `${selected.value.assetName} 的责任信息已确认`
    } else {
      await retireAssetGovernance(auth.token, selected.value.assetId, form.note.trim())
      message.value = `${selected.value.assetName} 已标记退役`
    }
    closeAction(); await load(); emit('changed')
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '治理操作失败' } finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <section class="governance-panel">
    <header class="governance-head">
      <div><ShieldCheck/><span><strong>资产责任治理</strong><small>负责人、项目组、位置、长期离线和生命周期确认</small></span></div>
      <div><button :disabled="busy" @click="load"><RefreshCw :class="{spin:busy}"/>刷新</button><button class="primary" :disabled="busy" @click="evaluate"><ClipboardCheck/>开始评估</button></div>
    </header>

    <div v-if="message" class="governance-message">{{ message }}</div>
    <div v-if="error" class="governance-error">{{ error }}</div>
    <div v-if="report?.errors?.length" class="governance-error">部分动作未完成：{{ report.errors.join('；') }}</div>

    <div class="governance-stats">
      <article><AlertTriangle/><span>严重问题<strong>{{ criticalCount }}</strong></span></article>
      <article class="warning"><Clock3/><span>关注问题<strong>{{ warningCount }}</strong></span></article>
      <article><UserCheck/><span>待确认<strong>{{ pendingCount }}</strong></span></article>
      <article><Ticket/><span>已生成工单<strong>{{ ticketCount }}</strong></span></article>
    </div>

    <div class="governance-toolbar">
      <label class="search"><Search/><input v-model="filters.query" placeholder="搜索资产、IP、负责人、项目组或位置"></label>
      <select v-model="filters.issueType"><option value="">全部问题</option><option v-for="type in issueTypes" :key="type" :value="type">{{ issueLabel(type) }}</option></select>
      <select v-model="filters.severity"><option value="">全部级别</option><option value="critical">严重</option><option value="warning">关注</option></select>
      <select v-model="filters.reviewStatus"><option value="">全部确认状态</option><option value="pending">待确认</option><option value="confirmed">已确认</option></select>
      <span class="result-count">共 {{ filtered.length }} 项</span>
    </div>

    <div class="governance-table">
      <div class="governance-row governance-table-head"><span>资产</span><span>治理问题</span><span>级别</span><span>责任信息</span><span>位置</span><span>最近发现</span><span>确认状态</span><span>操作</span></div>
      <article v-for="item in filtered" :key="item.assetId" class="governance-row">
        <span class="asset-cell"><button @click="emit('select', item.assetId)">{{ item.assetName }}</button><small>{{ item.assetTypeName }} · {{ item.ip || '无 IP' }}</small></span>
        <span><strong>{{ issueLabel(item.issueType) }}</strong><small>{{ item.detail }}<template v-if="item.staleDays"> · {{ item.staleDays }} 天</template></small><em v-if="item.ticketId">工单 {{ item.ticketId }}</em></span>
        <span><b class="severity" :data-severity="item.severity">{{ severityLabel(item.severity) }}</b></span>
        <span><strong>{{ item.owner || '未分配' }}</strong><small>{{ item.projectGroup || '项目组待确认' }}</small></span>
        <span>{{ item.location || '未设置' }}</span>
        <span>{{ item.lastSeenAt || '未知' }}</span>
        <span><b class="review-state" :data-state="item.reviewStatus">{{ reviewLabel(item.reviewStatus) }}</b><small v-if="item.reviewedBy">{{ item.reviewedBy }} · {{ item.reviewedAt }}</small></span>
        <span class="governance-actions"><button @click="openAction(item, 'assign')">分派</button><button @click="openAction(item, 'confirm')">确认</button><button class="danger" @click="openAction(item, 'retire')">退役</button></span>
      </article>
      <div v-if="!filtered.length && !busy" class="governance-empty"><CheckCircle2/><strong>当前没有匹配的责任治理问题</strong><span>可调整筛选条件，或点击“开始评估”检查最新资产数据。</span></div>
      <div v-if="busy" class="governance-empty"><RefreshCw class="spin"/><strong>正在读取责任治理数据…</strong></div>
    </div>

    <div v-if="selected && action" class="governance-modal" @click.self="closeAction">
      <form @submit.prevent="submitAction">
        <header><div><span>{{ actionLabel(action) }}</span><h3>{{ selected.assetName }}</h3><p>{{ selected.ip || '无 IP' }} · {{ selected.projectGroup || '项目组待确认' }}</p></div><button type="button" aria-label="关闭" @click="closeAction"><X/></button></header>
        <section class="action-summary"><AlertTriangle v-if="action === 'retire'"/><ClipboardCheck v-else/><p>{{ action === 'assign' ? '补充负责人、项目组和位置后，资产责任信息将同步写入 CMDB。' : action === 'confirm' ? '确认后该资产停止自动生成责任治理工单和告警，但仍保留在资产台账中。' : '退役后资产状态将变为离线并添加“退役”标签，后续治理评估不再纳入。' }}</p></section>
        <div class="action-form">
          <label v-if="action === 'assign'"><span>负责人 *</span><input v-model="form.owner" placeholder="姓名或运维团队"></label>
          <label v-if="action === 'assign'"><span>项目组</span><input v-model="form.projectGroup" placeholder="例如：核心系统组"></label>
          <label v-if="action === 'assign'" class="wide"><span>机房 / 位置</span><input v-model="form.location" placeholder="例如：上海一号机房 / A03-12"></label>
          <label class="wide"><span>治理说明</span><textarea v-model="form.note" rows="3" placeholder="记录确认依据、分派说明或退役原因"></textarea></label>
        </div>
        <footer><button type="button" @click="closeAction">取消</button><button class="primary" :disabled="busy" type="submit">{{ busy ? '处理中…' : actionLabel(action) }}</button></footer>
      </form>
    </div>
  </section>
</template>

<style scoped>
.governance-panel{margin-top:14px;border:1px solid var(--line,#24314d);background:var(--panel,#0d1526);border-radius:12px;overflow:hidden}.governance-head{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:15px 18px;border-bottom:1px solid var(--line,#24314d)}.governance-head>div{display:flex;align-items:center;gap:10px}.governance-head>div:first-child svg{width:22px;color:#48c8e8}.governance-head span{display:grid;gap:3px}.governance-head strong{font-size:14px;color:#e3f3fb}.governance-head small{font-size:11px;color:#7890a8}.governance-head>div:last-child{display:flex;gap:7px}.governance-panel button{display:inline-flex;align-items:center;justify-content:center;gap:5px;border:1px solid #28536c;background:#10273a;color:#b7d3e3;padding:7px 10px;border-radius:6px;font-size:12px;cursor:pointer}.governance-panel button.primary{background:#0b6da5;border-color:#1a93c8;color:#fff}.governance-panel button.danger{border-color:#7c3542;background:#351923;color:#ff9aa8}.governance-panel button:disabled{opacity:.5;cursor:not-allowed}.governance-panel button svg{width:14px;height:14px}.governance-message,.governance-error{margin:10px 18px 0;padding:9px 11px;border-radius:6px;font-size:12px}.governance-message{background:rgba(72,216,154,.1);color:#5bdfa1;border:1px solid rgba(72,216,154,.2)}.governance-error{background:rgba(255,82,104,.1);color:#ff8998;border:1px solid rgba(255,82,104,.2)}.governance-stats{display:grid;grid-template-columns:repeat(4,1fr);gap:9px;padding:14px 18px 4px}.governance-stats article{display:flex;align-items:center;gap:9px;padding:11px;background:#081a2b;border:1px solid #173b55;border-radius:8px}.governance-stats svg{width:20px;color:#48c8e8}.governance-stats article.warning svg{color:#ffc167}.governance-stats span{display:grid;gap:2px;color:#7890a8;font-size:11px}.governance-stats strong{font-size:20px;color:#e3f3fb;font-weight:600}.governance-toolbar{display:flex;align-items:center;gap:8px;padding:12px 18px}.governance-toolbar select,.governance-toolbar input{height:34px;border:1px solid #28536c;background:#081a2b;color:#c4dfed;border-radius:6px;padding:0 9px;font-size:12px}.governance-toolbar .search{display:flex;align-items:center;gap:6px;flex:1;min-width:220px;height:34px;border:1px solid #28536c;background:#081a2b;border-radius:6px;padding:0 9px}.governance-toolbar .search svg{width:15px;color:#64839a}.governance-toolbar .search input{flex:1;height:30px;border:0;background:transparent;outline:none}.result-count{margin-left:auto;color:#7890a8;font-size:11px;white-space:nowrap}.governance-table{padding:0 18px 18px;overflow:auto}.governance-row{display:grid;grid-template-columns:minmax(170px,1.3fr) minmax(190px,1.5fr) 72px minmax(130px,1fr) minmax(135px,1.15fr) 140px 105px 175px;gap:10px;align-items:center;min-width:1180px;padding:10px;border-bottom:1px solid #17324a;font-size:12px;color:#b8cfdd}.governance-table-head{background:#081a2b;color:#69889d;font-size:11px;border-radius:7px 7px 0 0}.governance-row>span{min-width:0}.governance-row strong,.governance-row small,.governance-row em{display:block}.governance-row small{margin-top:3px;color:#69889d;font-size:10px}.governance-row em{margin-top:4px;color:#48c8e8;font-size:10px;font-style:normal}.asset-cell button{padding:0;border:0;background:transparent;color:#d9eef8;font-weight:600;cursor:pointer}.asset-cell button:hover{color:#48c8e8}.severity,.review-state{display:inline-block;padding:3px 6px;border-radius:4px;font-size:10px;font-weight:500}.severity[data-severity=critical]{background:rgba(255,82,104,.14);color:#ff8998}.severity[data-severity=warning]{background:rgba(255,193,103,.14);color:#ffc167}.review-state[data-state=confirmed]{background:rgba(72,216,154,.12);color:#5bdfa1}.review-state[data-state=pending]{background:rgba(72,200,232,.12);color:#48c8e8}.governance-actions{display:flex;gap:5px}.governance-actions button{padding:5px 7px;font-size:10px}.governance-empty{display:grid;place-items:center;gap:7px;padding:38px;color:#69889d;text-align:center}.governance-empty svg{width:28px;color:#48c8e8}.governance-empty strong{color:#b8cfdd}.governance-modal{position:fixed;inset:0;z-index:90;background:rgba(0,8,16,.78);display:grid;place-items:center}.governance-modal form{width:min(620px,92vw);background:#081a2b;border:1px solid #246c98;border-radius:10px;overflow:hidden}.governance-modal form>header{display:flex;justify-content:space-between;padding:15px 18px;border-bottom:1px solid #174d78}.governance-modal header span{font-size:10px;color:#48c8e8;letter-spacing:1px}.governance-modal h3{margin:4px 0;color:#e3f3fb}.governance-modal p{margin:0;color:#7890a8;font-size:11px}.governance-modal header button{border:0;background:transparent;padding:3px}.action-summary{display:flex;gap:9px;margin:14px 18px 0;padding:10px;background:#10273a;border:1px solid #28536c;border-radius:7px}.action-summary svg{width:18px;color:#ffc167;flex:none}.action-summary p{margin:0;line-height:1.6}.action-form{display:grid;grid-template-columns:1fr 1fr;gap:10px;padding:16px 18px}.action-form label{display:grid;gap:5px}.action-form label.wide{grid-column:1/-1}.action-form span{color:#7890a8;font-size:11px}.action-form input,.action-form textarea{border:1px solid #28536c;background:#031525;color:#d5edf8;border-radius:6px;padding:8px 9px;font:inherit;font-size:12px}.governance-modal footer{display:flex;justify-content:flex-end;gap:7px;padding:12px 18px;border-top:1px solid #173d58}.spin{animation:spin .9s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}@media(max-width:900px){.governance-stats{grid-template-columns:1fr 1fr}.governance-head{align-items:flex-start;flex-direction:column}.governance-toolbar{flex-wrap:wrap}.result-count{margin-left:0}.action-form{grid-template-columns:1fr}.action-form label.wide{grid-column:auto}}
</style>
