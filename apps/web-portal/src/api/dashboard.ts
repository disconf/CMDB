import type { DashboardOverview } from '@/features/dashboard/types'

export async function fetchDashboardOverview(signal?: AbortSignal): Promise<DashboardOverview> {
  const response = await fetch('/api/v1/dashboard/overview', { signal })
  if (!response.ok) throw new Error(`大屏数据请求失败：${response.status}`)
  return response.json() as Promise<DashboardOverview>
}
