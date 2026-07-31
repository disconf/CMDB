<script setup lang="ts">
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { onBeforeUnmount, onMounted, useTemplateRef, watch } from 'vue'

const props = defineProps<{ points: Array<{ time:string; total:number; critical:number }> }>()
echarts.use([LineChart, GridComponent, TooltipComponent, CanvasRenderer])
const chartEl = useTemplateRef<HTMLDivElement>('chartEl')
let chartInstance: echarts.ECharts | undefined
function render() { if (!chartInstance) return; chartInstance.setOption({ tooltip:{trigger:'axis'}, grid:{left:36,right:12,top:18,bottom:28}, xAxis:{type:'category',data:props.points.map(p=>p.time),axisLine:{lineStyle:{color:'#244b6b'}},axisLabel:{color:'#7494ad'}}, yAxis:{type:'value',splitLine:{lineStyle:{color:'rgba(84,157,202,.12)'}},axisLabel:{color:'#7494ad'}}, series:[{name:'告警总数',type:'line',smooth:true,data:props.points.map(p=>p.total),symbol:'circle',symbolSize:5,lineStyle:{color:'#ff6474',width:2},itemStyle:{color:'#ff6474'},areaStyle:{color:new echarts.graphic.LinearGradient(0,0,0,1,[{offset:0,color:'rgba(255,100,116,.28)'},{offset:1,color:'rgba(255,100,116,0)'}])}},{name:'严重告警',type:'line',smooth:true,data:props.points.map(p=>p.critical),symbol:'circle',symbolSize:4,lineStyle:{color:'#ffae48',width:2},itemStyle:{color:'#ffae48'}}] }) }
onMounted(()=>{ if(chartEl.value){ chartInstance=echarts.init(chartEl.value); render(); window.addEventListener('resize',resize) } });
watch(()=>props.points,render,{deep:true}); function resize(){ chartInstance?.resize() } onBeforeUnmount(()=>{window.removeEventListener('resize',resize);chartInstance?.dispose()})
</script>
<template><div ref="chartEl" class="chart" role="img" aria-label="过去二十四小时告警趋势"></div></template>
