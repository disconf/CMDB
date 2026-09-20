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
