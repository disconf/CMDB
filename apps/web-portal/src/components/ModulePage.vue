<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { Boxes,Construction,Database,ShieldCheck,Workflow } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/useAuthStore'
const route=useRoute(),auth=useAuthStore();const module=computed(()=>auth.modules.find(item=>item.path===route.path));
const features=computed(()=>({cmdb:['CI 模型管理','资产台账','IDC 与机柜','云与 Kubernetes 资源'],topology:['基础设施拓扑','应用依赖','影响分析','历史快照'],monitor:['监控目标','告警规则','告警中心','通知策略'],jobs:['作业模板','任务执行','调度计划','执行日志'],tickets:['我的工单','流程审批','变更工单','资源申请'],releases:['流水线','制品版本','发布申请','回滚记录'],toolbox:['网络诊断','主机诊断','K8s 诊断','日志检索'],aiops:['智能分析','知识库','Runbook','受控执行'],system:['用户与组织','角色权限','模块中心','审计日志']}[module.value?.code||'']||[]))
</script>
<template><section class="module-page"><header><div><span>业务模块</span><h1>{{module?.name||'功能模块'}}</h1><p>模块框架已经接入统一登录、数据权限与动态导航。</p></div><b><Construction/>开发中</b></header><div class="module-overview"><article><Database/><div><span>数据边界</span><strong>独立领域服务</strong></div></article><article><ShieldCheck/><div><span>访问控制</span><strong>{{module?.permission}}</strong></div></article><article><Workflow/><div><span>协作方式</span><strong>API + 领域事件</strong></div></article></div><div class="module-feature-grid"><button v-for="feature in features" :key="feature"><Boxes/><span>{{feature}}</span><small>进入功能</small></button></div></section></template>
