<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Cable, Camera, Clock3, GitBranch, Maximize2, Network, RefreshCw, RotateCcw, Search, Server, Waypoints, X } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'
import { captureTopologySnapshot, fetchPhysicalTopology, fetchTopologySnapshot, fetchTopologySnapshots } from '@/api/cmdb'
import { healthLabel, impactCount } from '@/features/topology/topologyModel'

type Node = { id:string; name:string; kind:string; layer?:number; status:string; owner?:string; ip?:string; assetType?:string; location?:string; portCount?:number; upPortCount?:number }
type Edge = { id:string; source:string; target:string; relation:string; status?:string; sourceInterface?:string; targetPort?:string; sourceType?:string; verification?:string; confidence?:number; evidence?:string[]; lastSeenAt?:string }
type Graph = { nodes:Node[]; edges:Edge[]; updatedAt:string }
type ImpactNode = Node & { distance:number; direction:'upstream'|'downstream'|string; path:string[] }
type Impact = { root:Node; affected:ImpactNode[]; paths:string[][]; summary:{ total:number; healthy:number; warning:number; critical:number } }
type Mode = 'business'|'physical'
type Snapshot = { id:string;kind:string;environment:string;nodeCount:number;edgeCount:number;operator:string;createdAt:string;payload?:unknown }

const auth = useAuthStore()
const graph = ref<Graph>({ nodes: [], edges: [], updatedAt: '' })
const selected = ref<Node>()
const impact = ref<Impact>()
const loading = ref(false)
const query = ref('')
const mode = ref<Mode>('business')
const snapshots = ref<Snapshot[]>([])
const snapshotView = ref(false)
const headers = computed(() => ({ Authorization: `Bearer ${auth.token}` }))

const positions = computed(() => {
  const out: Record<string, { x:number; y:number }> = {}
  if (mode.value === 'physical') {
    const groups = [graph.value.nodes.filter(node => node.assetType !== 'network-device' && node.assetType !== 'load-balancer'), graph.value.nodes.filter(node => node.assetType === 'network-device' || node.assetType === 'load-balancer')]
    groups.forEach((nodes, layer) => nodes.forEach((node, index) => { out[node.id] = { x: 110 + layer * 600, y: 85 + index * 118 } }))
    return out
  }
  const grouped = new Map<number, Node[]>()
  graph.value.nodes.forEach(node => grouped.set(node.layer || 0, [...(grouped.get(node.layer || 0) || []), node]))
  grouped.forEach((nodes, layer) => nodes.forEach((node, index) => { out[node.id] = { x: 100 + layer * 220, y: 100 + index * 150 + (nodes.length === 1 ? 75 : 0) } }))
  return out
})
const visibleNodes = computed(() => graph.value.nodes.filter(node => !query.value || node.name.toLowerCase().includes(query.value.toLowerCase())))
const nodeNameByID = computed(() => Object.fromEntries(graph.value.nodes.map(node => [node.id, node.name])))
const selectedEdges = computed(() => selected.value ? graph.value.edges.filter(edge => edge.source === selected.value?.id || edge.target === selected.value?.id) : [])
const physicalNodeCount = computed(() => graph.value.nodes.filter(node => ['physical-server','virtual-machine','k8s-node'].includes(node.assetType || '')).length)
const physicalSwitchCount = computed(() => graph.value.nodes.filter(node => ['network-device','load-balancer'].includes(node.assetType || '')).length)

function verificationLabel(value?:string) {
  return ({ confirmed:'已确认', conflict:'冲突待复核', 'lldp-confirmed':'LLDP 已确认', candidate:'LLDP 候选', unverified:'待验证' } as Record<string,string>)[value || ''] || '待验证'
}
function verificationTone(value?:string) {
  if (value === 'confirmed' || value === 'lldp-confirmed') return 'healthy'
  if (value === 'conflict') return 'critical'
  if (value === 'candidate') return 'warning'
  return 'unknown'
}
async function captureSnapshot() {
  const snapshot = await captureTopologySnapshot(auth.token, 'physical', '')
  snapshots.value = [snapshot, ...snapshots.value]
}
async function viewSnapshot(id:string) {
  const snapshot = await fetchTopologySnapshot(auth.token, id)
  if (snapshot.payload) {
    graph.value = snapshot.payload as Graph
    snapshotView.value = true
    selected.value = graph.value.nodes[0]
  }
}
async function returnLive() { snapshotView.value = false; await load() }
function directionLabel(direction:string) { return direction === 'upstream' ? '向上游影响' : direction === 'downstream' ? '向下游影响' : '关联影响' }
function pathLabel(node:ImpactNode) { return (node.path || []).map(id => nodeNameByID.value[id] || id).join(' → ') }
function endpointLabel(edge:Edge, side:'source'|'target') { const id = side === 'source' ? edge.source : edge.target; const port = side === 'source' ? edge.sourceInterface : edge.targetPort; return `${nodeNameByID.value[id] || id}${port ? ` · ${port}` : ''}` }
async function load() {
  loading.value = true
  try {
    if (mode.value === 'physical') {
      graph.value = await fetchPhysicalTopology(auth.token)
      snapshots.value = await fetchTopologySnapshots(auth.token, 'physical', '', 12)
      selected.value = graph.value.nodes[0]
      impact.value = undefined
    } else {
      const response = await fetch('/api/v1/topology/graph', { headers: headers.value })
      graph.value = await response.json()
    }
  } finally { loading.value = false }
}
async function switchMode(value:Mode) { mode.value = value; selected.value = undefined; impact.value = undefined; query.value = ''; await load() }
async function select(node:Node) {
  selected.value = node
  if (mode.value === 'physical') { impact.value = undefined; return }
  const response = await fetch(`/api/v1/topology/nodes/${encodeURIComponent(node.id)}/impact`, { headers: headers.value })
  impact.value = await response.json()
}
onMounted(load)
</script>

<template>
  <section class="topology-page">
    <header class="topology-heading">
      <div><span>RELATIONSHIP &amp; IMPACT MAP</span><h1>{{ mode === 'physical' ? '物理网络拓扑' : '资产拓扑中心' }}</h1><p>{{ mode === 'physical' ? '服务器网卡 → 交换机端口 → LLDP 邻居关系' : '项目 → 应用 → 服务 → 实例 → 基础设施' }}</p></div>
      <div><label><Search/><input v-model="query" placeholder="搜索拓扑节点"></label><button @click="load"><RefreshCw :class="{spin:loading}"/>刷新</button><button><Maximize2/>全屏画布</button></div>
    </header>
    <nav class="topology-mode-tabs"><button :class="{active:mode==='business'}" @click="switchMode('business')"><GitBranch/>业务关系拓扑</button><button :class="{active:mode==='physical'}" @click="switchMode('physical')"><Cable/>物理网络拓扑</button></nav>
    <div class="topology-stats"><span>节点 <b>{{graph.nodes.length}}</b></span><span>关系 <b>{{graph.edges.length}}</b></span><template v-if="mode==='physical'"><span>服务器 <b>{{physicalNodeCount}}</b></span><span>网络设备 <b>{{physicalSwitchCount}}</b></span></template><span v-else>异常 <b>{{graph.nodes.filter(n=>n.status!=='healthy').length}}</b></span><span>更新时间 <b>{{graph.updatedAt}}</b></span></div>
    <div v-if="mode==='physical'" class="snapshot-strip"><button class="primary" @click="captureSnapshot"><Camera/>保存当前快照</button><button v-if="snapshotView" @click="returnLive"><RotateCcw/>返回实时拓扑</button><span v-for="snapshot in snapshots.slice(0,6)" :key="snapshot.id" class="snapshot-item"><Clock3/><button @click="viewSnapshot(snapshot.id)"><strong>{{ snapshot.createdAt }}</strong><small>{{ snapshot.nodeCount }} 节点 · {{ snapshot.edgeCount }} 链路 · {{ snapshot.operator }}</small></button></span></div>
    <div class="topology-workspace">
      <main class="graph-canvas" :class="{physical:mode==='physical'}">
        <div class="layer-labels"><span v-for="(name,i) in mode==='physical' ? ['服务器侧','网络设备侧'] : ['项目','应用','服务','实例','基础设施']" :key="name" :style="{left:(45+(mode==='physical'?i*600:i*220))+'px'}">{{name}}</span></div>
        <svg :viewBox="mode==='physical' ? '0 0 1200 620' : '0 0 1100 440'" preserveAspectRatio="xMinYMin meet"><defs><marker id="arrow" markerWidth="8" markerHeight="8" refX="7" refY="3" orient="auto"><path d="M0,0 L0,6 L8,3 z"/></marker></defs><g class="edges"><path v-for="edge in graph.edges" :key="edge.id" :d="`M ${positions[edge.source]?.x+65} ${positions[edge.source]?.y} C ${positions[edge.source]?.x+120} ${positions[edge.source]?.y}, ${positions[edge.target]?.x-120} ${positions[edge.target]?.y}, ${positions[edge.target]?.x-65} ${positions[edge.target]?.y}`"/><text v-for="edge in graph.edges" :key="'t'+edge.id" :x="(positions[edge.source]?.x+positions[edge.target]?.x)/2" :y="(positions[edge.source]?.y+positions[edge.target]?.y)/2-7">{{edge.relation}}</text></g></svg>
        <button v-for="node in visibleNodes" :key="node.id" class="graph-node" :class="{selected:selected?.id===node.id}" :data-status="node.status" :style="{left:(positions[node.id]?.x-65)+'px',top:(positions[node.id]?.y-34)+'px'}" @click="select(node)"><Network v-if="mode==='physical'&&node.assetType==='network-device'"/><Server v-else/><span><strong>{{node.name}}</strong><small>{{node.ip||node.kind}}</small><em v-if="mode==='physical'&&node.portCount">{{node.upPortCount}} / {{node.portCount}} 端口</em></span><i></i></button>
        <div class="graph-legend"><span v-for="s in ['healthy','warning','critical']" :key="s"><i :data-status="s"></i>{{healthLabel(s)}}</span></div>
      </main>
      <aside v-if="selected" class="impact-panel">
        <header><div><span>{{ mode==='physical' ? 'PHYSICAL LINK DETAIL' : 'IMPACT ANALYSIS' }}</span><h2>{{selected.name}}</h2></div><button @click="selected=undefined;impact=undefined"><X/></button></header>
        <template v-if="mode==='physical'">
          <div class="impact-score"><Waypoints/><div><strong>{{ selectedEdges.length }}</strong><span>关联物理链路</span></div></div>
          <dl><div><dt>节点类型</dt><dd>{{ selected.kind }}</dd></div><div><dt>健康状态</dt><dd :data-status="selected.status">{{healthLabel(selected.status)}}</dd></div><div><dt>IP 地址</dt><dd>{{selected.ip||'—'}}</dd></div><div><dt>位置</dt><dd>{{selected.location||'—'}}</dd></div><div><dt>端口摘要</dt><dd>{{selected.portCount ? `${selected.upPortCount} / ${selected.portCount} 在线` : '—'}}</dd></div></dl>
          <h3>物理链路 <small>基于人工绑定和 LLDP 自动发现</small></h3>
          <div class="affected-list physical-links"><div v-for="edge in selectedEdges" :key="edge.id"><Cable/><span><strong>{{ endpointLabel(edge,'source') }}</strong><small>→ {{ endpointLabel(edge,'target') }}</small><small>{{ edge.sourceType==='lldp' ? 'LLDP 自动发现' : '人工绑定' }} · {{ edge.lastSeenAt || '时间未知' }}</small><small class="edge-verification" :data-tone="verificationTone(edge.verification)">{{ verificationLabel(edge.verification) }} {{ edge.confidence || 0 }}% · {{ (edge.evidence||[]).join('；') }}</small></span><b>{{edge.relation}}</b></div><div v-if="!selectedEdges.length" class="rule-empty">当前节点没有已发现的物理链路</div></div>
        </template>
        <template v-else>
          <div class="impact-score"><GitBranch/><div><strong>{{ impactCount(impact) }}</strong><span>关联影响节点</span></div></div>
          <div class="impact-summary"><span data-status="critical">严重 {{impact?.summary.critical||0}}</span><span data-status="warning">关注 {{impact?.summary.warning||0}}</span><span data-status="healthy">健康 {{impact?.summary.healthy||0}}</span></div>
          <dl><div><dt>节点类型</dt><dd>{{selected.kind}}</dd></div><div><dt>健康状态</dt><dd :data-status="selected.status">{{healthLabel(selected.status)}}</dd></div><div><dt>负责人</dt><dd>{{selected.owner}}</dd></div><div><dt>IP 地址</dt><dd>{{selected.ip||'—'}}</dd></div></dl>
          <h3>影响范围 <small>异常优先 · 已展开上下游依赖路径</small></h3>
          <div class="affected-list"><button v-for="node in impact?.affected" :key="node.id" @click="select(node)"><i :data-status="node.status"></i><span><strong>{{node.name}}</strong><small>{{node.kind}} · {{directionLabel(node.direction)}} · 距离 {{node.distance}}</small><small class="impact-path">{{pathLabel(node)}}</small></span><b>查看</b></button><div v-if="impact && !impact.affected.length" class="rule-empty">当前没有关联影响节点</div></div>
        </template>
      </aside>
    </div>
  </section>
</template>

<style scoped>
.topology-mode-tabs{display:flex;gap:8px;margin-top:14px}.topology-mode-tabs button{display:flex;align-items:center;gap:7px}.topology-mode-tabs button.active{border-color:#22b9df;background:#0b3952;color:#dff9ff;box-shadow:inset 0 -2px #26d3ee}.graph-canvas.physical>svg{height:620px}.graph-node em{display:block;color:#42c9e8;font-size:9px;font-style:normal;margin-top:3px}.physical-links>div{width:calc(100% - 30px);margin:6px 15px;padding:9px;background:#0c293f;display:grid;grid-template-columns:18px 1fr auto;gap:8px;align-items:center}.physical-links>div>svg{width:15px;color:#31c6e7}.physical-links span{display:grid;gap:3px}.physical-links strong{font-size:10px;font-weight:500}.physical-links small{color:#668096;font-size:9px}.physical-links>b{font-size:9px;color:#29cde9;font-weight:500}
.snapshot-strip{display:flex;align-items:center;gap:7px;padding:9px;border:1px solid #173a54;border-top:0;background:#071d30;overflow-x:auto}.snapshot-strip>button{white-space:nowrap}.snapshot-strip>button.primary{background:#0b6da5;border-color:#1a93c8}.snapshot-item{display:flex;align-items:center;gap:5px;border:1px solid #1e435d;background:#04182a;padding:3px 6px}.snapshot-item>svg{width:13px;color:#32c7e8}.snapshot-item button{display:grid!important;text-align:left;padding:3px 5px!important;border:0!important;background:none!important}.snapshot-item strong{font-size:9px;font-weight:500}.snapshot-item small{font-size:8px;color:#65859a}.edge-verification{display:block;color:#7ca0b4!important}.edge-verification[data-tone=healthy]{color:#58dda0!important}.edge-verification[data-tone=warning]{color:#ffc167!important}.edge-verification[data-tone=critical]{color:#ff8292!important}
</style>
