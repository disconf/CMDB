import type { DashboardOverview } from '@/features/dashboard/types'

export async function fetchDashboardOverview(token:string, signal?: AbortSignal): Promise<DashboardOverview> {
  const response = await fetch('/api/v1/dashboard/overview', { signal, headers: { Authorization:`Bearer ${token}` } })
  if (response.status === 401) throw new Error('登录状态已失效，请重新登录')
  if (!response.ok) throw new Error(`大屏数据请求失败：${response.status}`)
  return response.json() as Promise<DashboardOverview>
}
