<script setup lang="ts">
import {ref,watch} from 'vue'
import {Activity,AlertTriangle} from 'lucide-vue-next'
import {fetchHostMonitoring,type HostMonitoring} from '@/api/monitor'
import {useAuthStore} from '@/stores/useAuthStore'
const props=defineProps<{assetId:string}>()
const auth=useAuthStore(),data=ref<HostMonitoring>(),loading=ref(false),error=ref('')
function height(value:number,values:number[]){const max=Math.max(...values,1);return `${Math.max(8,Math.round(value/max*100))}%`}
async function load(){loading.value=true;error.value='';try{data.value=await fetchHostMonitoring(auth.token,props.assetId)}catch(reason){error.value=reason instanceof Error?reason.message:'监控数据加载失败'}finally{loading.value=false}}
watch(()=>props.assetId,load,{immediate:true})
</script>
<template><Teleport to=".asset-drawer"><section class="host-monitoring"><h3><Activity/>实时监控</h3><div v-if="loading" class="host-monitor-state">正在读取 Prometheus 指标…</div><div v-else-if="error" class="host-monitor-state error">{{error}}</div><div v-else-if="data&&!data.monitored" class="host-monitor-state">该资产尚未接入 node_exporter，暂无真实监控数据</div><template v-else-if="data"><div class="host-monitor-health"><i :class="data.up?'up':'down'"></i><strong>{{data.up?'采集正常':'采集异常'}}</strong><span>Prometheus 实时数据</span></div><div class="host-metric-grid"><article v-for="metric in data.metrics" :key="metric.id"><span>{{metric.name}}</span><strong>{{metric.value}}<small>{{metric.unit}}</small></strong><div class="host-spark"><i v-for="(point,index) in metric.trend" :key="index" :style="{height:height(point,metric.trend)}"></i></div></article></div><div v-if="data.alerts.length" class="host-alerts"><h4><AlertTriangle/>关联告警（{{data.alerts.length}}）</h4><article v-for="alert in data.alerts" :key="alert.id" :data-severity="alert.severity"><strong>{{alert.title}}</strong><span>{{alert.status}} · {{alert.startedAt}}</span></article></div></template></section></Teleport></template>
