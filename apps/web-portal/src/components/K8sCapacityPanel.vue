<template>
  <div class="k8s-capacity-wrap">
    <div v-if="loading" class="capacity-loading">正在读取集群容量…</div>
    <div v-else-if="capacity" class="k8s-capacity">
      <article v-for="card in cards" :key="card.label" :data-available="card.available">
        <span>{{ card.label }}</span>
        <strong>{{ card.available ? `${card.used} / ${card.capacity}${card.unit}` : '数据不可用' }}</strong>
        <div v-if="card.available" class="capacity-bar">
          <i :style="{ width: Math.min(card.percent, 100) + '%' }"></i>
        </div>
        <small>{{ card.available ? card.percent + '%' : (capacity.message || '指标不可用') }}</small>
      </article>
    </div>
    <div v-else-if="error" class="capacity-error">{{ error }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { fetchKubernetesCapacity } from '@/api/cmdb'
import type { KubernetesCapacity } from '@/features/cmdb/types'

const props = defineProps<{ cluster: { name: string }; token: string }>()
const capacity = ref<KubernetesCapacity>()
const loading = ref(false)
const error = ref('')

const cards = computed(() => {
  const data = capacity.value
  if (!data) return []
  return [
    { label: 'Pod 使用', available: data.pods.available, used: data.pods.used, capacity: data.pods.capacity, unit: '', percent: data.pods.usagePercent },
    { label: 'CPU 预留', available: data.cpu.available, used: data.cpu.requested, capacity: data.cpu.capacity, unit: ' ' + data.cpu.unit, percent: data.cpu.usagePercent },
    { label: '内存预留', available: data.memory.available, used: data.memory.requested, capacity: data.memory.capacity, unit: ' ' + data.memory.unit, percent: data.memory.usagePercent },
  ]
})

async function load() {
  if (!props.cluster?.name || !props.token) return
  loading.value = true
  error.value = ''
  capacity.value = undefined
  try {
    capacity.value = await fetchKubernetesCapacity(props.token, props.cluster.name)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '容量数据加载失败'
  } finally {
    loading.value = false
  }
}

watch(() => [props.cluster?.name, props.token], load, { immediate: true })
</script>

<style scoped>
.k8s-capacity-wrap { margin-top: 16px; }
.capacity-loading, .capacity-error { padding: 12px; border: 1px solid #18384a; border-radius: 10px; background: #091b29; color: #7f9bb0; font-size: 12px; }
.capacity-error { color: #fb7185; }
.k8s-capacity { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 10px; }
.k8s-capacity article { display: grid; gap: 8px; padding: 12px; border: 1px solid #18384a; border-radius: 10px; background: #091b29; }
.k8s-capacity span { color: #7f9bb0; font-size: 11px; }
.k8s-capacity strong { color: #dff7ff; font-size: 18px; }
.k8s-capacity small { color: #22d3ee; font-size: 11px; }
.k8s-capacity article[data-available=false] strong { color: #f59e0b; font-size: 13px; }
.capacity-bar { height: 6px; overflow: hidden; border-radius: 999px; background: #102d3f; }
.capacity-bar i { display: block; height: 100%; background: linear-gradient(90deg, #16b8e4, #2dd4bf); }
</style>
