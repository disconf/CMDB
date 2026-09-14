<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { Ban, Clock3, FileUp, History, KeyRound, Pause, Play, RefreshCw, RotateCcw, Search, ShieldAlert, ShieldCheck, Terminal as TerminalIcon, Upload, X } from 'lucide-vue-next'
import { Terminal as XTerm } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { listCredentials, type Credential } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'

type Asset = { id:string; name:string; ip:string; projectGroup:string; status:string }
type HostKey = { id:string; assetId:string; host:string; port:number; keyType:string; fingerprint:string; addedBy:string; createdAt:string }
type HostKeyProbe = { assetId:string; host:string; port:number; keyType:string; fingerprint:string; publicKey:string; trusted:boolean; changed:boolean; trustedFingerprint:string }
type Grant = { id:string; subjectType:string; subject:string; assetId:string; projectGroup:string; permissions:string[]; enabled:boolean; createdBy:string }
type SessionLog = { time:string; level:string; kind?:string; message:string; durationMs?:number }
type Collaborator = { username:string; access:'control'|'readonly'; addedBy:string; addedAt:string }
type Session = { id:string; assetId:string; assetName:string; ip:string; credentialId:string; operator:string; status:string; createdAt:string; expiresAt:string; closedAt?:string; collaborators:Collaborator[]; accessMode?:'control'|'readonly'; activeConnections:number; controllerOnline:boolean; logs:SessionLog[] }
type TerminalEvent = { sequence:number; direction:string; data:string; createdAt:string }
type TerminalRecording = { events:TerminalEvent[]; truncated:boolean }
type TerminalApproval = { id:string; sessionId:string; assetId:string; assetName:string; ip:string; operator:string; command:string; reason:string; status:string; requestedAt:string; expiresAt:string; decidedAt?:string; approver?:string; decisionComment?:string }
type TerminalSocketMessage = { type:string; data?:string; message?:string; access?:'control'|'readonly'; cols?:number; rows?:number; approval?:TerminalApproval }
type SecurityPolicy = { id:string; name:string; subjectType:'global'|'user'|'role'; subject:string; enabled:boolean; priority:number; maxSessionMinutes:number; maxConcurrentSessions:number; recordingRetentionDays:number; fileTransferEnabled:boolean; uploadMaxMB:number; downloadMaxMB:number; allowedUploadPaths:string[]; allowedDownloadPaths:string[]; allowControlCollaborators:boolean; approvalMode:'risk'|'all'; approvalTtlMinutes:number; createdBy:string; updatedBy:string; updatedAt:string }

const auth = useAuthStore()
const assets = ref<Asset[]>([])
const grants = ref<Grant[]>([])
const sessions = ref<Session[]>([])
const history = ref<Session[]>([])
const credentials = ref<Credential[]>([])
const hostKeys = ref<HostKey[]>([])
const hostProbe = ref<HostKeyProbe>()
const selectedAssetId = ref('')
const selectedSessionId = ref('')
const credentialId = ref('')
const command = ref('uptime')
const uploadPath = ref('/tmp/upload.bin')
const downloadPath = ref('/etc/os-release')
const uploadFile = ref<File>()
const busy = ref(false)
const error = ref('')
const message = ref('')
const historyQuery = ref('')
const historyStatus = ref('')
const replaySession = ref<Session>()
const replayVisible = ref(0)
const replayPlaying = ref(false)
const terminalMode = ref(false)
const terminalConnecting = ref(false)
const terminalConnected = ref(false)
const terminalError = ref('')
const recordingLoading = ref(false)
const recordingEvents = ref<TerminalEvent[]>([])
const terminalApprovals = ref<TerminalApproval[]>([])
const securityPolicies = ref<SecurityPolicy[]>([])
const effectivePolicy = ref<SecurityPolicy>()
const approvalComment = ref('')
const approvalBusyId = ref('')
const terminalAccess = ref<'control'|'readonly'>('control')
const watermarkTime = ref(new Date().toLocaleString('zh-CN', { hour12:false }))
const interactiveTerminalElement = ref<HTMLElement>()
const replayTerminalElement = ref<HTMLElement>()
let replayTimer: ReturnType<typeof setInterval> | undefined
let approvalPoll: ReturnType<typeof setInterval> | undefined
let watermarkTimer: ReturnType<typeof setInterval> | undefined
let recordingTimers: ReturnType<typeof setTimeout>[] = []
let terminalInstance: XTerm | undefined
let terminalFit: FitAddon | undefined
let terminalSocket: WebSocket | undefined
let replayTerminal: XTerm | undefined
let replayFit: FitAddon | undefined
const grantForm = ref({ subjectType:'user', subject:'admin', scopeType:'project', assetId:'', projectGroup:'', permissions:['terminal', 'file'] })
const collaboratorForm = ref({ username:'', access:'readonly' as 'control'|'readonly' })
const defaultPolicyForm = () => ({ id:'', name:'自定义远程运维策略', subjectType:'user' as 'global'|'user'|'role', subject:'', enabled:true, maxSessionMinutes:30, maxConcurrentSessions:20, recordingRetentionDays:180, fileTransferEnabled:true, uploadMaxMB:50, downloadMaxMB:25, allowedUploadPaths:'/**', allowedDownloadPaths:'/**', allowControlCollaborators:true, approvalMode:'risk' as 'risk'|'all', approvalTtlMinutes:10 })
const policyForm = ref(defaultPolicyForm())

const headers = computed(() => ({ Authorization:`Bearer ${auth.token}`, 'Content-Type':'application/json' }))
const selectedSession = computed(() => sessions.value.find(item => item.id === selectedSessionId.value))
const projectGroups = computed(() => [...new Set(assets.value.map(item => item.projectGroup).filter(Boolean))])
const visibleReplayLogs = computed(() => replaySession.value?.logs.slice(0, replayVisible.value) ?? [])
const filteredHistory = computed(() => {
  const query = historyQuery.value.trim().toLowerCase()
  return history.value.filter(item => {
    const matchesQuery = !query || `${item.assetName} ${item.ip} ${item.operator} ${item.id}`.toLowerCase().includes(query)
    const matchesStatus = !historyStatus.value || item.status === historyStatus.value
    return matchesQuery && matchesStatus
  })
})
const isPlatformAdmin = computed(() => auth.user?.roles.some(role => role === 'admin' || role === 'platform-admin') ?? false)
const canManageSelectedSession = computed(() => Boolean(selectedSession.value && (isPlatformAdmin.value || selectedSession.value.operator === auth.user?.username)))
const terminalReadOnly = computed(() => terminalAccess.value === 'readonly' || selectedSession.value?.accessMode === 'readonly')
const watermarkText = computed(() => `${auth.user?.username || 'unknown'} · ${watermarkTime.value} · ${selectedSession.value?.assetName || 'CMDB 远程运维'}`)
const pendingApprovalCount = computed(() => terminalApprovals.value.filter(item => item.status === 'pending').length)
const visibleApprovals = computed(() => terminalApprovals.value
  .filter(item => item.status === 'pending' || item.sessionId === selectedSessionId.value)
  .slice(0, 30))
const effectivePolicySummary = computed(() => effectivePolicy.value ? `${effectivePolicy.value.maxSessionMinutes} 分钟 · ${effectivePolicy.value.maxConcurrentSessions} 并发 · ${effectivePolicy.value.approvalMode === 'all' ? '全部命令审批' : '风险命令审批'}` : '策略加载中')

async function request<T>(url:string, init?:RequestInit) {
  const response = await fetch(url, { ...init, headers: init?.body instanceof FormData ? { Authorization:`Bearer ${auth.token}` } : headers.value })
  if (!response.ok) {
    let detail = `请求失败 (${response.status})`
    try { const body = await response.json() as { message?:string }; detail = body.message || detail } catch {}
    throw new Error(detail)
  }
  return response.status === 204 ? undefined as T : response.json() as Promise<T>
}

function upsertApproval(item:TerminalApproval) {
  const index = terminalApprovals.value.findIndex(existing => existing.id === item.id)
  if (index >= 0) terminalApprovals.value[index] = item
  else terminalApprovals.value = [item, ...terminalApprovals.value]
}

async function refreshApprovals() {
  try { terminalApprovals.value = await request<TerminalApproval[]>('/api/v1/discovery/remote-terminal-approvals') } catch {}
}

function startApprovalPolling() {
  if (approvalPoll) return
  approvalPoll = setInterval(() => void refreshApprovals(), 2000)
}

async function decideApproval(item:TerminalApproval, decision:'approve'|'reject') {
  if (approvalBusyId.value) return
  approvalBusyId.value = item.id
  try {
    const updated = await request<TerminalApproval>(`/api/v1/discovery/remote-terminal-approvals/${encodeURIComponent(item.id)}/decision`, {
      method:'POST',
      body:JSON.stringify({ decision, comment:approvalComment.value }),
    })
    upsertApproval(updated)
    approvalComment.value = ''
    message.value = decision === 'approve' ? '审批通过，命令已按一次性授权放行' : '审批已拒绝，命令未执行'
    if (item.sessionId === selectedSessionId.value && terminalInstance) {
      terminalInstance.writeln(`\r\n\x1b[${decision === 'approve' ? '32' : '31'}m[审批${decision === 'approve' ? '通过' : '拒绝'}] ${item.command}\x1b[0m\r\n`)
    }
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '审批处理失败'
    await refreshApprovals()
  } finally {
    approvalBusyId.value = ''
  }
}

function resetPolicyForm() {
  policyForm.value = defaultPolicyForm()
}

function editPolicy(item:SecurityPolicy) {
  policyForm.value = {
    id:item.id, name:item.name, subjectType:item.subjectType, subject:item.subject, enabled:item.enabled,
    maxSessionMinutes:item.maxSessionMinutes, maxConcurrentSessions:item.maxConcurrentSessions, recordingRetentionDays:item.recordingRetentionDays,
    fileTransferEnabled:item.fileTransferEnabled, uploadMaxMB:item.uploadMaxMB, downloadMaxMB:item.downloadMaxMB,
    allowedUploadPaths:item.allowedUploadPaths.join('\n'), allowedDownloadPaths:item.allowedDownloadPaths.join('\n'),
    allowControlCollaborators:item.allowControlCollaborators, approvalMode:item.approvalMode, approvalTtlMinutes:item.approvalTtlMinutes,
  }
}

function policySubjectLabel(item:SecurityPolicy) {
  if (item.subjectType === 'global') return '全局默认'
  return `${item.subjectType === 'user' ? '用户' : '角色'} · ${item.subject}`
}

async function savePolicy() {
  try {
    const form = policyForm.value
    await request<SecurityPolicy>('/api/v1/discovery/remote-security-policies', { method:'POST', body:JSON.stringify({
      id:form.id, name:form.name, subjectType:form.subjectType, subject:form.subject, enabled:form.enabled,
      maxSessionMinutes:Number(form.maxSessionMinutes), maxConcurrentSessions:Number(form.maxConcurrentSessions), recordingRetentionDays:Number(form.recordingRetentionDays),
      fileTransferEnabled:form.fileTransferEnabled, uploadMaxMb:Number(form.uploadMaxMB), downloadMaxMb:Number(form.downloadMaxMB),
      allowedUploadPaths:form.allowedUploadPaths.split(/[\n,]+/).map(value => value.trim()).filter(Boolean),
      allowedDownloadPaths:form.allowedDownloadPaths.split(/[\n,]+/).map(value => value.trim()).filter(Boolean),
      allowControlCollaborators:form.allowControlCollaborators, approvalMode:form.approvalMode, approvalTtlMinutes:Number(form.approvalTtlMinutes),
    }) })
    message.value = form.id ? '远程安全策略已更新' : '远程安全策略已创建'
    resetPolicyForm()
    await load()
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '远程安全策略保存失败' }
}

async function togglePolicy(item:SecurityPolicy) {
  try {
    await request('/api/v1/discovery/remote-security-policies', { method:'POST', body:JSON.stringify({
      id:item.id, name:item.name, subjectType:item.subjectType, subject:item.subject, enabled:!item.enabled,
      maxSessionMinutes:item.maxSessionMinutes, maxConcurrentSessions:item.maxConcurrentSessions, recordingRetentionDays:item.recordingRetentionDays,
      fileTransferEnabled:item.fileTransferEnabled, uploadMaxMb:item.uploadMaxMB, downloadMaxMb:item.downloadMaxMB,
      allowedUploadPaths:item.allowedUploadPaths, allowedDownloadPaths:item.allowedDownloadPaths,
      allowControlCollaborators:item.allowControlCollaborators, approvalMode:item.approvalMode, approvalTtlMinutes:item.approvalTtlMinutes,
    }) })
    message.value = item.enabled ? '策略已停用' : '策略已启用'
    await load()
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '远程安全策略状态更新失败' }
}

async function removePolicy(item:SecurityPolicy) {
  if (!confirm(`删除安全策略“${item.name}”？`)) return
  try {
    await request(`/api/v1/discovery/remote-security-policies/${encodeURIComponent(item.id)}`, { method:'DELETE' })
    if (policyForm.value.id === item.id) resetPolicyForm()
    message.value = '远程安全策略已删除'
    await load()
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '远程安全策略删除失败' }
}

function approvalStatusLabel(status:string) {
  return ({ pending:'待审批', approved:'已放行', rejected:'已拒绝', expired:'已超时', failed:'放行失败' } as Record<string,string>)[status] || status
}

async function load() {
  busy.value = true
  error.value = ''
  try {
    const [assetPage, gs, ss, cs, hks, hs, approvals, effective, policies] = await Promise.all([
      request<{data:Asset[]}>('/api/v1/cmdb/assets?page=1&pageSize=100&type=server'),
      request<Grant[]>('/api/v1/discovery/access-grants'),
      request<Session[]>('/api/v1/discovery/remote-sessions'),
      listCredentials(auth.token),
      request<HostKey[]>('/api/v1/discovery/remote-host-keys'),
      request<Session[]>('/api/v1/discovery/remote-sessions/history'),
      request<TerminalApproval[]>('/api/v1/discovery/remote-terminal-approvals'),
      request<SecurityPolicy>('/api/v1/discovery/remote-security-policies/effective'),
      isPlatformAdmin.value ? request<SecurityPolicy[]>('/api/v1/discovery/remote-security-policies') : Promise.resolve([] as SecurityPolicy[]),
    ])
    assets.value = assetPage.data
    grants.value = gs
    sessions.value = ss
    credentials.value = cs
    hostKeys.value = hks
    history.value = hs
    terminalApprovals.value = approvals
    effectivePolicy.value = effective
    securityPolicies.value = policies
    if (!selectedAssetId.value && assets.value.length) selectedAssetId.value = assets.value[0].id
    if (!selectedSessionId.value && sessions.value.length) selectedSessionId.value = sessions.value[0].id
    if (replaySession.value) replaySession.value = history.value.find(item => item.id === replaySession.value?.id) || replaySession.value
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '远程运维数据加载失败'
  } finally {
    busy.value = false
  }
}

async function createGrant() {
  try {
    const scope = grantForm.value.scopeType === 'project' ? { projectGroup:grantForm.value.projectGroup, assetId:'' } : { assetId:grantForm.value.assetId, projectGroup:'' }
    await request('/api/v1/discovery/access-grants', { method:'POST', body:JSON.stringify({ subjectType:grantForm.value.subjectType, subject:grantForm.value.subject, permissions:grantForm.value.permissions, ...scope }) })
    message.value = '授权已创建'
    await load()
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '授权创建失败' }
}

async function toggleGrant(item:Grant) {
  try { await request(`/api/v1/discovery/access-grants/${item.id}/toggle`, { method:'POST' }); await load() }
  catch (reason) { error.value = reason instanceof Error ? reason.message : '授权状态更新失败' }
}

async function removeGrant(item:Grant) {
  if (!confirm(`删除 ${item.subject} 的远程访问授权？`)) return
  try { await request(`/api/v1/discovery/access-grants/${item.id}`, { method:'DELETE' }); await load() }
  catch (reason) { error.value = reason instanceof Error ? reason.message : '授权删除失败' }
}

async function probeHostKey() {
  try {
    hostProbe.value = await request<HostKeyProbe>('/api/v1/discovery/remote-host-keys/probe', { method:'POST', body:JSON.stringify({ assetId:selectedAssetId.value, credentialId:credentialId.value }) })
    message.value = '主机指纹已探测，请确认后信任'
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '主机指纹探测失败' }
}

async function trustHostKey() {
  if (!hostProbe.value) return
  try {
    await request('/api/v1/discovery/remote-host-keys', { method:'POST', body:JSON.stringify(hostProbe.value) })
    hostProbe.value = undefined
    message.value = '主机指纹已信任'
    await load()
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '主机指纹信任失败' }
}

async function removeHostKey(item:HostKey) {
  if (!confirm(`删除 ${item.host} 的主机指纹信任？`)) return
  try { await request(`/api/v1/discovery/remote-host-keys/${item.id}`, { method:'DELETE' }); message.value = '主机指纹已删除'; await load() }
  catch (reason) { error.value = reason instanceof Error ? reason.message : '主机指纹删除失败' }
}

async function createSession() {
  try {
    const item = await request<Session>('/api/v1/discovery/remote-sessions', { method:'POST', body:JSON.stringify({ assetId:selectedAssetId.value, credentialId:credentialId.value }) })
    selectedSessionId.value = item.id
    terminalAccess.value = item.accessMode === 'readonly' ? 'readonly' : 'control'
    message.value = '远程会话已建立，操作记录会自动保存'
    await load()
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '会话建立失败' }
}

async function execute() {
  try { await request(`/api/v1/discovery/remote-sessions/${selectedSessionId.value}/command`, { method:'POST', body:JSON.stringify({ command:command.value }) }); await load() }
  catch (reason) { error.value = reason instanceof Error ? reason.message : '命令执行失败'; await load() }
}

async function upload() {
  if (!uploadFile.value) return
  const form = new FormData()
  form.append('file', uploadFile.value)
  form.append('path', uploadPath.value)
  try { await request(`/api/v1/discovery/remote-sessions/${selectedSessionId.value}/upload`, { method:'POST', body:form }); message.value = '文件上传完成'; await load() }
  catch (reason) { error.value = reason instanceof Error ? reason.message : '文件上传失败' }
}

async function download() {
  try {
    const response = await fetch(`/api/v1/discovery/remote-sessions/${selectedSessionId.value}/download?path=${encodeURIComponent(downloadPath.value)}`, { headers:{ Authorization:`Bearer ${auth.token}` } })
    if (!response.ok) throw new Error(`下载失败 (${response.status})`)
    const blob = await response.blob()
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = downloadPath.value.split('/').pop() || 'download.bin'
    link.click()
    URL.revokeObjectURL(url)
    message.value = '文件下载完成'
    await load()
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '文件下载失败' }
}

async function closeSession() {
  try { await request(`/api/v1/discovery/remote-sessions/${selectedSessionId.value}`, { method:'DELETE' }); message.value = '远程会话已关闭，录像已归档'; await load() }
  catch (reason) { error.value = reason instanceof Error ? reason.message : '关闭会话失败' }
}

async function addCollaborator() {
  if (!selectedSessionId.value || !collaboratorForm.value.username.trim()) return
  try {
    await request(`/api/v1/discovery/remote-sessions/${encodeURIComponent(selectedSessionId.value)}/collaborators`, {
      method:'POST',
      body:JSON.stringify(collaboratorForm.value),
    })
    collaboratorForm.value.username = ''
    message.value = '协作者已加入当前远程会话'
    await load()
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '添加协作者失败' }
}

async function removeCollaborator(item:Collaborator) {
  if (!selectedSessionId.value || !confirm(`移除协作者 ${item.username}？`)) return
  try {
    await request(`/api/v1/discovery/remote-sessions/${encodeURIComponent(selectedSessionId.value)}/collaborators/${encodeURIComponent(item.username)}`, { method:'DELETE' })
    message.value = `已移除协作者 ${item.username}`
    await load()
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '移除协作者失败' }
}

async function forceDisconnectSession() {
  if (!selectedSessionId.value || !confirm('确认强制断开该远程会话及全部协作者连接？')) return
  try {
    await request(`/api/v1/discovery/remote-sessions/${encodeURIComponent(selectedSessionId.value)}/force-disconnect`, { method:'POST' })
    closeInteractiveTerminal(false)
    message.value = '远程会话已由管理员强制断开'
    await load()
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '强制断开失败' }
}

async function downloadExport(kind:'terminal-recording'|'command-log') {
  if (!selectedSessionId.value) return
  try {
    const response = await fetch(`/api/v1/discovery/remote-sessions/${encodeURIComponent(selectedSessionId.value)}/${kind}/export`, { headers:{ Authorization:`Bearer ${auth.token}` } })
    if (!response.ok) throw new Error(`导出失败 (${response.status})`)
    const blob = await response.blob()
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `${selectedSession.value?.assetName || 'remote-session'}-${kind}.txt`
    link.click()
    URL.revokeObjectURL(url)
    message.value = kind === 'terminal-recording' ? '终端录像已导出' : '命令审计日志已导出'
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '审计记录导出失败' }
}

function decodeTerminalData(value:string) {
  const binary = atob(value)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index)
  return bytes
}

function createTerminal() {
  return new XTerm({
    cursorBlink: true,
    convertEol: false,
    fontFamily: '"JetBrains Mono", "Cascadia Mono", Consolas, monospace',
    fontSize: 12,
    scrollback: 5000,
    theme: { background:'#020b12', foreground:'#d8f3ff', cursor:'#67e8f9', selectionBackground:'#155e75' },
  })
}

async function ensureInteractiveTerminal() {
  await nextTick()
  if (!interactiveTerminalElement.value) return
  if (!terminalInstance) {
    terminalInstance = createTerminal()
    terminalFit = new FitAddon()
    terminalInstance.loadAddon(terminalFit)
    terminalInstance.open(interactiveTerminalElement.value)
    terminalInstance.onData(data => {
      if (terminalReadOnly.value) return
      if (terminalSocket?.readyState === WebSocket.OPEN) terminalSocket.send(JSON.stringify({ type:'input', data }))
    })
    terminalInstance.onResize(({ cols, rows }) => {
      if (terminalSocket?.readyState === WebSocket.OPEN) terminalSocket.send(JSON.stringify({ type:'resize', cols, rows }))
    })
  }
  terminalFit?.fit()
}

function resizeInteractiveTerminal() {
  nextTick(() => terminalFit?.fit())
}

function focusInteractiveTerminal() {
  terminalInstance?.focus()
}

function focusReplayTerminal() {
  replayTerminal?.focus()
}

async function openInteractiveTerminal() {
  if (!selectedSessionId.value || terminalConnecting.value) return
  terminalMode.value = true
  terminalError.value = ''
  terminalAccess.value = selectedSession.value?.accessMode === 'readonly' ? 'readonly' : 'control'
  await ensureInteractiveTerminal()
  if (terminalInstance) terminalInstance.options.disableStdin = terminalReadOnly.value
  closeInteractiveTerminal(false)
  terminalConnecting.value = true
  try {
    const ticket = await request<{ticket:string}>(`/api/v1/discovery/remote-sessions/${selectedSessionId.value}/terminal-ticket`, { method:'POST' })
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const query = new URLSearchParams({ ticket:ticket.ticket, cols:String(terminalInstance?.cols || 120), rows:String(terminalInstance?.rows || 32) })
    const socket = new WebSocket(`${protocol}//${location.host}/api/v1/discovery/remote-sessions/${selectedSessionId.value}/terminal?${query}`)
    terminalSocket = socket
    socket.onopen = () => {
      terminalConnecting.value = false
      terminalConnected.value = true
      if (!terminalReadOnly.value) terminalInstance?.focus()
      terminalInstance?.writeln(terminalReadOnly.value
        ? '\x1b[36m[CMDB] 已以只读协作者身份接入，仅同步观看远程终端输出。\x1b[0m'
        : '\x1b[32m[CMDB] 安全终端已连接，高风险命令将进入审批流程。\x1b[0m')
    }
    socket.onmessage = event => {
      let socketMessage: TerminalSocketMessage
      try { socketMessage = JSON.parse(String(event.data)) as TerminalSocketMessage } catch { terminalInstance?.write(String(event.data)); return }
      if (socketMessage.type === 'output' && socketMessage.data) terminalInstance?.write(decodeTerminalData(socketMessage.data))
      if (socketMessage.type === 'ready') {
        terminalAccess.value = socketMessage.access === 'readonly' ? 'readonly' : 'control'
        if (terminalInstance) terminalInstance.options.disableStdin = terminalReadOnly.value
      }
      if (socketMessage.type === 'blocked') terminalInstance?.writeln(`\r\n\x1b[31m[已阻止] ${socketMessage.message}\x1b[0m\r\n`)
      if (socketMessage.type === 'approval_required' && socketMessage.approval) {
        upsertApproval(socketMessage.approval)
        message.value = '高风险命令已暂停，等待平台管理员审批'
        terminalInstance?.writeln(`\r\n\x1b[33m[待审批] ${socketMessage.approval.reason}，命令尚未执行。\x1b[0m\r\n`)
      }
      if (socketMessage.type === 'error') { terminalError.value = socketMessage.message || '交互终端连接失败'; terminalInstance?.writeln(`\r\n\x1b[31m${terminalError.value}\x1b[0m`) }
      if (socketMessage.type === 'closed') { terminalConnected.value = false; terminalConnecting.value = false }
    }
    socket.onerror = () => { terminalError.value = '交互终端连接失败'; terminalConnected.value = false; terminalConnecting.value = false }
    socket.onclose = () => { terminalConnected.value = false; terminalConnecting.value = false }
  } catch (reason) {
    terminalConnecting.value = false
    terminalError.value = reason instanceof Error ? reason.message : '交互终端连接失败'
  }
}

function closeInteractiveTerminal(updateMessage = true) {
  if (terminalSocket?.readyState === WebSocket.OPEN) {
    terminalSocket.send(JSON.stringify({ type:'close' }))
    terminalSocket.close()
  }
  terminalSocket = undefined
  terminalConnected.value = false
  terminalConnecting.value = false
  if (updateMessage) terminalInstance?.writeln('\x1b[90m[CMDB] 交互终端已断开，远程会话仍保持。\x1b[0m')
}

function clearRecordingTimers() {
  recordingTimers.forEach(timer => clearTimeout(timer))
  recordingTimers = []
}

function resetRecordingView() {
  clearRecordingTimers()
  recordingEvents.value = []
  replayTerminal?.dispose()
  replayTerminal = undefined
  replayFit = undefined
}

async function ensureReplayTerminal() {
  await nextTick()
  if (!replayTerminalElement.value) return
  if (!replayTerminal) {
    replayTerminal = createTerminal()
    replayFit = new FitAddon()
    replayTerminal.loadAddon(replayFit)
    replayTerminal.open(replayTerminalElement.value)
  }
  replayFit?.fit()
}

async function loadTerminalRecording() {
  if (!replaySession.value || recordingLoading.value) return
  resetRecordingView()
  recordingLoading.value = true
  try {
    const recording = await request<TerminalRecording>(`/api/v1/discovery/remote-sessions/${encodeURIComponent(replaySession.value.id)}/terminal-recording`)
    recordingEvents.value = recording.events
    await ensureReplayTerminal()
    playTerminalRecording()
    if (recording.truncated) message.value = '终端录像较长，当前仅回放最近 20000 个事件'
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '终端录像加载失败'
  } finally {
    recordingLoading.value = false
  }
}

async function playTerminalRecording() {
  if (!recordingEvents.value.length) return
  clearRecordingTimers()
  await ensureReplayTerminal()
  replayTerminal?.reset()
  let delay = 0
  recordingEvents.value.forEach(event => {
    if (event.direction !== 'output') return
    delay += 80
    const chunk = decodeTerminalData(event.data)
    recordingTimers.push(setTimeout(() => replayTerminal?.write(chunk), Math.min(delay, 4000)))
  })
}

async function openReplay(id:string) {
  stopReplay()
  resetRecordingView()
  try {
    replaySession.value = await request<Session>(`/api/v1/discovery/remote-sessions/${encodeURIComponent(id)}/replay`)
    replayVisible.value = replaySession.value.logs.length
  } catch (reason) { error.value = reason instanceof Error ? reason.message : '会话回放加载失败' }
}

function startReplay() {
  if (!replaySession.value) return
  stopReplay()
  if (replayVisible.value >= replaySession.value.logs.length) replayVisible.value = 0
  replayPlaying.value = true
  replayTimer = setInterval(() => {
    const total = replaySession.value?.logs.length ?? 0
    if (replayVisible.value >= total) { stopReplay(); return }
    replayVisible.value += 1
  }, 700)
}

function stopReplay() {
  if (replayTimer) clearInterval(replayTimer)
  replayTimer = undefined
  replayPlaying.value = false
}

function resetReplay() {
  stopReplay()
  replayVisible.value = replaySession.value?.logs.length ?? 0
}

function statusLabel(status:string) {
  return ({ active:'进行中', closed:'已关闭', interrupted:'已中断' } as Record<string,string>)[status] || status
}

function kindLabel(kind?:string) {
  return ({ session:'会话', command:'命令', 'interactive-command':'交互命令', 'terminal-open':'终端连接', 'terminal-close':'终端断开', 'risk-blocked':'风险阻止', 'command-approved':'审批放行', 'command-rejected':'审批拒绝', 'file-upload':'上传', 'file-download':'下载' } as Record<string,string>)[kind || ''] || '事件'
}

function commandCount(item:Session) {
  return item.logs.filter(log => log.kind === 'command' || log.kind === 'interactive-command' || log.message.startsWith('$ ')).length
}

onMounted(async () => {
  await load()
  startApprovalPolling()
  watermarkTimer = setInterval(() => { watermarkTime.value = new Date().toLocaleString('zh-CN', { hour12:false }) }, 15000)
})
onBeforeUnmount(() => {
  if (approvalPoll) clearInterval(approvalPoll)
  if (watermarkTimer) clearInterval(watermarkTimer)
  stopReplay()
  closeInteractiveTerminal(false)
  clearRecordingTimers()
  terminalInstance?.dispose()
  replayTerminal?.dispose()
})
</script>

<template>
  <section class="remote-access">
    <header>
      <div><h2>远程终端与文件传输</h2><span>基于资产级授权，命令、文件操作和会话回放全程审计</span></div>
      <button @click="load"><RefreshCw :class="{spin:busy}"/></button>
    </header>
    <div v-if="error" class="access-error">{{error}}</div>
    <div class="access-grid">
      <section class="access-card">
        <h3><ShieldCheck/>账户授权</h3>
        <div class="access-form">
          <select v-model="grantForm.subjectType"><option value="user">用户</option><option value="role">角色</option></select>
          <input v-model="grantForm.subject" placeholder="用户或角色">
          <select v-model="grantForm.scopeType"><option value="project">项目组</option><option value="asset">指定资产</option></select>
          <select v-if="grantForm.scopeType==='project'" v-model="grantForm.projectGroup"><option value="">选择项目组</option><option v-for="group in projectGroups" :key="group">{{group}}</option></select>
          <select v-else v-model="grantForm.assetId"><option value="">选择资产</option><option v-for="asset in assets" :key="asset.id" :value="asset.id">{{asset.name}} · {{asset.ip}}</option></select>
          <label><input v-model="grantForm.permissions" type="checkbox" value="terminal">终端</label>
          <label><input v-model="grantForm.permissions" type="checkbox" value="file">文件</label>
          <button class="primary" @click="createGrant">新增授权</button>
        </div>
        <div class="grant-list">
          <article v-for="item in grants" :key="item.id" :class="{disabled:!item.enabled}">
            <KeyRound/><div><strong>{{item.subjectType}} · {{item.subject}}</strong><span>{{item.projectGroup||item.assetId}} · {{item.permissions.join(' / ')}}</span></div>
            <button @click="toggleGrant(item)">{{item.enabled?'停用':'启用'}}</button><button @click="removeGrant(item)">删除</button>
          </article>
        </div>
      </section>

      <section class="access-card">
        <h3><ShieldCheck/>主机指纹</h3>
        <div class="access-form">
          <button @click="probeHostKey">探测当前资产指纹</button>
          <div v-if="hostProbe" class="host-key-probe"><span>{{hostProbe.keyType}}</span><code>{{hostProbe.fingerprint}}</code><b v-if="hostProbe.changed">指纹已变更，原指纹：{{hostProbe.trustedFingerprint}}</b><button class="primary" @click="trustHostKey">{{hostProbe.trusted?'更新信任':'信任指纹'}}</button></div>
          <div class="grant-list"><article v-for="item in hostKeys" :key="item.id"><KeyRound/><div><strong>{{item.host}}:{{item.port}}</strong><span>{{item.fingerprint}}</span></div><button @click="removeHostKey(item)">删除</button></article></div>
        </div>
      </section>

      <section class="access-card">
        <h3><TerminalIcon/>远程会话</h3>
        <div class="access-form">
          <select v-model="selectedAssetId"><option v-for="asset in assets" :key="asset.id" :value="asset.id">{{asset.name}} · {{asset.ip}}</option></select>
          <select v-model="credentialId"><option value="">默认凭据</option><option v-for="credential in credentials.filter(item=>item.kind==='ssh')" :key="credential.id" :value="credential.id">{{credential.name}}</option></select>
          <button class="primary" :disabled="!selectedAssetId" @click="createSession">建立会话</button>
          <select v-model="selectedSessionId"><option value="">选择会话</option><option v-for="session in sessions" :key="session.id" :value="session.id">{{session.assetName}} · {{session.operator}} · {{session.status}}</option></select>
          <button :disabled="!selectedSessionId||!canManageSelectedSession" @click="closeSession"><X/>关闭</button>
          <button v-if="isPlatformAdmin" class="danger" :disabled="!selectedSessionId" @click="forceDisconnectSession"><Ban/>强制断开</button>
          <button class="primary" :disabled="!selectedSessionId||terminalConnecting" @click="openInteractiveTerminal"><TerminalIcon/>{{terminalConnected?'重新连接交互终端':'连接交互终端'}}</button>
          <button v-if="terminalConnected" @click="closeInteractiveTerminal()">断开交互终端</button>
        </div>
        <p class="policy-effective">当前账号生效策略：{{effectivePolicySummary}}</p>
        <div v-if="selectedSession" class="session-participants">
          <header>
            <div><strong>会话协作者</strong><span>{{selectedSession.accessMode==='readonly'?'当前身份：只读协作者':'当前身份：控制者'}} · {{selectedSession.activeConnections||0}} 人在线</span></div>
            <b :data-online="selectedSession.controllerOnline">{{selectedSession.controllerOnline?'控制端在线':'等待控制端'}}</b>
          </header>
          <div v-if="canManageSelectedSession" class="collaborator-form">
            <input v-model="collaboratorForm.username" placeholder="平台用户名" @keyup.enter="addCollaborator">
            <select v-model="collaboratorForm.access"><option value="readonly">只读观看</option><option value="control">允许控制</option></select>
            <button :disabled="!collaboratorForm.username.trim()" @click="addCollaborator">添加协作者</button>
          </div>
          <div class="collaborator-list">
            <article v-for="item in selectedSession.collaborators" :key="item.username">
              <KeyRound/><div><strong>{{item.username}}</strong><span>{{item.access==='readonly'?'只读观看':'允许控制'}} · 由 {{item.addedBy}} 添加</span></div>
              <button v-if="canManageSelectedSession" @click="removeCollaborator(item)">移除</button>
            </article>
            <div v-if="!selectedSession.collaborators.length" class="participant-empty">尚未添加协作者，可邀请其他平台用户实时观看或共同处理。</div>
          </div>
        </div>
        <div class="terminal-box">
          <div class="command-row"><input v-model="command" :disabled="terminalReadOnly" @keyup.enter="execute" placeholder="输入远程命令"><button :disabled="!selectedSessionId||terminalReadOnly" @click="execute"><Play/>执行</button></div>
          <pre v-if="selectedSession"><span v-for="log in selectedSession.logs" :key="log.time+log.message" :data-level="log.level"><em>{{log.time}}</em>{{log.message}}</span></pre>
          <div v-else class="empty-access">建立或选择会话后开始执行</div>
        </div>
      </section>

      <section class="access-card">
        <h3><FileUp/>文件传输</h3>
        <label>上传目标路径<input v-model="uploadPath"></label>
        <input type="file" @change="uploadFile=($event.target as HTMLInputElement).files?.[0]">
        <button :disabled="!selectedSessionId||!uploadFile||terminalReadOnly" @click="upload"><Upload/>上传</button>
        <label>下载远程路径<input v-model="downloadPath"></label>
        <button :disabled="!selectedSessionId||terminalReadOnly" @click="download">下载</button>
        <p>单文件限制：上传 50MB，下载 25MB。路径不允许包含 `..`。</p>
      </section>

      <section class="access-card live-terminal-card">
        <header class="live-terminal-head">
          <div><h3><TerminalIcon/>交互式 WebSSH</h3><span>PTY 实时终端，逐屏录像写入 PostgreSQL；高风险命令在执行前自动阻止</span></div>
          <div class="terminal-actions">
            <button :disabled="!selectedSessionId" @click="downloadExport('terminal-recording')">导出录像</button>
            <button :disabled="!selectedSessionId" @click="downloadExport('command-log')">导出命令日志</button>
            <b :data-state="terminalConnected?'online':terminalConnecting?'connecting':'offline'">{{terminalConnected?(terminalAccess==='readonly'?'只读在线':'控制在线'):terminalConnecting?'连接中':'未连接'}}</b>
            <button v-if="terminalConnected" @click="closeInteractiveTerminal()"><X/>断开</button>
            <button v-else class="primary" :disabled="!selectedSessionId||terminalConnecting" @click="openInteractiveTerminal"><Play/>连接</button>
          </div>
        </header>
        <div v-if="terminalError" class="access-error">{{terminalError}}</div>
        <div ref="interactiveTerminalElement" v-show="terminalMode" class="xterm-shell" @click="focusInteractiveTerminal"><span class="terminal-watermark">{{watermarkText}}</span></div>
        <div v-if="!terminalMode" class="terminal-empty"><TerminalIcon/><strong>选择或新建远程会话后连接</strong><span>支持交互输入、窗口缩放、逐屏录像与风险命令阻断</span></div>
      </section>

      <section v-if="isPlatformAdmin" class="access-card policy-card">
        <header class="policy-head"><div><h3><ShieldCheck/>终端安全策略中心</h3><span>用户策略优先于角色策略，角色策略优先于全局默认策略；修改操作全程审计</span></div><b>{{securityPolicies.length}} 条策略</b></header>
        <div class="policy-layout">
          <div class="policy-form">
            <input v-model="policyForm.name" placeholder="策略名称">
            <div class="policy-row"><select v-model="policyForm.subjectType" :disabled="policyForm.id==='rpolicy-global-default'"><option value="global">全局默认</option><option value="user">指定用户</option><option value="role">指定角色</option></select><input v-if="policyForm.subjectType!=='global'" v-model="policyForm.subject" placeholder="用户名或角色名" :disabled="policyForm.id==='rpolicy-global-default'"></div>
            <div class="policy-numbers"><label>会话时长（分钟）<input v-model.number="policyForm.maxSessionMinutes" type="number" min="5" max="1440"></label><label>最大并发会话<input v-model.number="policyForm.maxConcurrentSessions" type="number" min="1" max="100"></label><label>录像保留（天）<input v-model.number="policyForm.recordingRetentionDays" type="number" min="1" max="3650"></label><label>审批有效期（分钟）<input v-model.number="policyForm.approvalTtlMinutes" type="number" min="1" max="60"></label></div>
            <div class="policy-switches"><label><input v-model="policyForm.fileTransferEnabled" type="checkbox">允许文件传输</label><label><input v-model="policyForm.allowControlCollaborators" type="checkbox">允许可控协作者</label><label><input v-model="policyForm.enabled" type="checkbox">策略启用</label><select v-model="policyForm.approvalMode"><option value="risk">仅风险命令审批</option><option value="all">全部命令审批</option></select></div>
            <div class="policy-limits"><label>上传上限 MB<input v-model.number="policyForm.uploadMaxMB" type="number" min="1" max="50"></label><label>下载上限 MB<input v-model.number="policyForm.downloadMaxMB" type="number" min="1" max="25"></label></div>
            <label class="policy-path">上传路径白名单（逗号或换行分隔）<textarea v-model="policyForm.allowedUploadPaths" rows="2"></textarea></label>
            <label class="policy-path">下载路径白名单（逗号或换行分隔）<textarea v-model="policyForm.allowedDownloadPaths" rows="2"></textarea></label>
            <div class="policy-actions"><button class="primary" @click="savePolicy">{{policyForm.id?'保存修改':'新建策略'}}</button><button v-if="policyForm.id" @click="resetPolicyForm">取消编辑</button></div>
          </div>
          <div class="policy-list">
            <article v-for="item in securityPolicies" :key="item.id" :class="{disabled:!item.enabled}">
              <header><div><strong>{{item.name}}</strong><span>{{policySubjectLabel(item)}} · 优先级 {{item.priority}}</span></div><b>{{item.enabled?'已启用':'已停用'}}</b></header>
              <p>{{item.maxSessionMinutes}} 分钟 · {{item.maxConcurrentSessions}} 并发 · 录像 {{item.recordingRetentionDays}} 天 · {{item.approvalMode==='all'?'全部命令审批':'风险命令审批'}}</p>
              <p>文件传输 {{item.fileTransferEnabled?'开启':'关闭'}} · {{item.uploadMaxMB}}/{{item.downloadMaxMB}} MB · 可控协作者 {{item.allowControlCollaborators?'允许':'禁止'}}</p>
              <footer><button @click="editPolicy(item)">编辑</button><button @click="togglePolicy(item)">{{item.enabled?'停用':'启用'}}</button><button v-if="item.subjectType!=='global'" class="danger" @click="removePolicy(item)">删除</button></footer>
            </article>
          </div>
        </div>
      </section>

      <section class="access-card approval-card">
        <header class="approval-head">
          <div><h3><ShieldAlert/>高风险命令审批</h3><span>命令先暂停，平台管理员批准后按一次性授权放行；拒绝或超时均不会执行</span></div>
          <b :data-active="pendingApprovalCount > 0">{{pendingApprovalCount}} 条待处理</b>
        </header>
        <div class="approval-list">
          <article v-for="item in visibleApprovals" :key="item.id" :data-status="item.status">
            <div class="approval-main">
              <header><strong>{{item.assetName}} · {{item.ip}}</strong><b :data-status="item.status">{{approvalStatusLabel(item.status)}}</b></header>
              <code>{{item.command}}</code>
              <p>{{item.reason}}</p>
              <small>申请人 {{item.operator}} · {{item.requestedAt}} · 有效至 {{item.expiresAt}}</small>
              <small v-if="item.approver">审批人 {{item.approver}} · {{item.decidedAt}} · {{item.decisionComment || '无备注'}}</small>
            </div>
            <footer v-if="item.status === 'pending'">
              <input v-if="isPlatformAdmin" v-model="approvalComment" maxlength="500" placeholder="审批备注（可选）">
              <template v-if="isPlatformAdmin">
                <button :disabled="approvalBusyId===item.id" @click="decideApproval(item, 'reject')"><Ban/>拒绝</button>
                <button class="primary" :disabled="approvalBusyId===item.id" @click="decideApproval(item, 'approve')"><ShieldCheck/>通过并执行</button>
              </template>
              <span v-else>等待平台管理员审批，当前命令尚未执行</span>
            </footer>
          </article>
          <div v-if="!visibleApprovals.length" class="empty-access"><ShieldCheck/><strong>暂无高风险命令审批</strong><span>在交互式终端中触发风险规则后，请求会显示在这里</span></div>
        </div>
      </section>

      <section class="access-card history-card">
        <header class="history-head"><div><h3><History/>历史会话与命令回放</h3><span>会话日志持久化保存，重启后仍可审计和回放</span></div><b>{{filteredHistory.length}} 条</b></header>
        <div class="history-toolbar">
          <label class="search-box"><Search/><input v-model="historyQuery" placeholder="搜索资产、IP、操作人或会话 ID"></label>
          <select v-model="historyStatus"><option value="">全部状态</option><option value="active">进行中</option><option value="closed">已关闭</option><option value="interrupted">已中断</option></select>
        </div>
        <div class="history-layout">
          <div class="history-list">
            <button v-for="item in filteredHistory.slice(0,80)" :key="item.id" :class="{selected:replaySession?.id===item.id}" @click="openReplay(item.id)">
              <span class="history-main"><strong>{{item.assetName}}</strong><small>{{item.ip}} · {{item.operator}}</small></span>
              <span class="history-meta"><b :data-status="item.status">{{statusLabel(item.status)}}</b><small>{{item.createdAt}} · {{commandCount(item)}} 条命令</small></span>
            </button>
            <div v-if="!filteredHistory.length" class="empty-access">暂无匹配的历史会话</div>
          </div>
          <div v-if="replaySession" class="replay-panel">
            <header>
              <div><strong>{{replaySession.assetName}}</strong><span>{{replaySession.ip}} · {{replaySession.operator}} · {{replaySession.createdAt}}</span></div>
              <b :data-status="replaySession.status">{{statusLabel(replaySession.status)}}</b>
            </header>
            <div class="replay-actions">
              <button v-if="!replayPlaying" class="primary" @click="startReplay"><Play/>{{replayVisible ? '继续回放' : '开始回放'}}</button>
              <button v-else @click="stopReplay"><Pause/>暂停</button>
              <button @click="resetReplay"><RotateCcw/>显示全部</button>
              <button :disabled="recordingLoading" @click="loadTerminalRecording"><TerminalIcon/>{{recordingLoading?'加载中':'逐屏录像'}}</button>
              <span>{{replayVisible}} / {{replaySession.logs.length}} 个事件</span>
            </div>
            <div class="replay-timeline">
              <article v-for="(log,index) in visibleReplayLogs" :key="`${log.time}-${index}-${log.message}`" :data-level="log.level" :data-kind="log.kind||'event'">
                <div class="timeline-dot"><Clock3/></div>
                <div class="timeline-content"><header><b>{{kindLabel(log.kind)}}</b><time>{{log.time}}</time><span v-if="log.durationMs">{{log.durationMs}} ms</span></header><pre>{{log.message}}</pre></div>
              </article>
              <div v-if="!replayVisible" class="empty-access">点击开始回放，按时间顺序还原会话操作</div>
            </div>
            <div v-if="recordingEvents.length" class="terminal-recording">
              <header><div><strong>PTY 逐屏录像</strong><span>{{recordingEvents.length}} 个原始终端事件</span></div><button @click="playTerminalRecording"><Play/>重新播放</button></header>
              <div ref="replayTerminalElement" class="xterm-shell replay-xterm" @click="focusReplayTerminal"></div>
            </div>
          </div>
          <div v-else class="replay-placeholder"><History/><strong>选择一个历史会话</strong><span>查看命令时间线、输出结果、文件操作和关闭记录</span></div>
        </div>
      </section>
    </div>
    <div v-if="message" class="access-message">{{message}}</div>
  </section>
</template>

<style scoped>
.remote-access{margin:18px 20px;padding:18px;border:1px solid #18384a;border-radius:12px;background:#071925}.remote-access>header{display:flex;justify-content:space-between;align-items:center}.remote-access h2{margin:4px 0}.remote-access header span{color:#6f91a5;font-size:11px}.access-grid{display:grid;grid-template-columns:1fr 1.3fr 1fr;gap:12px;margin-top:14px}.access-card{padding:13px;border:1px solid #173a52;background:#081f31}.access-card h3{display:flex;align-items:center;gap:7px;font-size:13px}.access-form{display:grid;gap:7px}.access-form input,.access-form select{border:1px solid #22465f;background:#061725;color:#dcecff;padding:8px}.access-form label{font-size:11px;color:#7894a8}.grant-list article{display:grid;grid-template-columns:20px 1fr 50px 50px;gap:6px;align-items:center;padding:8px 0;border-top:1px solid #123043}.grant-list article.disabled{opacity:.5}.grant-list span{display:block;color:#6f91a5;font-size:10px}.grant-list button{padding:4px 5px;font-size:10px}.terminal-box{margin-top:10px}.command-row{display:flex;gap:6px}.command-row input{flex:1}.terminal-box pre{min-height:240px;max-height:360px;overflow:auto;white-space:pre-wrap;background:#020b12;color:#bae6fd;padding:10px;font:11px monospace}.terminal-box pre span{display:block;margin-bottom:8px}.terminal-box em{color:#4f7188;font-style:normal;margin-right:8px}.terminal-box span[data-level=error]{color:#fb7185}.empty-access{padding:30px;text-align:center;color:#64869a}.access-card p{color:#6f91a5;font-size:10px}.access-error,.access-message{margin-top:10px;padding:9px;border:1px solid #7b3140;background:#351723;color:#ff9cad}.access-message{border-color:#155e75;background:#082b38;color:#a5f3fc}.history-card{grid-column:1/-1}.history-head{display:flex;justify-content:space-between;align-items:center}.history-head h3{margin:0}.history-head>div{display:grid;gap:3px}.history-head>b{padding:4px 9px;border-radius:999px;background:#0c2b3d;color:#67e8f9;font-size:11px}.history-toolbar{display:grid;grid-template-columns:1fr 180px;gap:10px;margin:13px 0}.search-box{display:flex;align-items:center;gap:8px;padding:0 10px;border:1px solid #22465f;background:#061725}.search-box svg{width:15px;color:#64869a}.search-box input{width:100%;border:0;background:transparent;color:#dcecff;padding:9px 0;outline:0}.history-toolbar select{border:1px solid #22465f;background:#061725;color:#dcecff;padding:8px}.history-layout{display:grid;grid-template-columns:minmax(280px,.8fr) minmax(440px,1.6fr);gap:12px;min-height:330px}.history-list{max-height:460px;overflow:auto;border:1px solid #123043;background:#061620}.history-list>button{display:flex;justify-content:space-between;gap:12px;width:100%;padding:11px 12px;border:0;border-bottom:1px solid #123043;background:transparent;color:#dcecff;text-align:left;cursor:pointer}.history-list>button:hover,.history-list>button.selected{background:#0b2a3b}.history-main,.history-meta{display:grid;gap:3px}.history-main small,.history-meta small{color:#6f91a5;font-size:10px}.history-meta{text-align:right}.history-meta b{font-size:11px}.history-meta b[data-status=active],.replay-panel header>b[data-status=active]{color:#38bdf8}.history-meta b[data-status=closed],.replay-panel header>b[data-status=closed]{color:#2dd4bf}.history-meta b[data-status=interrupted],.replay-panel header>b[data-status=interrupted]{color:#f59e0b}.replay-panel{display:flex;min-width:0;flex-direction:column;border:1px solid #123043;background:#040f17}.replay-panel>header{display:flex;justify-content:space-between;gap:12px;padding:12px;border-bottom:1px solid #123043}.replay-panel>header>div{display:grid;gap:3px}.replay-panel>header span{color:#6f91a5}.replay-actions{display:flex;align-items:center;gap:8px;padding:10px 12px;border-bottom:1px solid #123043}.replay-actions span{margin-left:auto;color:#6f91a5;font-size:11px}.replay-actions button{display:flex;align-items:center;gap:5px}.replay-timeline{flex:1;max-height:390px;overflow:auto;padding:12px}.replay-timeline>article{display:grid;grid-template-columns:28px 1fr;gap:8px}.timeline-dot{display:flex;justify-content:center;color:#22d3ee}.timeline-dot::after{content:'';width:1px;flex:1;margin-top:4px;background:#17415a}.timeline-content{min-width:0;padding-bottom:12px}.timeline-content>header{display:flex;align-items:center;gap:9px;margin-bottom:5px}.timeline-content>header b{color:#67e8f9;font-size:11px}.timeline-content time,.timeline-content span{color:#64869a;font-size:10px}.timeline-content pre{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;padding:9px;border-radius:7px;background:#020b12;color:#bae6fd;font:11px/1.55 monospace}.replay-timeline article[data-level=error] .timeline-dot{color:#fb7185}.replay-timeline article[data-level=error] pre{color:#fda4af}.replay-placeholder{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:7px;border:1px dashed #1b4258;color:#64869a}.replay-placeholder svg{width:30px;height:30px;color:#22d3ee}.replay-placeholder strong{color:#c9e6f5}.host-key-probe{display:grid;gap:6px;padding:8px;border:1px solid #155e75;background:#082b38}.host-key-probe code{overflow-wrap:anywhere;color:#a5f3fc;font-size:10px}.host-key-probe b{color:#f59e0b;font-size:10px}.live-terminal-card{grid-column:1/-1}.live-terminal-head{display:flex;justify-content:space-between;align-items:center;gap:16px}.live-terminal-head>div:first-child{display:grid;gap:4px}.live-terminal-head h3{margin:0}.terminal-actions{display:flex;align-items:center;gap:8px}.terminal-actions b{padding:4px 9px;border-radius:999px;font-size:11px}.terminal-actions b[data-state=offline]{background:#2a1721;color:#fda4af}.terminal-actions b[data-state=connecting]{background:#322712;color:#fcd34d}.terminal-actions b[data-state=online]{background:#0b2f2b;color:#5eead4}.terminal-actions button{display:flex;align-items:center;gap:5px}.xterm-shell{height:430px;margin-top:12px;padding:8px;border:1px solid #16445c;border-radius:8px;background:#020b12;overflow:hidden}.xterm-shell .xterm{height:100%}.terminal-empty{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:7px;height:180px;margin-top:12px;border:1px dashed #1b4258;color:#64869a}.terminal-empty svg{width:30px;height:30px;color:#22d3ee}.terminal-empty strong{color:#c9e6f5}.terminal-recording{margin-top:12px;border-top:1px solid #123043}.terminal-recording>header{display:flex;justify-content:space-between;align-items:center;padding:10px 0}.terminal-recording>header>div{display:grid;gap:3px}.terminal-recording>header span{color:#6f91a5;font-size:10px}.terminal-recording>header button{display:flex;align-items:center;gap:5px}.replay-xterm{height:300px;margin-top:0}.terminal-recording+.replay-timeline{max-height:220px}.approval-card{grid-column:1/-1}.approval-head{display:flex;align-items:center;justify-content:space-between;gap:16px}.approval-head>div{display:grid;gap:4px}.approval-head h3{margin:0}.approval-head>div>span{color:#6f91a5}.approval-head>b{padding:5px 10px;border-radius:999px;background:#162c38;color:#8fb2c4;font-size:11px}.approval-head>b[data-active=true]{background:#3a2610;color:#fbbf24}.approval-list{display:grid;grid-template-columns:repeat(auto-fit,minmax(360px,1fr));gap:10px;margin-top:12px}.approval-list article{display:flex;flex-direction:column;gap:10px;padding:12px;border:1px solid #443414;background:#171307}.approval-list article[data-status=approved]{border-color:#14564c;background:#071b19}.approval-list article[data-status=rejected],.approval-list article[data-status=failed]{border-color:#692c3a;background:#1d0b11}.approval-list article[data-status=expired]{border-color:#40515d;background:#101820}.approval-main{display:grid;gap:6px;min-width:0}.approval-main header{display:flex;align-items:center;justify-content:space-between;gap:12px}.approval-main header b{font-size:10px;color:#fbbf24}.approval-main header b[data-status=approved]{color:#5eead4}.approval-main header b[data-status=rejected],.approval-main header b[data-status=failed]{color:#fb7185}.approval-main code{overflow-wrap:anywhere;padding:8px;border-radius:6px;background:#020b12;color:#fde68a;font:11px/1.5 monospace}.approval-main p{margin:0;color:#fca5a5;font-size:12px}.approval-main small{color:#7895a6;font-size:10px}.approval-list footer{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.approval-list footer input{flex:1;min-width:180px;padding:8px 9px;border:1px solid #4a3a1a;border-radius:6px;background:#080f14;color:#e5eef5}.approval-list footer button{display:flex;align-items:center;gap:5px}.approval-list footer span{color:#fbbf24;font-size:11px}@media(max-width:1200px){.access-grid{grid-template-columns:1fr}.history-layout{grid-template-columns:1fr}.history-list{max-height:260px}}
.session-participants{margin-top:12px;padding-top:12px;border-top:1px solid #123043}.session-participants>header{display:flex;align-items:center;justify-content:space-between;gap:12px}.session-participants>header>div{display:grid;gap:3px}.session-participants>header span{color:#6f91a5;font-size:10px}.session-participants>header>b{padding:4px 8px;border-radius:999px;background:#321a22;color:#fda4af;font-size:10px}.session-participants>header>b[data-online=true]{background:#0b2f2b;color:#5eead4}.collaborator-form{display:grid;grid-template-columns:1fr 130px auto;gap:7px;margin-top:10px}.collaborator-form input,.collaborator-form select{min-width:0;padding:8px;border:1px solid #22465f;background:#061725;color:#dcecff}.collaborator-list{display:grid;gap:6px;margin-top:9px}.collaborator-list article{display:grid;grid-template-columns:18px 1fr auto;align-items:center;gap:8px;padding:8px;border:1px solid #123043;background:#061a29}.collaborator-list article>div{display:grid;gap:2px}.collaborator-list article strong{font-size:12px}.collaborator-list article span{color:#6f91a5;font-size:10px}.participant-empty{padding:10px;border:1px dashed #1b4258;color:#64869a;font-size:11px;text-align:center}.xterm-shell{position:relative}.terminal-watermark{position:absolute;inset:0;z-index:5;display:flex;align-items:center;justify-content:center;transform:rotate(-18deg);pointer-events:none;color:rgba(103,232,249,.09);font:700 24px/1.6 monospace;letter-spacing:3px;text-align:center;white-space:pre-wrap;overflow:hidden}.danger{border-color:#7f1d1d!important;background:#331116!important;color:#fecaca!important}@media(max-width:1200px){.collaborator-form{grid-template-columns:1fr}}</style>
<style scoped>
.policy-card{grid-column:1/-1}.policy-head{display:flex;justify-content:space-between;align-items:center;gap:16px}.policy-head>div{display:grid;gap:4px}.policy-head h3{margin:0}.policy-head>div>span{color:#6f91a5;font-size:10px}.policy-head>b{padding:4px 9px;border-radius:999px;background:#0c2b3d;color:#67e8f9;font-size:10px}.policy-layout{display:grid;grid-template-columns:minmax(420px,1fr) minmax(420px,1.15fr);gap:14px;margin-top:12px}.policy-form{display:grid;gap:8px;padding:12px;border:1px solid #123043;background:#061620}.policy-form input,.policy-form select,.policy-form textarea{padding:8px;border:1px solid #22465f;background:#061725;color:#dcecff;font:inherit}.policy-form textarea{resize:vertical}.policy-row,.policy-limits{display:grid;grid-template-columns:1fr 1fr;gap:8px}.policy-numbers{display:grid;grid-template-columns:1fr 1fr;gap:8px}.policy-numbers label,.policy-limits label,.policy-path{display:grid;gap:4px;color:#7894a8;font-size:10px}.policy-switches{display:flex;align-items:center;gap:12px;flex-wrap:wrap;color:#8fb2c4;font-size:11px}.policy-switches label{display:flex;align-items:center;gap:4px}.policy-switches select{margin-left:auto}.policy-actions{display:flex;gap:8px}.policy-list{display:grid;gap:8px;max-height:520px;overflow:auto}.policy-list article{display:grid;gap:7px;padding:11px;border:1px solid #16445c;background:#061a29}.policy-list article.disabled{opacity:.55}.policy-list article>header{display:flex;justify-content:space-between;gap:12px}.policy-list article>header>div{display:grid;gap:2px}.policy-list article>header span,.policy-list article p{color:#6f91a5;font-size:10px;margin:0}.policy-list article>header>b{color:#5eead4;font-size:10px}.policy-list article.disabled>header>b{color:#94a3b8}.policy-list footer{display:flex;gap:7px}.policy-effective{margin:9px 0 0;padding:7px 9px;border:1px solid #155e75;background:#082b38;color:#a5f3fc!important}.policy-list article>p:first-of-type{color:#8fb2c4}.policy-list article>p:last-of-type{color:#64748b}@media(max-width:1200px){.policy-layout{grid-template-columns:1fr}.policy-numbers{grid-template-columns:1fr 1fr}}
</style>
