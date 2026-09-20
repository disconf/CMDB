export type CICDIntegration = {
  id: string
  name: string
  type: string
  endpoint: string
  enabled: boolean
  status: 'not_configured' | 'unknown' | 'connected' | 'error'
  message: string
  lastCheckedAt?: string
  details?: Record<string, string>
}

export type CICDIntegrationSummary = {
  total: number
  connected: number
  pending: number
  errors: number
  notConfigured: number
}

export const integrationStatusLabel = (status: CICDIntegration['status']) => ({
  connected: '已连通',
  unknown: '待检测',
  error: '连接异常',
  not_configured: '未配置',
}[status] || status)

export const integrationTypeLabel = (type: string) => ({
  'container-registry': '镜像仓库',
  gitops: 'GitOps 持续部署',
  'source-control': '代码仓库',
}[type] || type)

export const integrationDetailLabel = (key: string) => ({
  authMode: '认证方式',
  version: '服务版本',
  applications: '应用数量',
  registryUrl: 'Registry 地址',
  revision: '版本修订',
  requiredEnv: '配置项',
}[key] || key)

export const summarizeIntegrations = (items: CICDIntegration[]): CICDIntegrationSummary => ({
  total: items.length,
  connected: items.filter(item => item.status === 'connected').length,
  pending: items.filter(item => item.status === 'unknown').length,
  errors: items.filter(item => item.status === 'error').length,
  notConfigured: items.filter(item => item.status === 'not_configured').length,
})

export type CICDResources = {
  generatedAt: string
  harbor: {
    available: boolean
    status: CICDIntegration['status']
    message: string
    projects: number
    repositories: number
    recentRepositories: Array<{ project:string; name:string; artifactCount:number; pullCount:number; updatedAt?:string }>
  }
  argoCD: {
    available: boolean
    status: CICDIntegration['status']
    message: string
    applications: number
    healthy: number
    degraded: number
    synced: number
    outOfSync: number
    items: Array<{ name:string; namespace:string; project:string; destination:string; syncStatus:string; healthStatus:string; revision?:string; lastOperation?:string; updatedAt?:string }>
  }
  gitlab: {
    available: boolean
    status: CICDIntegration['status']
    message: string
    projects: number
    recentProjects: Array<{ id:number; name:string; path:string; visibility?:string; defaultBranch?:string; lastActivityAt?:string; webUrl?:string; latestPipeline?:{ status:string; ref?:string; sha?:string; updatedAt?:string; webUrl?:string } }>
  }
}

export const argoSyncLabel = (status:string) => ({ Synced:'已同步', OutOfSync:'待同步', Unknown:'未知' }[status] || status)
export const argoHealthLabel = (status:string) => ({ Healthy:'健康', Degraded:'异常', Progressing:'进行中', Missing:'缺失', Unknown:'未知' }[status] || status)
export const pipelineStatusLabel = (status:string) => ({ success:'成功', failed:'失败', running:'执行中', pending:'等待中', canceled:'已取消', skipped:'已跳过', manual:'手动', created:'已创建' }[status] || status)
