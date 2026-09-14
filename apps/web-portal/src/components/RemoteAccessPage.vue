<script setup lang="ts">
import { MonitorCog, ShieldCheck, Waypoints } from 'lucide-vue-next'
import RemoteAccessPanel from './RemoteAccessPanel.vue'
</script>

<template>
  <section class="remote-workbench">
    <header>
      <div>
        <span>SECURE REMOTE OPERATIONS</span>
        <h1>远程运维工作台</h1>
        <p>统一管理 WebSSH 会话、协作接入、文件传输、审批策略和录像归档。</p>
      </div>
      <div class="remote-capabilities">
        <article><ShieldCheck/><span><b>安全审批</b><small>高风险命令与策略管控</small></span></article>
        <article><Waypoints/><span><b>多实例接入</b><small>跨网关副本协同与状态同步</small></span></article>
        <article><MonitorCog/><span><b>全程审计</b><small>终端录像、命令与文件记录</small></span></article>
      </div>
    </header>
    <RemoteAccessPanel />
  </section>
</template>

<style scoped>
.remote-workbench{min-height:100%;padding:18px 0 28px}.remote-workbench>header{display:flex;justify-content:space-between;gap:22px;align-items:flex-end;padding:0 20px}.remote-workbench>header>div:first-child{display:grid;gap:6px}.remote-workbench>header span{color:#22d3ee;font:10px/1.2 monospace;letter-spacing:.18em}.remote-workbench h1{margin:0;font-size:24px;color:#e6f7ff}.remote-workbench p{margin:0;color:#7894a8;font-size:12px}.remote-capabilities{display:flex;gap:9px}.remote-capabilities article{display:flex;align-items:center;gap:8px;min-width:170px;padding:11px 13px;border:1px solid #16445c;border-radius:10px;background:linear-gradient(135deg,#071925,#082638)}.remote-capabilities svg{width:18px;color:#22d3ee}.remote-capabilities span{display:grid;gap:2px}.remote-capabilities b{font-size:12px;color:#d8f3ff}.remote-capabilities small{color:#64869a;font-size:9px}@media(max-width:1200px){.remote-workbench>header{align-items:flex-start;flex-direction:column}.remote-capabilities{flex-wrap:wrap}}
</style>
