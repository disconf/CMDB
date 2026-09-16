<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { AlertTriangle, CheckCircle2, ClipboardCheck, Clock3, Plus, RefreshCw, Search, Settings2, ShieldCheck, Ticket, Trash2, UserCheck, X } from 'lucide-vue-next'
import { assignAssetGovernance, bulkAssetGovernance, confirmAssetGovernance, createAssetGovernanceRule, deleteAssetGovernanceRule, evaluateAssetGovernance, evaluateAssetGovernanceRules, fetchAssetGovernanceIssues, fetchAssetGovernanceRules, retireAssetGovernance, updateAssetGovernanceRule } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'
import type { AssetGovernanceIssue, AssetGovernanceReport, AssetGovernanceRule, AssetGovernanceRuleInput } from '@/features/cmdb/types'
import ResponsibilityEditor from './ResponsibilityEditor.vue'

const emit = defineEmits<{ changed: []; select: [assetId: string] }>()
const auth = useAuthStore()
const issues = ref<AssetGovernanceIssue[]>([])
const rules = ref<AssetGovernanceRule[]>([])
const report = ref<AssetGovernanceReport>()
const busy = ref(false)
const error = ref('')
const message = ref('')
const selectedIds = ref<string[]>([])
const targetIds = ref<string[]>([])
const action = ref<'assign' | 'confirm' | 'retire' | ''>('')
const form = reactive({ owner: '', projectGroup: '', location: '', note: '' })
const filters = reactive({ query: '', issueType: '', severity: '', reviewStatus: '' })
const showRules = ref(false)
const showRuleForm = ref(false)
const editingRule = ref<AssetGovernanceRule>()
const ruleForm = reactive<AssetGovernanceRuleInput>({ name: '', enabled: true, priority: 10, matchAssetType: '', matchSource: '', matchIpCidr: '', matchProjectGroup: '', matchTag: '', owner: '', targetProjectGroup: '', location: '', note: '' })

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
const visibleIDs = computed(() => filtered.value.map(item => item.assetId))
const allVisibleSelected = computed(() => visibleIDs.value.length > 0 && visibleIDs.value.every(id => selectedIds.value.includes(id)))
const targetLabel = computed(() => targetIds.value.length === 1 ? (issues.value.find(item => item.assetId === targetIds.value[0])?.assetName || '资产责任治理') : `已选择 ${targetIds.value.length} 台资产`)

function issueLabel(value: string) { return ({ unowned: '未分配负责人', unassigned: '项目组待确认', 'missing-location': '缺少机房位置', 'long-offline': '长期离线', stale: '长期未更新' } as Record<string, string>)[value] || value }
function severityLabel(value: string) { return ({ critical: '严重', warning: '关注' } as Record<string, string>)[value] || value }
function reviewLabel(value: string) { return ({ pending: '待确认', confirmed: '已确认', retired: '已退役' } as Record<string, string>)[value] || value }
function actionLabel(value: typeof action.value) { return ({ assign: '分派负责人', confirm: '确认责任信息', retire: '资产退役' } as Record<string, string>)[value] || '责任治理' }
function ruleMatchLabel(rule: AssetGovernanceRule) {
  return [rule.matchAssetType && `类型 ${rule.matchAssetType}`, rule.matchIpCidr && `网段 ${rule.matchIpCidr}`, rule.matchSource && `来源 ${rule.matchSource}`, rule.matchProjectGroup && `项目组 ${rule.matchProjectGroup}`, rule.matchTag && `标签 ${rule.matchTag}`].filter(Boolean).join(' · ')
}
function toggleAll() {
  if (allVisibleSelected.value) selectedIds.value = selectedIds.value.filter(id => !visibleIDs.value.includes(id))
  else selectedIds.value = Array.from(new Set([...selectedIds.value, ...visibleIDs.value]))
}
function toggleSelect(assetId: string) {
  selectedIds.value = selectedIds.value.includes(assetId) ? selectedIds.value.filter(id => id !== assetId) : [...selectedIds.value, assetId]
}
function openAction(item: AssetGovernanceIssue, next: typeof action.value) {
  targetIds.value = [item.assetId]
  action.value = next
  form.owner = item.owner && item.owner !== '待分配' ? item.owner : ''
  form.projectGroup = item.projectGroup === '待确认' || item.projectGroup === '自动发现' ? '' : item.projectGroup
  form.location = item.location
  form.note = ''
}
function openBulk(next: typeof action.value) {
  if (!selectedIds.value.length) return
  targetIds.value = [...selectedIds.value]
  action.value = next
  form.owner = ''; form.projectGroup = ''; form.location = ''; form.note = ''
}
function closeAction() { targetIds.value = []; action.value = '' }
function resetRuleForm() {
  editingRule.value = undefined
  Object.assign(ruleForm, { name: '', enabled: true, priority: 10, matchAssetType: '', matchSource: '', matchIpCidr: '', matchProjectGroup: '', matchTag: '', owner: '', targetProjectGroup: '', location: '', note: '' })
}
function editRule(rule: AssetGovernanceRule) {
  editingRule.value = rule
  Object.assign(ruleForm, { name: rule.name, enabled: rule.enabled, priority: rule.priority, matchAssetType: rule.matchAssetType, matchSource: rule.matchSource, matchIpCidr: rule.matchIpCidr, matchProjectGroup: rule.matchProjectGroup, matchTag: rule.matchTag, owner: rule.owner, targetProjectGroup: rule.targetProjectGroup, location: rule.location, note: rule.note })
  showRuleForm.value = true
}
async function load() {
  busy.value = true; error.value = ''
  try { [issues.value, rules.value] = await Promise.all([fetchAssetGovernanceIssues(auth.token), fetchAssetGovernanceRules(auth.token)]) } catch (reason) { error.value = reason instanceof Error ? reason.message : '责任治理数据加载失败' } finally { busy.value = false }
}
async function evaluate() {
  busy.value = true; error.value = ''; message.value = ''
  try {
    report.value = await evaluateAssetGovernance(auth.token)
    message.value = `已评估 ${report.value.issues} 项，规则匹配 ${report.value.ruleMatched} 项，自动分派 ${report.value.ruleAssigned} 项，新增工单 ${report.value.tickets} 个`
    selectedIds.value = []; await load(); emit('changed')
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '责任治理评估失败' } finally { busy.value = false }
}
async function submitAction() {
  if (!targetIds.value.length || !action.value) return
  if (action.value === 'assign' && !form.owner.trim()) { error.value = '负责人不能为空'; return }
  busy.value = true; error.value = ''; message.value = ''
  try {
    if (targetIds.value.length === 1) {
      const assetId = targetIds.value[0]
      if (action.value === 'assign') await assignAssetGovernance(auth.token, assetId, { owner: form.owner.trim(), projectGroup: form.projectGroup.trim(), location: form.location.trim(), note: form.note.trim() })
      else if (action.value === 'confirm') await confirmAssetGovernance(auth.token, assetId, form.note.trim())
      else await retireAssetGovernance(auth.token, assetId, form.note.trim())
    } else {
      const result = await bulkAssetGovernance(auth.token, { assetIds: targetIds.value, action: action.value, owner: form.owner.trim(), projectGroup: form.projectGroup.trim(), location: form.location.trim(), note: form.note.trim() })
      message.value = `批量${actionLabel(action.value)}完成：成功 ${result.succeeded} 台，失败 ${result.failed} 台`
      if (result.failed) error.value = result.items.filter(item => !item.success).slice(0, 5).map(item => `${item.assetName || item.assetId}: ${item.error}`).join('；')
    }
    if (!message.value) message.value = `${actionLabel(action.value)}已完成`
    selectedIds.value = []; closeAction(); await load(); emit('changed')
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '治理操作失败' } finally { busy.value = false }
}
async function saveRule() {
  if (!ruleForm.name.trim() || !ruleForm.owner.trim()) { error.value = '规则名称和负责人不能为空'; return }
  busy.value = true; error.value = ''
  try {
    if (editingRule.value) await updateAssetGovernanceRule(auth.token, editingRule.value.id, { ...ruleForm })
    else await createAssetGovernanceRule(auth.token, { ...ruleForm })
    message.value = editingRule.value ? '治理规则已更新' : '治理规则已创建'
    showRuleForm.value = false; resetRuleForm(); rules.value = await fetchAssetGovernanceRules(auth.token)
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '治理规则保存失败' } finally { busy.value = false }
}
async function removeRule(rule: AssetGovernanceRule) {
  busy.value = true; error.value = ''
  try { await deleteAssetGovernanceRule(auth.token, rule.id); rules.value = await fetchAssetGovernanceRules(auth.token); message.value = `规则“${rule.name}”已删除` } catch (reason) { error.value = reason instanceof Error ? reason.message : '规则删除失败' } finally { busy.value = false }
}
async function applyRules() {
  busy.value = true; error.value = ''; message.value = ''
  try { const result = await evaluateAssetGovernanceRules(auth.token); message.value = `规则评估完成：匹配 ${result.matched} 项，自动分派 ${result.assigned} 项，跳过 ${result.skipped} 项`; await load(); emit('changed') } catch (reason) { error.value = reason instanceof Error ? reason.message : '规则执行失败' } finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <section class="governance-panel">
    <header class="governance-head">
      <div><ShieldCheck/><span><strong>资产责任治理</strong><small>负责人、项目组、位置、长期离线和生命周期确认</small></span></div>
      <div><button :disabled="busy" @click="showRules=true"><Settings2/>治理规则</button><button :disabled="busy" @click="load"><RefreshCw :class="{spin:busy}"/>刷新</button><button class="primary" :disabled="busy" @click="evaluate"><ClipboardCheck/>开始评估</button></div>
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
      <div v-if="selectedIds.length" class="bulk-actions"><strong>已选 {{ selectedIds.length }} 台</strong><button @click="openBulk('assign')">批量分派</button><button @click="openBulk('confirm')">批量确认</button><button class="danger" @click="openBulk('retire')">批量退役</button><button @click="selectedIds=[]">取消选择</button></div>
    </div>

    <div class="governance-table">
      <div class="governance-row governance-table-head"><label class="check"><input type="checkbox" :checked="allVisibleSelected" @change="toggleAll"></label><span>资产</span><span>治理问题</span><span>级别</span><span>责任信息</span><span>位置</span><span>最近发现</span><span>确认状态</span><span>操作</span></div>
      <article v-for="item in filtered" :key="item.assetId" class="governance-row">
        <label class="check"><input type="checkbox" :checked="selectedIds.includes(item.assetId)" @change="toggleSelect(item.assetId)"></label>
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

    <div v-if="targetIds.length && action" class="governance-modal" @click.self="closeAction">
      <form @submit.prevent="submitAction">
        <header><div><span>{{ actionLabel(action) }}</span><h3>{{ targetLabel }}</h3><p>{{ targetIds.length > 1 ? `将按同一组责任信息处理 ${targetIds.length} 台资产` : '单台资产治理操作' }}</p></div><button type="button" aria-label="关闭" @click="closeAction"><X/></button></header>
        <section class="action-summary"><AlertTriangle v-if="action === 'retire'"/><ClipboardCheck v-else/><p>{{ action === 'assign' ? '补充负责人、项目组和位置后，资产责任信息将同步写入 CMDB。' : action === 'confirm' ? '确认后该资产停止自动生成责任治理工单和告警，但仍保留在资产台账中。' : '退役后资产状态将变为离线并添加“退役”标签，后续治理评估不再纳入。' }}</p></section>
        <ResponsibilityEditor :owner="form.owner" :project-group="form.projectGroup" :location="form.location" :show-assignment="action === 'assign'" :show-location="action === 'assign'" :note="form.note" :show-note="true" @update:owner="form.owner = $event" @update:project-group="form.projectGroup = $event" @update:location="form.location = $event" @update:note="form.note = $event" />
        <footer><button type="button" @click="closeAction">取消</button><button class="primary" :disabled="busy" type="submit">{{ busy ? '处理中…' : actionLabel(action) }}</button></footer>
      </form>
    </div>

    <div v-if="showRules" class="governance-modal" @click.self="showRules=false">
      <section class="rules-panel">
        <header><div><span>AUTOMATION RULES</span><h3>资产责任治理规则</h3><p>按资产类型、来源、网段、项目组或标签自动分派负责人。</p></div><button aria-label="关闭" @click="showRules=false"><X/></button></header>
        <div class="rules-toolbar"><button class="primary" @click="resetRuleForm();showRuleForm=true"><Plus/>新建规则</button><button :disabled="busy || !rules.length" @click="applyRules"><ClipboardCheck/>立即执行规则</button></div>
        <div v-if="showRuleForm" class="rule-form">
          <label><span>规则名称 *</span><input v-model="ruleForm.name" placeholder="例如：68 网段服务器分派"></label>
          <label><span>优先级</span><input v-model.number="ruleForm.priority" type="number" min="1" max="100"></label>
          <label><span>资产类型</span><input v-model="ruleForm.matchAssetType" placeholder="physical-server / virtual-machine"></label>
          <label><span>来源</span><input v-model="ruleForm.matchSource" placeholder="node-exporter / agent / ssh"></label>
          <label><span>IP 网段</span><input v-model="ruleForm.matchIpCidr" placeholder="172.28.68.0/24"></label>
          <label><span>原项目组</span><input v-model="ruleForm.matchProjectGroup" placeholder="自动发现"></label>
          <label><span>资产标签</span><input v-model="ruleForm.matchTag" placeholder="核心 / 测试"></label>
          <label><span>分派负责人 *</span><input v-model="ruleForm.owner" placeholder="姓名或运维团队"></label>
          <label><span>目标项目组</span><input v-model="ruleForm.targetProjectGroup" placeholder="核心系统组"></label>
          <label><span>目标位置</span><input v-model="ruleForm.location" placeholder="上海一号机房 / A03"></label>
          <label class="wide"><span>规则说明</span><input v-model="ruleForm.note" placeholder="自动分派依据"></label>
          <label class="enabled"><input v-model="ruleForm.enabled" type="checkbox"><span>启用规则</span></label>
          <div class="rule-form-actions"><button type="button" @click="showRuleForm=false;resetRuleForm()">取消</button><button class="primary" :disabled="busy" @click="saveRule">{{ editingRule ? '保存规则' : '创建规则' }}</button></div>
        </div>
        <div class="rules-list">
          <article v-for="rule in rules" :key="rule.id">
            <div><strong>{{ rule.name }}</strong><small>{{ ruleMatchLabel(rule) || '未设置匹配条件' }}</small></div>
            <div><span>{{ rule.owner }}</span><small>{{ rule.targetProjectGroup || '沿用原项目组' }} · 优先级 {{ rule.priority }}</small></div>
            <b :data-enabled="rule.enabled">{{ rule.enabled ? '启用' : '停用' }}</b>
            <div class="rule-actions"><button @click="editRule(rule)">编辑</button><button class="danger" @click="removeRule(rule)"><Trash2/>删除</button></div>
          </article>
          <div v-if="!rules.length" class="governance-empty"><Settings2/><strong>尚未配置自动化规则</strong><span>可先按网段或来源创建规则，再批量分派负责人。</span></div>
        </div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.governance-panel{margin-top:14px;border:1px solid var(--line,#24314d);background:var(--panel,#0d1526);border-radius:12px;overflow:hidden}.governance-head{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:15px 18px;border-bottom:1px solid var(--line,#24314d)}.governance-head>div{display:flex;align-items:center;gap:10px}.governance-head>div:first-child svg{width:22px;color:#48c8e8}.governance-head span{display:grid;gap:3px}.governance-head strong{font-size:14px;color:#e3f3fb}.governance-head small{font-size:11px;color:#7890a8}.governance-head>div:last-child{display:flex;gap:7px}.governance-panel button{display:inline-flex;align-items:center;justify-content:center;gap:5px;border:1px solid #28536c;background:#10273a;color:#b7d3e3;padding:7px 10px;border-radius:6px;font-size:12px;cursor:pointer}.governance-panel button.primary{background:#0b6da5;border-color:#1a93c8;color:#fff}.governance-panel button.danger{border-color:#7c3542;background:#351923;color:#ff9aa8}.governance-panel button:disabled{opacity:.5;cursor:not-allowed}.governance-panel button svg{width:14px;height:14px}.governance-message,.governance-error{margin:10px 18px 0;padding:9px 11px;border-radius:6px;font-size:12px}.governance-message{background:rgba(72,216,154,.1);color:#5bdfa1;border:1px solid rgba(72,216,154,.2)}.governance-error{background:rgba(255,82,104,.1);color:#ff8998;border:1px solid rgba(255,82,104,.2)}.governance-stats{display:grid;grid-template-columns:repeat(4,1fr);gap:9px;padding:14px 18px 4px}.governance-stats article{display:flex;align-items:center;gap:9px;padding:11px;background:#081a2b;border:1px solid #173b55;border-radius:8px}.governance-stats svg{width:20px;color:#48c8e8}.governance-stats article.warning svg{color:#ffc167}.governance-stats span{display:grid;gap:2px;color:#7890a8;font-size:11px}.governance-stats strong{font-size:20px;color:#e3f3fb;font-weight:600}.governance-toolbar{display:flex;align-items:center;gap:8px;padding:12px 18px;flex-wrap:wrap}.governance-toolbar select,.governance-toolbar input{height:34px;border:1px solid #28536c;background:#081a2b;color:#c4dfed;border-radius:6px;padding:0 9px;font-size:12px}.governance-toolbar .search{display:flex;align-items:center;gap:6px;flex:1;min-width:220px;height:34px;border:1px solid #28536c;background:#081a2b;border-radius:6px;padding:0 9px}.governance-toolbar .search svg{width:15px;color:#64839a}.governance-toolbar .search input{flex:1;height:30px;border:0;background:transparent;outline:none}.result-count{margin-left:auto;color:#7890a8;font-size:11px;white-space:nowrap}.bulk-actions{display:flex;align-items:center;gap:6px;padding:5px 7px;border:1px solid #246c98;background:#0d3048;border-radius:7px}.bulk-actions strong{font-size:11px;color:#9ddaf0}.bulk-actions button{padding:4px 7px;font-size:10px}.governance-table{padding:0 18px 18px;overflow:auto}.governance-row{display:grid;grid-template-columns:28px minmax(170px,1.3fr) minmax(190px,1.5fr) 72px minmax(130px,1fr) minmax(135px,1.15fr) 140px 105px 175px;gap:10px;align-items:center;min-width:1210px;padding:10px;border-bottom:1px solid #17324a;font-size:12px;color:#b8cfdd}.governance-table-head{background:#081a2b;color:#69889d;font-size:11px;border-radius:7px 7px 0 0}.check{display:grid;place-items:center}.check input{accent-color:#1a93c8}.governance-row>span{min-width:0}.governance-row strong,.governance-row small,.governance-row em{display:block}.governance-row small{margin-top:3px;color:#69889d;font-size:10px}.governance-row em{margin-top:4px;color:#48c8e8;font-size:10px;font-style:normal}.asset-cell button{padding:0;border:0;background:transparent;color:#d9eef8;font-weight:600;cursor:pointer}.asset-cell button:hover{color:#48c8e8}.severity,.review-state{display:inline-block;padding:3px 6px;border-radius:4px;font-size:10px;font-weight:500}.severity[data-severity=critical]{background:rgba(255,82,104,.14);color:#ff8998}.severity[data-severity=warning]{background:rgba(255,193,103,.14);color:#ffc167}.review-state[data-state=confirmed]{background:rgba(72,216,154,.12);color:#5bdfa1}.review-state[data-state=pending]{background:rgba(72,200,232,.12);color:#48c8e8}.governance-actions{display:flex;gap:5px}.governance-actions button{padding:5px 7px;font-size:10px}.governance-empty{display:grid;place-items:center;gap:7px;padding:38px;color:#69889d;text-align:center}.governance-empty svg{width:28px;color:#48c8e8}.governance-empty strong{color:#b8cfdd}.governance-modal{position:fixed;inset:0;z-index:90;background:rgba(0,8,16,.78);display:grid;place-items:center}.governance-modal form,.rules-panel{width:min(760px,94vw);max-height:90vh;overflow:auto;background:#081a2b;border:1px solid #246c98;border-radius:10px}.governance-modal form>header,.rules-panel>header{display:flex;justify-content:space-between;padding:15px 18px;border-bottom:1px solid #174d78}.governance-modal header span{font-size:10px;color:#48c8e8;letter-spacing:1px}.governance-modal h3{margin:4px 0;color:#e3f3fb}.governance-modal p{margin:0;color:#7890a8;font-size:11px}.governance-modal header button{border:0;background:transparent;padding:3px}.action-summary{display:flex;gap:9px;margin:14px 18px 0;padding:10px;background:#10273a;border:1px solid #28536c;border-radius:7px}.action-summary svg{width:18px;color:#ffc167;flex:none}.action-summary p{margin:0;line-height:1.6}.action-form{display:grid;grid-template-columns:1fr 1fr;gap:10px;padding:16px 18px}.action-form label,.rule-form label{display:grid;gap:5px}.action-form label.wide{grid-column:1/-1}.action-form span,.rule-form span{color:#7890a8;font-size:11px}.action-form input,.action-form textarea,.rule-form input{border:1px solid #28536c;background:#031525;color:#d5edf8;border-radius:6px;padding:8px 9px;font:inherit;font-size:12px}.governance-modal footer{display:flex;justify-content:flex-end;gap:7px;padding:12px 18px;border-top:1px solid #173d58}.rules-toolbar{display:flex;justify-content:flex-end;gap:7px;padding:12px 18px 0}.rule-form{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin:12px 18px;padding:12px;border:1px solid #28536c;background:#10273a;border-radius:8px}.rule-form label.wide{grid-column:1/-1}.rule-form label.enabled{display:flex;align-items:center;gap:6px}.rule-form-actions{grid-column:1/-1;display:flex;justify-content:flex-end;gap:7px}.rules-list{display:grid;gap:8px;padding:12px 18px 18px}.rules-list article{display:grid;grid-template-columns:1.5fr 1fr 60px auto;gap:10px;align-items:center;padding:10px;border:1px solid #17415e;background:#061a2c;border-radius:7px}.rules-list strong,.rules-list small{display:block}.rules-list small{margin-top:3px;color:#69889d;font-size:10px}.rules-list b{padding:3px 6px;background:rgba(72,216,154,.12);color:#5bdfa1;border-radius:4px;font-size:10px;text-align:center}.rules-list b[data-enabled=false]{background:rgba(255,193,103,.12);color:#ffc167}.rule-actions{display:flex;gap:5px}.rule-actions button{padding:5px 7px;font-size:10px}.spin{animation:spin .9s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}@media(max-width:900px){.governance-stats{grid-template-columns:1fr 1fr}.governance-head{align-items:flex-start;flex-direction:column}.result-count{margin-left:0}.action-form,.rule-form{grid-template-columns:1fr}.action-form label.wide,.rule-form label.wide,.rule-form-actions{grid-column:auto}.rules-list article{grid-template-columns:1fr}.bulk-actions{width:100%}}
</style>
