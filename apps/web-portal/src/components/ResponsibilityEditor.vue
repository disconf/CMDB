<script setup lang="ts">
withDefaults(defineProps<{
  owner: string
  projectGroup: string
  location?: string
  showAssignment?: boolean
  showLocation?: boolean
  locationReadonly?: boolean
  note?: string
  showNote?: boolean
}>(), { showAssignment: true, showLocation: false, locationReadonly: false, showNote: false })
import ProjectGroupSelect from '@/components/ProjectGroupSelect.vue'

const emit = defineEmits<{
  'update:owner': [value: string]
  'update:projectGroup': [value: string]
  'update:location': [value: string]
  'update:note': [value: string]
}>()
</script>

<template>
  <section class="responsibility-editor">
    <header><div><strong>责任信息</strong><span>负责人和项目组统一在这里维护，治理页面复用同一组件。</span></div></header>
    <div class="responsibility-grid">
      <label v-if="showAssignment"><span>负责人 *</span><input :value="owner" placeholder="姓名或运维团队" @input="emit('update:owner', ($event.target as HTMLInputElement).value)"></label>
      <label v-if="showAssignment" class="wide"><span>项目组 *</span><ProjectGroupSelect :model-value="projectGroup" @update:model-value="emit('update:projectGroup', $event)"/></label>
      <label v-if="showLocation" class="wide"><span>机房 / 位置</span><input :value="location" :readonly="locationReadonly" placeholder="物理机直接维护，虚拟机从宿主继承" @input="emit('update:location', ($event.target as HTMLInputElement).value)"></label>
      <label v-if="showNote" class="wide"><span>治理说明</span><textarea :value="note" rows="3" placeholder="记录分派、确认或退役原因" @input="emit('update:note', ($event.target as HTMLTextAreaElement).value)"></textarea></label>
    </div>
  </section>
</template>

<style scoped>
.responsibility-editor{margin-top:14px;border-top:1px dashed var(--line,#2a3550);padding-top:12px}.responsibility-editor>header{display:flex;justify-content:space-between;align-items:center}.responsibility-editor strong{display:block;font-size:14px}.responsibility-editor header span{display:block;margin-top:4px;color:#6f8ca1;font-size:10px}.responsibility-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:10px}.responsibility-grid label{display:grid;gap:5px}.responsibility-grid label.wide{grid-column:1/-1}.responsibility-grid span{color:#7890a8;font-size:11px}.responsibility-grid input,.responsibility-grid textarea{border:1px solid #28536c;background:#031525;color:#d5edf8;border-radius:6px;padding:8px 9px;font:inherit;font-size:12px}.responsibility-grid input[readonly]{opacity:.7;cursor:not-allowed}@media(max-width:800px){.responsibility-grid{grid-template-columns:1fr}.responsibility-grid label.wide{grid-column:auto}}
</style>
