<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { FileUp, LayoutDashboard, MonitorCog, Play, ShieldAlert, ShieldCheck, Terminal, Waypoints } from 'lucide-vue-next'
import RemoteAccessPanel from './RemoteAccessPanel.vue'
import RemoteExecutionPanel from './RemoteExecutionPanel.vue'
import { remoteSections, type RemoteSection } from '@/features/remote/remoteNavigation'

const props = defineProps<{ section?: string }>()
const router = useRouter()
const active = computed<RemoteSection>(() => remoteSections.some(item => item.id === props.section) ? props.section as RemoteSection : 'overview')
const current = computed(() => remoteSections.find(item => item.id === active.value) || remoteSections[0])
const sectionIcons = { overview: LayoutDashboard, sessions: Terminal, files: FileUp, execution: Play, approvals: ShieldAlert, security: ShieldCheck }
const quickSections = computed(() => remoteSections.filter(item => item.id !== 'overview'))
</script>

<template>
  <section class="remote-workbench" :data-view="active">
    <header>
      <div>
        <span>SECURE REMOTE OPERATIONS</span>
        <h1>{{ current.name }}</h1>
        <p>{{ current.description }}</p>
      </div>
      <div class="remote-capabilities">
        <article><ShieldCheck/><span><b>安全审批</b><small>高风险命令与策略管控</small></span></article>
        <article><Waypoints/><span><b>多实例接入</b><small>跨网关副本协同与状态同步</small></span></article>
        <article><MonitorCog/><span><b>全程审计</b><small>终端录像、命令与文件记录</small></span></article>
      </div>
    </header>



    <div v-if="active === 'overview'" class="remote-overview">
      <section class="remote-overview-hero">
        <div>
          <span>REMOTE OPERATIONS CONTROL PLANE</span>
          <h2>统一入口，按任务拆分操作界面</h2>
          <p>终端、文件、批量执行和安全策略通过左侧远程运维二级菜单切换，避免页面内重复导航。</p>
        </div>
        <div class="remote-flow">
          <span><b>1</b>身份校验</span><i></i><span><b>2</b>资产授权</span><i></i><span><b>3</b>策略与审批</span><i></i><span><b>4</b>会话审计</span>
        </div>
      </section>

      <div class="remote-quick-grid">
        <button v-for="item in quickSections" :key="item.id" @click="router.push(item.path)">
          <component :is="sectionIcons[item.id]"/>
          <span><strong>{{ item.name }}</strong><small>{{ item.description }}</small></span>
          <em>进入</em>
        </button>
      </div>
    </div>

    <RemoteAccessPanel v-else-if="['sessions','files','approvals','security'].includes(active)" :view="active"/>
    <RemoteExecutionPanel v-else-if="active === 'execution'"/>
  </section>
</template>

<style scoped>
.remote-workbench{min-height:100%;padding:18px 0 30px}
.remote-workbench>header{display:flex;justify-content:space-between;gap:22px;align-items:flex-end;padding:0 20px}
.remote-workbench>header>div:first-child{display:grid;gap:6px}
.remote-workbench>header span{color:#22d3ee;font:10px/1.2 monospace;letter-spacing:.18em}
.remote-workbench h1{margin:0;font-size:24px;color:#e6f7ff}
.remote-workbench p{margin:0;color:#7894a8;font-size:12px}
.remote-capabilities{display:flex;gap:9px}
.remote-capabilities article{display:flex;align-items:center;gap:8px;min-width:170px;padding:11px 13px;border:1px solid #16445c;border-radius:10px;background:linear-gradient(135deg,#071925,#082638)}
.remote-capabilities svg{width:18px;color:#22d3ee}
.remote-capabilities span{display:grid;gap:2px}
.remote-capabilities b{font-size:12px;color:#d8f3ff}
.remote-capabilities small{color:#64869a;font-size:9px}
.remote-section-nav{display:grid;grid-template-columns:repeat(6,minmax(0,1fr));gap:8px;margin:18px 20px 0;padding:8px;border:1px solid #123043;border-radius:12px;background:#04121c}
.remote-section-nav button{display:grid;grid-template-columns:auto 1fr;gap:9px;align-items:center;min-width:0;padding:10px;border:1px solid transparent;border-radius:9px;background:transparent;color:#7f9fb2;text-align:left;cursor:pointer}
.remote-section-nav button:hover{border-color:#17415a;background:#082638;color:#dcecff}
.remote-section-nav button.active{border-color:#155e75;background:#082b38;color:#a5f3fc;box-shadow:inset 0 -2px #22d3ee}
.remote-section-nav svg{width:18px}
.remote-section-nav span{display:grid;gap:2px;min-width:0}
.remote-section-nav strong{font-size:11px}
.remote-section-nav small{overflow:hidden;color:#64748b;font-size:9px;text-overflow:ellipsis;white-space:nowrap}
.remote-overview{padding:16px 20px 0}
.remote-overview-hero{display:grid;grid-template-columns:minmax(280px,1fr) minmax(520px,1.4fr);gap:24px;align-items:center;padding:22px;border:1px solid #16445c;border-radius:14px;background:linear-gradient(135deg,#061a29,#082638)}
.remote-overview-hero span{color:#22d3ee;font:10px/1.2 monospace;letter-spacing:.15em}
.remote-overview-hero h2{margin:7px 0;color:#e6f7ff;font-size:20px}
.remote-overview-hero p{max-width:620px;color:#7f9fb2;line-height:1.8}
.remote-flow{display:flex;align-items:center;justify-content:flex-end;gap:8px}
.remote-flow span{display:grid;justify-items:center;gap:5px;padding:10px 12px;border:1px solid #17415a;border-radius:9px;background:#061725;color:#9bdffc;font-size:10px;white-space:nowrap}
.remote-flow b{display:grid;place-items:center;width:20px;height:20px;border-radius:50%;background:#0e7490;color:#ecfeff}
.remote-flow i{width:24px;height:1px;background:#1d5f7b}
.remote-quick-grid{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:10px;margin-top:12px}
.remote-quick-grid button{display:grid;grid-template-columns:auto 1fr auto;gap:10px;align-items:center;min-height:92px;padding:14px;border:1px solid #173a52;border-radius:12px;background:#081f31;color:#dcecff;text-align:left;cursor:pointer}
.remote-quick-grid button:hover{border-color:#22d3ee;background:#0a2a3e;transform:translateY(-1px)}
.remote-quick-grid svg{width:22px;color:#22d3ee}
.remote-quick-grid span{display:grid;gap:5px}
.remote-quick-grid strong{font-size:12px}
.remote-quick-grid small{color:#6f91a5;font-size:9px;line-height:1.5}
.remote-quick-grid em{color:#67e8f9;font-size:10px;font-style:normal}
@media(max-width:1450px){.remote-section-nav{grid-template-columns:repeat(3,1fr)}.remote-quick-grid{grid-template-columns:repeat(3,1fr)}.remote-overview-hero{grid-template-columns:1fr}.remote-flow{justify-content:flex-start;flex-wrap:wrap}.remote-capabilities{display:none}}
@media(max-width:900px){.remote-workbench>header{padding:0 14px;flex-direction:column;align-items:flex-start}.remote-section-nav{grid-template-columns:1fr 1fr;margin:14px 14px 0}.remote-overview{padding:14px 14px 0}.remote-quick-grid{grid-template-columns:1fr 1fr}.remote-flow i{display:none}}
</style>