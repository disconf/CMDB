import { describe, expect, it } from 'vitest'
import { buildMetricCards, formatUpdateTime } from './dashboardModel'
import type { DashboardOverview } from './types'

const overview = {
  updatedAt: '2026-07-15T10:30:45+08:00',
  metrics: { assetTotal: 4286, onlineAgents: 1024, activeAlerts: 37, todayJobs: 186 },
} as DashboardOverview

describe('dashboardModel', () => {
  it('maps API metrics into ordered display cards', () => {
    expect(buildMetricCards(overview)).toEqual([
      expect.objectContaining({ label: '资产总数', value: '4,286', tone: 'blue' }),
      expect.objectContaining({ label: '在线 Agent', value: '1,024', tone: 'cyan' }),
      expect.objectContaining({ label: '活跃告警', value: '37', tone: 'red' }),
      expect.objectContaining({ label: '今日任务', value: '186', tone: 'violet' }),
    ])
  })

  it('formats update time for the global header', () => {
    expect(formatUpdateTime(overview.updatedAt)).toBe('2026-07-15 10:30:45')
  })
})
