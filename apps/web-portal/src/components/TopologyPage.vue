<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { GitBranch, Maximize2, RefreshCw, Search, Server, X } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'
import { healthLabel, impactCount } from '@/features/topology/topologyModel'

type Node = { id:string; name:string; kind:string; layer:number; status:string; owner:string; ip?:string }
type Edge = { id:string; source:string; target:string; relation:string }
type Graph = { nodes:Node[]; edges:Edge[]; updatedAt:string }
type ImpactNode = Node & { distance:number; direction:'upstream'|'downstream'|string; path:string[] }
type Impact = { root:Node; affected:ImpactNode[]; paths:string[][]; summary:{ total:number; healthy:number; warning:number; critical:number } }
const auth = useAuthStore()
const graph = ref<Graph>({ nodes: [], edges: [], updatedAt: '' })
const selected = ref<Node>()
const impact = ref<Impact>()
const loading = ref(false)
const query = ref('')
const headers = computed(() => ({ Authorization: `Bearer ${auth.token}` }))
const positions = computed(() => {
  const grouped = new Map<number, Node[]>()
  graph.value.nodes.forEach(node => grouped.set(node.layer, [...(grouped.get(node.layer) || []), node]))
  const out: Record<string, { x:number; y:number }> = {}
  grouped.forEach((nodes, layer) => nodes.forEach((node, index) => {
    out[node.id] = { x: 100 + layer * 220, y: 100 + index * 150 + (nodes.length === 1 ? 75 : 0) }
  }))
  return out
})
const visibleNodes = computed(() => graph.value.nodes.filter(node => !query.value || node.name.toLowerCase().includes(query.value.toLowerCase())))
const nodeNameByID = computed(() => Object.fromEntries(graph.value.nodes.map(node => [node.id, node.name])))
function directionLabel(direction:string) {
  return direction === 'upstream' ? '向上游影响' : direction === 'downstream' ? '向下游影响' : '关联影响'
}
function pathLabel(node:ImpactNode) {
  return (node.path || []).map(id => nodeNameByID.value[id] || id).join(' → ')
}
async function load() {
  loading.value = true
  try {
    const response = await fetch('/api/v1/topology/graph', { headers: headers.value })
    graph.value = await response.json()
  } finally {
    loading.value = false
  }
}
async function select(node:Node) {
  selected.value = node
  const response = await fetch(`/api/v1/topology/nodes/${encodeURIComponent(node.id)}/impact`, { headers: headers.value })
  impact.value = await response.json()
}
onMounted(load)
</script>
<template>
  <section class="topology-page">
    <header class="topology-heading">
      <div><span>RELATIONSHIP &amp; IMPACT MAP</span><h1>资产拓扑中心</h1><p>项目 → 应用 → 服务 → 实例 → 基础设施</p></div>
      <div><label><Search/><input v-model="query" placeholder="搜索拓扑节点"></label><button @click="load"><RefreshCw :class="{spin:loading}"/>刷新</button><button><Maximize2/>全屏画布</button></div>
    </header>
    <div class="topology-stats"><span>节点 <b>{{graph.nodes.length}}</b></span><span>关系 <b>{{graph.edges.length}}</b></span><span>异常 <b>{{graph.nodes.filter(n=>n.status!=='healthy').length}}</b></span><span>更新时间 <b>{{graph.updatedAt}}</b></span></div>
    <div class="topology-workspace">
      <main class="graph-canvas">
        <div class="layer-labels"><span v-for="(name,i) in ['项目','应用','服务','实例','基础设施']" :key="name" :style="{left:(45+i*220)+'px'}">{{name}}</span></div>
        <svg viewBox="0 0 1100 440" preserveAspectRatio="xMinYMin meet"><defs><marker id="arrow" markerWidth="8" markerHeight="8" refX="7" refY="3" orient="auto"><path d="M0,0 L0,6 L8,3 z"/></marker></defs><g class="edges"><path v-for="edge in graph.edges" :key="edge.id" :d="`M ${positions[edge.source]?.x+65} ${positions[edge.source]?.y} C ${positions[edge.source]?.x+120} ${positions[edge.source]?.y}, ${positions[edge.target]?.x-120} ${positions[edge.target]?.y}, ${positions[edge.target]?.x-65} ${positions[edge.target]?.y}`"/><text v-for="edge in graph.edges" :key="'t'+edge.id" :x="(positions[edge.source]?.x+positions[edge.target]?.x)/2" :y="(positions[edge.source]?.y+positions[edge.target]?.y)/2-7">{{edge.relation}}</text></g></svg>
        <button v-for="node in visibleNodes" :key="node.id" class="graph-node" :class="{selected:selected?.id===node.id}" :data-status="node.status" :style="{left:(positions[node.id]?.x-65)+'px',top:(positions[node.id]?.y-34)+'px'}" @click="select(node)"><Server/><span><strong>{{node.name}}</strong><small>{{node.ip||node.kind}}</small></span><i></i></button>
        <div class="graph-legend"><span v-for="s in ['healthy','warning','critical']" :key="s"><i :data-status="s"></i>{{healthLabel(s)}}</span></div>
      </main>
      <aside v-if="selected" class="impact-panel">
        <header><div><span>IMPACT ANALYSIS</span><h2>{{selected.name}}</h2></div><button @click="selected=undefined;impact=undefined"><X/></button></header>
        <div class="impact-score"><GitBranch/><div><strong>{{impactCount(impact)}}</strong><span>关联影响节点</span></div></div>
        <div class="impact-summary"><span data-status="critical">严重 {{impact?.summary.critical||0}}</span><span data-status="warning">关注 {{impact?.summary.warning||0}}</span><span data-status="healthy">健康 {{impact?.summary.healthy||0}}</span></div>
        <dl><div><dt>节点类型</dt><dd>{{selected.kind}}</dd></div><div><dt>健康状态</dt><dd :data-status="selected.status">{{healthLabel(selected.status)}}</dd></div><div><dt>负责人</dt><dd>{{selected.owner}}</dd></div><div><dt>IP 地址</dt><dd>{{selected.ip||'—'}}</dd></div></dl>
        <h3>影响范围 <small>异常优先 · 已展开上下游依赖路径</small></h3>
        <div class="affected-list">
          <button v-for="node in impact?.affected" :key="node.id" @click="select(node)"><i :data-status="node.status"></i><span><strong>{{node.name}}</strong><small>{{node.kind}} · {{directionLabel(node.direction)}} · 距离 {{node.distance}}</small><small class="impact-path">{{pathLabel(node)}}</small></span><b>查看</b></button>
          <div v-if="impact && !impact.affected.length" class="rule-empty">当前没有关联影响节点</div>
        </div>
      </aside>
    </div>
  </section>
</template>
