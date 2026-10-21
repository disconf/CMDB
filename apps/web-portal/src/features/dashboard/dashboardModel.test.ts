import { describe, expect, it } from 'vitest'
import { buildMetricCards, formatCompactTime, formatPercent, formatUpdateTime, statusTone } from './dashboardModel'
import type { DashboardOverview } from './types'

const overview = {
  updatedAt: '2026-07-15T10:30:45+08:00',
  metrics: { assetTotal: 4286, onlineAgents: 1024, activeAlerts: 37, todayJobs: 186 },
  command: { platformStatus:'attention', assetHealth:96.4, agentCoverage:88.2, monitorCoverage:92.1, taskSuccessRate:98.3, slaCompliance:99.1, releaseSuccessRate:97.2, activeIncidents:2, criticalAlerts:2 },
  assets: { total:4286, online:4132, warning:112, offline:35, unknown:7, projectGroups:12, environments:4, sources:6 },
  agents: { total:1161, online:1024, offline:137, pending:0, coverage:88.2 },
  monitoring: { targets:286, healthy:268, coverage:92.1, availability:93.7, critical:2, acknowledged:5 },
  tasks: { today:186, running:8, success:179, failed:3, pendingApproval:2, schedules:34, successRate:98.3 },
  tickets: { total:91, pending:6, processing:14, approvedToday:12, slaWarning:1, slaCompliance:98.9 },
  releases: { today:8, running:1, success:7, failed:1, successRate:87.5, recent:[] },
  sources: [],
  capacity: [],
  health: [],
  topology: { layers: [] },
  alertTrend: [],
  alerts: [],
  jobs: [],
  timeline: [],
  deployments: [],
} satisfies DashboardOverview

describe('dashboardModel', () => {
  it('maps API metrics into ordered display cards', () => {
    expect(buildMetricCards(overview)).toEqual([
      expect.objectContaining({ label: '资产总量', value: '4,286', tone: 'blue' }),
      expect.objectContaining({ label: 'Agent 在线', value: '1,024', tone: 'cyan' }),
      expect.objectContaining({ label: '活跃告警', value: '37', tone: 'red' }),
      expect.objectContaining({ label: '监控目标', value: '286', tone: 'green' }),
      expect.objectContaining({ label: '今日任务', value: '186', tone: 'violet' }),
      expect.objectContaining({ label: '发布成功率', value: '87.5%', tone: 'amber' }),
    ])
  })

  it('formats update time for the global header', () => {
    expect(formatUpdateTime(overview.updatedAt)).toBe('2026-07-15 10:30:45')
  })

  it('formats real ratios and status tones', () => {
    expect(formatPercent(98.26)).toBe('98.3%')
    expect(formatCompactTime('2026-10-21T04:30:00Z')).toMatch(/^\d{2}:\d{2}$/)
    expect(formatCompactTime('2026-10-21 04:30:00')).toBe('04:30')
    expect(statusTone('operational')).toBe('healthy')
    expect(statusTone('attention')).toBe('warning')
    expect(statusTone('failed')).toBe('critical')
  })
})
