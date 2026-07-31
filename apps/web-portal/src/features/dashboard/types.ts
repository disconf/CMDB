export interface DashboardOverview {
  updatedAt: string
  metrics: { assetTotal: number; onlineAgents: number; activeAlerts: number; todayJobs: number }
  capacity: Array<{ name: string; value: number; detail: string }>
  health: Array<{ name: string; value: number }>
  topology: { layers: Array<{ name: string; nodes: Array<{ id: string; name: string; kind: string; status: string }> }> }
  alertTrend: Array<{ time: string; total: number; critical: number }>
  alerts: Array<{ id: string; level: string; title: string; target: string; occurredAt: string }>
  jobs: Array<{ id: string; name: string; type: string; progress: number; status: string; owner: string }>
  timeline: Array<{ id: string; time: string; type: string; message: string; source: string }>
  deployments: Array<{ name: string; version: string; status: string; updatedAt: string }>
}

export interface MetricCard { label: string; value: string; tone: 'blue'|'cyan'|'red'|'violet'; icon: string; note: string }
