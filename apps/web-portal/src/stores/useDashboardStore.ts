import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { fetchDashboardOverview } from '@/api/dashboard'
import { buildMetricCards } from '@/features/dashboard/dashboardModel'
import type { DashboardOverview } from '@/features/dashboard/types'

export const useDashboardStore = defineStore('dashboard', () => {
  const overview = ref<DashboardOverview | null>(null)
  const isLoading = ref(false)
  const error = ref('')
  const metricCards = computed(() => overview.value ? buildMetricCards(overview.value) : [])
  async function load(signal?: AbortSignal) {
    isLoading.value = true; error.value = ''
    try { overview.value = await fetchDashboardOverview(signal) }
    catch (reason) { if ((reason as Error).name !== 'AbortError') error.value = reason instanceof Error ? reason.message : '大屏数据加载失败' }
    finally { isLoading.value = false }
  }
  return { overview, isLoading, error, metricCards, load }
})
