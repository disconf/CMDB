export const remoteSections = [
  { id: 'overview', name: '远程运维概览', shortName: '概览', path: '/remote/overview', description: '汇总会话、执行、审批与安全状态' },
  { id: 'sessions', name: '终端会话', shortName: '终端会话', path: '/remote/sessions', description: '建立 WebSSH、协作接入并管理在线会话' },
  { id: 'files', name: '文件传输', shortName: '文件传输', path: '/remote/files', description: '在安全策略约束下上传和下载文件' },
  { id: 'execution', name: '批量远程执行', shortName: '批量执行', path: '/remote/execution', description: '按项目组、标签或 IP 批量执行受控命令' },
  { id: 'approvals', name: '审批与审计', shortName: '审批审计', path: '/remote/approvals', description: '处理高风险命令审批并追溯会话操作' },
  { id: 'security', name: '安全设置', shortName: '安全设置', path: '/remote/security', description: '维护账户授权、主机指纹与终端安全策略' },
] as const

export type RemoteSection = typeof remoteSections[number]['id']

export function resolveRemoteSection(path: string): RemoteSection {
  const match = path.match(/^\/remote\/([^/]+)/)
  const candidate = (match?.[1] || 'overview') as RemoteSection
  return remoteSections.some(item => item.id === candidate) ? candidate : 'overview'
}