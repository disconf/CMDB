<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Clock3, FileUp, History, KeyRound, Pause, Play, RefreshCw, RotateCcw, Search, ShieldCheck, Terminal, Upload, X } from 'lucide-vue-next'
import { listCredentials, type Credential } from '@/api/cmdb'
import { useAuthStore } from '@/stores/useAuthStore'

type Asset = { id:string; name:string; ip:string; projectGroup:string; status:string }
type HostKey = { id:string; assetId:string; host:string; port:number; keyType:string; fingerprint:string; addedBy:string; createdAt:string }
type HostKeyProbe = { assetId:string; host:string; port:number; keyType:string; fingerprint:string; publicKey:string; trusted:boolean; changed:boolean; trustedFingerprint:string }
type Grant = { id:string; subjectType:string; subject:string; assetId:string; projectGroup:string; permissions:string[]; enabled:boolean; createdBy:string }
type SessionLog = { time:string; level:string; kind?:string; message:string; durationMs?:number }
type Session = { id:string; assetId:string; assetName:string; ip:string; credentialId:string; operator:string; status:string; createdAt:string; expiresAt:string; closedAt?:string; logs:SessionLog[] }

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
let replayTimer: ReturnType<typeof setInterval> | undefined
const grantForm = ref({ subjectType:'user', subject:'admin', scopeType:'project', assetId:'', projectGroup:'', permissions:['terminal', 'file'] })

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

async function request<T>(url:string, init?:RequestInit) {
  const response = await fetch(url, { ...init, headers: init?.body instanceof FormData ? { Authorization:`Bearer ${auth.token}` } : headers.value })
  if (!response.ok) {
    let detail = `请求失败 (${response.status})`
    try { const body = await response.json() as { message?:string }; detail = body.message || detail } catch {}
    throw new Error(detail)
  }
  return response.status === 204 ? undefined as T : response.json() as Promise<T>
}

async function load() {
  busy.value = true
  error.value = ''
  try {
    const [assetPage, gs, ss, cs, hks, hs] = await Promise.all([
      request<{data:Asset[]}>('/api/v1/cmdb/assets?page=1&pageSize=100&type=server'),
      request<Grant[]>('/api/v1/discovery/access-grants'),
      request<Session[]>('/api/v1/discovery/remote-sessions'),
      listCredentials(auth.token),
      request<HostKey[]>('/api/v1/discovery/remote-host-keys'),
      request<Session[]>('/api/v1/discovery/remote-sessions/history'),
    ])
    assets.value = assetPage.data
    grants.value = gs
    sessions.value = ss
    credentials.value = cs
    hostKeys.value = hks
    history.value = hs
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

async function openReplay(id:string) {
  stopReplay()
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
  return ({ session:'会话', command:'命令', 'file-upload':'上传', 'file-download':'下载' } as Record<string,string>)[kind || ''] || '事件'
}

function commandCount(item:Session) {
  return item.logs.filter(log => log.kind === 'command' || log.message.startsWith('$ ')).length
}

onMounted(load)
onBeforeUnmount(stopReplay)
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
        <h3><Terminal/>远程会话</h3>
        <div class="access-form">
          <select v-model="selectedAssetId"><option v-for="asset in assets" :key="asset.id" :value="asset.id">{{asset.name}} · {{asset.ip}}</option></select>
          <select v-model="credentialId"><option value="">默认凭据</option><option v-for="credential in credentials.filter(item=>item.kind==='ssh')" :key="credential.id" :value="credential.id">{{credential.name}}</option></select>
          <button class="primary" :disabled="!selectedAssetId" @click="createSession">建立会话</button>
          <select v-model="selectedSessionId"><option value="">选择会话</option><option v-for="session in sessions" :key="session.id" :value="session.id">{{session.assetName}} · {{session.operator}} · {{session.status}}</option></select>
          <button :disabled="!selectedSessionId" @click="closeSession"><X/>关闭</button>
        </div>
        <div class="terminal-box">
          <div class="command-row"><input v-model="command" @keyup.enter="execute" placeholder="输入远程命令"><button :disabled="!selectedSessionId" @click="execute"><Play/>执行</button></div>
          <pre v-if="selectedSession"><span v-for="log in selectedSession.logs" :key="log.time+log.message" :data-level="log.level"><em>{{log.time}}</em>{{log.message}}</span></pre>
          <div v-else class="empty-access">建立或选择会话后开始执行</div>
        </div>
      </section>

      <section class="access-card">
        <h3><FileUp/>文件传输</h3>
        <label>上传目标路径<input v-model="uploadPath"></label>
        <input type="file" @change="uploadFile=($event.target as HTMLInputElement).files?.[0]">
        <button :disabled="!selectedSessionId||!uploadFile" @click="upload"><Upload/>上传</button>
        <label>下载远程路径<input v-model="downloadPath"></label>
        <button :disabled="!selectedSessionId" @click="download">下载</button>
        <p>单文件限制：上传 50MB，下载 25MB。路径不允许包含 `..`。</p>
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
              <span>{{replayVisible}} / {{replaySession.logs.length}} 个事件</span>
            </div>
            <div class="replay-timeline">
              <article v-for="(log,index) in visibleReplayLogs" :key="`${log.time}-${index}-${log.message}`" :data-level="log.level" :data-kind="log.kind||'event'">
                <div class="timeline-dot"><Clock3/></div>
                <div class="timeline-content"><header><b>{{kindLabel(log.kind)}}</b><time>{{log.time}}</time><span v-if="log.durationMs">{{log.durationMs}} ms</span></header><pre>{{log.message}}</pre></div>
              </article>
              <div v-if="!replayVisible" class="empty-access">点击开始回放，按时间顺序还原会话操作</div>
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
.remote-access{margin:18px 20px;padding:18px;border:1px solid #18384a;border-radius:12px;background:#071925}.remote-access>header{display:flex;justify-content:space-between;align-items:center}.remote-access h2{margin:4px 0}.remote-access header span{color:#6f91a5;font-size:11px}.access-grid{display:grid;grid-template-columns:1fr 1.3fr 1fr;gap:12px;margin-top:14px}.access-card{padding:13px;border:1px solid #173a52;background:#081f31}.access-card h3{display:flex;align-items:center;gap:7px;font-size:13px}.access-form{display:grid;gap:7px}.access-form input,.access-form select{border:1px solid #22465f;background:#061725;color:#dcecff;padding:8px}.access-form label{font-size:11px;color:#7894a8}.grant-list article{display:grid;grid-template-columns:20px 1fr 50px 50px;gap:6px;align-items:center;padding:8px 0;border-top:1px solid #123043}.grant-list article.disabled{opacity:.5}.grant-list span{display:block;color:#6f91a5;font-size:10px}.grant-list button{padding:4px 5px;font-size:10px}.terminal-box{margin-top:10px}.command-row{display:flex;gap:6px}.command-row input{flex:1}.terminal-box pre{min-height:240px;max-height:360px;overflow:auto;white-space:pre-wrap;background:#020b12;color:#bae6fd;padding:10px;font:11px monospace}.terminal-box pre span{display:block;margin-bottom:8px}.terminal-box em{color:#4f7188;font-style:normal;margin-right:8px}.terminal-box span[data-level=error]{color:#fb7185}.empty-access{padding:30px;text-align:center;color:#64869a}.access-card p{color:#6f91a5;font-size:10px}.access-error,.access-message{margin-top:10px;padding:9px;border:1px solid #7b3140;background:#351723;color:#ff9cad}.access-message{border-color:#155e75;background:#082b38;color:#a5f3fc}.history-card{grid-column:1/-1}.history-head{display:flex;justify-content:space-between;align-items:center}.history-head h3{margin:0}.history-head>div{display:grid;gap:3px}.history-head>b{padding:4px 9px;border-radius:999px;background:#0c2b3d;color:#67e8f9;font-size:11px}.history-toolbar{display:grid;grid-template-columns:1fr 180px;gap:10px;margin:13px 0}.search-box{display:flex;align-items:center;gap:8px;padding:0 10px;border:1px solid #22465f;background:#061725}.search-box svg{width:15px;color:#64869a}.search-box input{width:100%;border:0;background:transparent;color:#dcecff;padding:9px 0;outline:0}.history-toolbar select{border:1px solid #22465f;background:#061725;color:#dcecff;padding:8px}.history-layout{display:grid;grid-template-columns:minmax(280px,.8fr) minmax(440px,1.6fr);gap:12px;min-height:330px}.history-list{max-height:460px;overflow:auto;border:1px solid #123043;background:#061620}.history-list>button{display:flex;justify-content:space-between;gap:12px;width:100%;padding:11px 12px;border:0;border-bottom:1px solid #123043;background:transparent;color:#dcecff;text-align:left;cursor:pointer}.history-list>button:hover,.history-list>button.selected{background:#0b2a3b}.history-main,.history-meta{display:grid;gap:3px}.history-main small,.history-meta small{color:#6f91a5;font-size:10px}.history-meta{text-align:right}.history-meta b{font-size:11px}.history-meta b[data-status=active],.replay-panel header>b[data-status=active]{color:#38bdf8}.history-meta b[data-status=closed],.replay-panel header>b[data-status=closed]{color:#2dd4bf}.history-meta b[data-status=interrupted],.replay-panel header>b[data-status=interrupted]{color:#f59e0b}.replay-panel{display:flex;min-width:0;flex-direction:column;border:1px solid #123043;background:#040f17}.replay-panel>header{display:flex;justify-content:space-between;gap:12px;padding:12px;border-bottom:1px solid #123043}.replay-panel>header>div{display:grid;gap:3px}.replay-panel>header span{color:#6f91a5}.replay-actions{display:flex;align-items:center;gap:8px;padding:10px 12px;border-bottom:1px solid #123043}.replay-actions span{margin-left:auto;color:#6f91a5;font-size:11px}.replay-actions button{display:flex;align-items:center;gap:5px}.replay-timeline{flex:1;max-height:390px;overflow:auto;padding:12px}.replay-timeline>article{display:grid;grid-template-columns:28px 1fr;gap:8px}.timeline-dot{display:flex;justify-content:center;color:#22d3ee}.timeline-dot::after{content:'';width:1px;flex:1;margin-top:4px;background:#17415a}.timeline-content{min-width:0;padding-bottom:12px}.timeline-content>header{display:flex;align-items:center;gap:9px;margin-bottom:5px}.timeline-content>header b{color:#67e8f9;font-size:11px}.timeline-content time,.timeline-content span{color:#64869a;font-size:10px}.timeline-content pre{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;padding:9px;border-radius:7px;background:#020b12;color:#bae6fd;font:11px/1.55 monospace}.replay-timeline article[data-level=error] .timeline-dot{color:#fb7185}.replay-timeline article[data-level=error] pre{color:#fda4af}.replay-placeholder{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:7px;border:1px dashed #1b4258;color:#64869a}.replay-placeholder svg{width:30px;height:30px;color:#22d3ee}.replay-placeholder strong{color:#c9e6f5}.host-key-probe{display:grid;gap:6px;padding:8px;border:1px solid #155e75;background:#082b38}.host-key-probe code{overflow-wrap:anywhere;color:#a5f3fc;font-size:10px}.host-key-probe b{color:#f59e0b;font-size:10px}@media(max-width:1200px){.access-grid{grid-template-columns:1fr}.history-layout{grid-template-columns:1fr}.history-list{max-height:260px}}
</style>
