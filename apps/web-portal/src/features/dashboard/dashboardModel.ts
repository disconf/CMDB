import type { DashboardOverview, MetricCard } from './types'

const number = new Intl.NumberFormat('zh-CN')

export function buildMetricCards(overview: DashboardOverview): MetricCard[] {
  return [
    {
      label: '资产总量',
      value: number.format(overview.assets.total),
      tone: 'blue',
      icon: 'Database',
      note: `在线 ${number.format(overview.assets.online)} · ${overview.assets.projectGroups} 个项目组`,
    },
    {
      label: 'Agent 在线',
      value: number.format(overview.agents.online),
      tone: 'cyan',
      icon: 'RadioTower',
      note: `舰队覆盖 ${formatPercent(overview.agents.coverage)}`,
    },
    {
      label: '活跃告警',
      value: number.format(overview.metrics.activeAlerts),
      tone: 'red',
      icon: 'BellRing',
      note: `严重 ${number.format(overview.monitoring.critical)} · 已确认 ${number.format(overview.monitoring.acknowledged)}`,
    },
    {
      label: '监控目标',
      value: number.format(overview.monitoring.targets),
      tone: 'green',
      icon: 'Activity',
      note: `健康 ${number.format(overview.monitoring.healthy)} · 覆盖 ${formatPercent(overview.monitoring.coverage)}`,
    },
    {
      label: '今日任务',
      value: number.format(overview.tasks.today),
      tone: 'violet',
      icon: 'ClipboardCheck',
      note: `成功 ${overview.tasks.success} · 运行中 ${overview.tasks.running}`,
    },
    {
      label: '发布成功率',
      value: formatPercent(overview.releases.successRate),
      tone: 'amber',
      icon: 'Rocket',
      note: `今日 ${overview.releases.today} 次 · 失败 ${overview.releases.failed}`,
    },
  ]
}

export function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return '0.0%'
  return `${value.toFixed(1)}%`
}

export function formatCount(value: number): string {
  return number.format(value)
}

export function formatCompactTime(value: string): string {
  if (!value) return '--:--'
  const timestamp = new Date(value)
  if (value.includes('T') && !Number.isNaN(timestamp.getTime())) {
    return new Intl.DateTimeFormat('zh-CN', { hour:'2-digit', minute:'2-digit', hour12:false }).format(timestamp)
  }
  return value.match(/(\d{2}:\d{2})(?::\d{2})?$/)?.[1] || value
}

export function statusTone(value: string): string {
  switch (value) {
    case 'operational':
    case 'success':
    case 'healthy':
    case 'online':
      return 'healthy'
    case 'attention':
    case 'warning':
    case 'running':
    case 'pending':
    case 'awaiting_approval':
    case 'approved':
      return 'warning'
    case 'failed':
    case 'critical':
    case 'offline':
      return 'critical'
    default:
      return 'neutral'
  }
}

export function formatUpdateTime(value: string): string {
  const date = new Date(value)
  return new Intl.DateTimeFormat('zh-CN', { year:'numeric', month:'2-digit', day:'2-digit', hour:'2-digit', minute:'2-digit', second:'2-digit', hour12:false })
    .format(date).replaceAll('/', '-').replace(' ', ' ')
}
