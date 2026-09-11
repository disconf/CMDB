import type { DashboardOverview, MetricCard } from './types'

const number = new Intl.NumberFormat('zh-CN')

export function buildMetricCards(overview: DashboardOverview): MetricCard[] {
  return [
    { label: '资产总数', value: number.format(overview.metrics.assetTotal), tone: 'blue', icon: 'Database', note: '' },
    { label: '在线 Agent', value: number.format(overview.metrics.onlineAgents), tone: 'cyan', icon: 'RadioTower', note: '' },
    { label: '活跃告警', value: number.format(overview.metrics.activeAlerts), tone: 'red', icon: 'BellRing', note: '' },
    { label: '今日任务', value: number.format(overview.metrics.todayJobs), tone: 'violet', icon: 'ClipboardCheck', note: '' },
  ]
}

export function formatUpdateTime(value: string): string {
  const date = new Date(value)
  return new Intl.DateTimeFormat('zh-CN', { year:'numeric', month:'2-digit', day:'2-digit', hour:'2-digit', minute:'2-digit', second:'2-digit', hour12:false })
    .format(date).replaceAll('/', '-').replace(' ', ' ')
}
