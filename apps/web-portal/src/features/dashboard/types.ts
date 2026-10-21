export interface DashboardOverview {
  updatedAt: string
  metrics: { assetTotal: number; onlineAgents: number; activeAlerts: number; todayJobs: number }
  command: {
    platformStatus: string
    assetHealth: number
    agentCoverage: number
    monitorCoverage: number
    taskSuccessRate: number
    slaCompliance: number
    releaseSuccessRate: number
    activeIncidents: number
    criticalAlerts: number
  }
  assets: {
    total: number
    online: number
    warning: number
    offline: number
    unknown: number
    projectGroups: number
    environments: number
    sources: number
  }
  agents: { total: number; online: number; offline: number; pending: number; coverage: number }
  monitoring: {
    targets: number
    healthy: number
    coverage: number
    availability: number
    critical: number
    acknowledged: number
  }
  tasks: {
    today: number
    running: number
    success: number
    failed: number
    pendingApproval: number
    schedules: number
    successRate: number
  }
  tickets: {
    total: number
    pending: number
    processing: number
    approvedToday: number
    slaWarning: number
    slaCompliance: number
  }
  releases: {
    today: number
    running: number
    success: number
    failed: number
    successRate: number
    recent: Array<{ name: string; version: string; status: string; updatedAt: string }>
  }
  sources: Array<{ code: string; name: string; status: string; detail: string; count: number; updatedAt: string }>
  capacity: Array<{ name: string; value: number; detail: string }>
  health: Array<{ name: string; value: number }>
  topology: { layers: Array<{ name: string; nodes: Array<{ id: string; name: string; kind: string; status: string }> }> }
  alertTrend: Array<{ time: string; total: number; critical: number }>
  alerts: Array<{ id: string; level: string; title: string; target: string; occurredAt: string }>
  jobs: Array<{ id: string; name: string; type: string; progress: number; status: string; owner: string }>
  timeline: Array<{ id: string; time: string; type: string; message: string; source: string }>
  deployments: Array<{ name: string; version: string; status: string; updatedAt: string }>
}

export interface MetricCard {
  label: string
  value: string
  tone: 'blue' | 'cyan' | 'red' | 'violet' | 'green' | 'amber'
  icon: string
  note: string
}
