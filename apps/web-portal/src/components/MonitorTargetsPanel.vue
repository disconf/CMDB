<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Activity, RefreshCw } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'

type Target = { assetId:string; assetName:string; assetType:string; target:string; port:string; exporter:string; status:string; source:string; lastSeenAt:string }
const auth = useAuthStore()
const targets = ref<Target[]>([])
const loading = ref(false)
const error = ref('')
const configured = computed(() => targets.value.filter(item => item.target).length)
const pending = computed(() => targets.value.filter(item => !item.target).length)
async function load(){loading.value=true;error.value='';try{const response=await fetch('/api/v1/cmdb/monitor-targets',{headers:{Authorization:`Bearer ${auth.token}`}});if(!response.ok)throw new Error(`请求失败 (${response.status})`);targets.value=await response.json()}catch(reason){error.value=reason instanceof Error?reason.message:'监控目标加载失败'}finally{loading.value=false}}
onMounted(load)
</script>

<template>
  <section class="monitor-targets-panel">
    <header><div><span>PROMETHEUS SERVICE DISCOVERY</span><h2>监控目标管理</h2><p>CMDB 负责登记 Exporter 目标，Prometheus 负责采集和告警计算。</p></div><button :disabled="loading" @click="load"><RefreshCw :class="{spin:loading}"/>刷新</button></header>
    <div class="target-summary"><article><Activity/><strong>{{targets.length}}</strong><span>全部目标</span></article><article><strong>{{configured}}</strong><span>已配置 Exporter</span></article><article><strong>{{pending}}</strong><span>待补充 Exporter</span></article></div>
    <div v-if="error" class="target-error">{{error}}</div>
    <div class="target-table"><div class="target-row target-head"><span>资产</span><span>类型</span><span>采集目标</span><span>Exporter</span><span>状态</span><span>数据来源</span></div><div v-for="item in targets" :key="`${item.assetId}-${item.exporter}`" class="target-row"><span><strong>{{item.assetName}}</strong><small>{{item.assetId}}</small></span><span>{{item.assetType}}</span><span class="mono">{{item.target||'待配置'}}</span><span>{{item.exporter}}</span><b :data-status="item.status">{{item.status}}</b><span>{{item.source}}</span></div><div v-if="!targets.length" class="target-empty">暂无监控目标，请先完成资产 Exporter 字段登记。</div></div>
  </section>
</template>

<style scoped>
.monitor-targets-panel{padding:18px;border:1px solid #173b55;border-radius:12px;background:#071925}.monitor-targets-panel>header{display:flex;justify-content:space-between;gap:16px;align-items:flex-start}.monitor-targets-panel header span{color:#22d3ee;font:10px/1.2 monospace;letter-spacing:.15em}.monitor-targets-panel h2{margin:5px 0}.monitor-targets-panel p{margin:0;color:#7894a8;font-size:11px}.monitor-targets-panel button{display:flex;gap:6px;align-items:center;padding:8px 11px;border:1px solid #23455f;background:#0b263b;color:#bdd8e9;cursor:pointer}.target-summary{display:grid;grid-template-columns:repeat(3,1fr);gap:10px;margin:14px 0}.target-summary article{display:grid;grid-template-columns:auto 1fr;gap:3px 9px;align-items:center;padding:13px;border:1px solid #173b55;background:#0b273c}.target-summary svg{grid-row:1/3;color:#22d3ee}.target-summary strong{font-size:22px;color:#e6f7ff}.target-summary span{color:#7894a8;font-size:10px}.target-error{padding:9px 11px;border:1px solid #7b3140;background:#351723;color:#ff9cad}.target-table{margin-top:12px;border:1px solid #173b55}.target-row{display:grid;grid-template-columns:1.3fr .8fr 1.2fr .9fr .7fr .8fr;gap:10px;align-items:center;min-height:46px;padding:8px 11px;border-bottom:1px solid #123043;color:#c9e6f5;font-size:11px}.target-head{color:#6f91a5;font-size:10px}.target-row small{display:block;color:#64748b}.target-row .mono{font-family:monospace;color:#7dd3fc}.target-row b{font-size:10px;color:#2dd4bf}.target-row b[data-status=pending]{color:#fbbf24}.target-row b[data-status=warning]{color:#fbbf24}.target-row b[data-status=offline]{color:#fb7185}.target-empty{padding:30px;text-align:center;color:#64748b}@media(max-width:1100px){.target-row{grid-template-columns:1fr 1fr}.target-head{display:none}}
</style>