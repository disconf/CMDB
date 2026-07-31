export type MonitorSummary = { targets: number; healthy: number; active: number; critical: number; acknowledged: number; availability: number }
export type MonitorMetric = { id: string; name: string; target: string; value: number; unit: string; warning: number; critical: number; trend: number[] }
export type MonitorAlert = { id: string; title: string; severity: string; status: string; targetId: string; target: string; rule: string; startedAt: string; duration: string; owner: string; message: string }
export type HostMonitoring = { assetId:string;monitored:boolean;up:boolean;metrics:MonitorMetric[];alerts:MonitorAlert[] }
export type CoverageHost = { assetId:string;name:string;ip:string;environment:string;projectGroup:string;assetStatus:string;monitorStatus:'healthy'|'down'|'missing' }
export type MonitorCoverage = { available:boolean;total:number;monitored:number;healthy:number;down:number;missing:number;percentage:number;hosts:CoverageHost[] }
export type AlertEvent = { id: number; alertId: string; type: string; operator: string; message: string; createdAt: string }
export type AlertRule = { group: string; name: string; query: string; duration: number; health: string; lastError: string; state: string; severity: string; firingCount: number }
export type AlertSilence = { id: string; matchers: { name: string; value: string; isRegex: boolean; isEqual: boolean }[]; startsAt: string; endsAt: string; createdBy: string; comment: string; status: { state: string } }
export type RoutingRule = { id: string; name: string; matcherName: string; matcherValue: string; team: string; owner: string; channelId: string; enabled: boolean }
export type NotificationChannel = { id: string; name: string; displayUrl: string; template: string; enabled: boolean; lastStatus: string; lastError: string; lastSentAt: string }
export type NotificationDelivery = { id: string; channelId: string; channelName: string; alertId: string; alertTitle: string; attempt: number; status: string; httpStatus: number; error: string; createdAt: string }
export type DeliveryStats = { total: number; successful: number; failed: number; successRate: number }
export type NotificationQueueStatus = { enabled: boolean; consumer: string; pendingOutbox: number; deadLetters: number; lastConsumedAt: string }
export type NotificationDeadLetter = { id: string; eventId: string; alertId: string; alertTitle: string; channelId: string; error: string; attempts: number; createdAt: string }
export type EscalationPolicy = { id:string;name:string;severity:string;timeoutMinutes:number;team:string;owner:string;channelId:string;enabled:boolean }

async function request<T>(path: string, token: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { ...init, headers: { Authorization: `Bearer ${token}`, ...init?.headers } })
  if (!response.ok) throw new Error(response.status === 401 ? '登录状态已失效' : `监控服务请求失败（${response.status}）`)
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

export const fetchMonitorData = async (token: string) => {
  const [summary, metrics, alerts, coverage] = await Promise.all([
    request<MonitorSummary>('/api/v1/monitor/summary', token),
    request<MonitorMetric[]>('/api/v1/monitor/metrics', token),
    request<MonitorAlert[]>('/api/v1/monitor/alerts', token),
    request<MonitorCoverage>('/api/v1/monitor/coverage', token),
  ])
  return { summary, metrics, alerts, coverage }
}
export const fetchHostMonitoring = (token:string,assetId:string) => request<HostMonitoring>(`/api/v1/monitor/hosts/${encodeURIComponent(assetId)}`,token)
export const installHostExporter = (token:string,assetId:string) => request<{id:string;status:string}>(`/api/v1/monitor/hosts/${encodeURIComponent(assetId)}/install-exporter`,token,{method:'POST'})

export const updateAlert = (token: string, id: string, action: 'acknowledge' | 'resolve') =>
  request<MonitorAlert>(`/api/v1/monitor/alerts/${encodeURIComponent(id)}/${action}`, token, { method: 'POST' })

export const fetchAlertEvents = (token: string, id: string) =>
  request<AlertEvent[]>(`/api/v1/monitor/alerts/${encodeURIComponent(id)}/events`, token)

export const assignAlert = (token: string, id: string, owner: string) =>
  request<MonitorAlert>(`/api/v1/monitor/alerts/${encodeURIComponent(id)}/assign`, token, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ owner }) })

export const silenceAlert = (token: string, id: string) =>
  request<MonitorAlert>(`/api/v1/monitor/alerts/${encodeURIComponent(id)}/silence`, token, { method: 'POST' })

export const bulkUpdateAlerts = (token: string, ids: string[], action: 'acknowledge' | 'resolve') =>
  request<{ succeeded: number; failed: string[] }>('/api/v1/monitor/alerts/bulk', token, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ids, action }) })

export const fetchAlertRules = (token: string) => request<AlertRule[]>('/api/v1/monitor/rules', token)
export const fetchSilences = (token: string) => request<AlertSilence[]>('/api/v1/monitor/silences', token)
export const createSilence = (token: string, matcherName: string, matcherValue: string, hours: number, comment: string) => request<{ id: string }>('/api/v1/monitor/silences', token, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ matchers: [{ name: matcherName, value: matcherValue, isRegex: false, isEqual: true }], startsAt: new Date().toISOString(), endsAt: new Date(Date.now() + hours * 3600_000).toISOString(), comment }) })
export const expireSilence = (token: string, id: string) => request<void>(`/api/v1/monitor/silences/${encodeURIComponent(id)}`, token, { method: 'DELETE' })
export const fetchRoutingRules = (token: string) => request<RoutingRule[]>('/api/v1/monitor/routes', token)
export const createRoutingRule = (token: string, input: Omit<RoutingRule, 'id' | 'enabled'>) => request<RoutingRule>('/api/v1/monitor/routes', token, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input) })
export const toggleRoutingRule = (token: string, id: string) => request<RoutingRule>(`/api/v1/monitor/routes/${encodeURIComponent(id)}/toggle`, token, { method: 'POST' })
export const deleteRoutingRule = (token: string, id: string) => request<void>(`/api/v1/monitor/routes/${encodeURIComponent(id)}`, token, { method: 'DELETE' })
export const fetchNotificationChannels = (token: string) => request<NotificationChannel[]>('/api/v1/monitor/channels', token)
export const createNotificationChannel = (token: string, name: string, endpoint: string, template: string) => request<NotificationChannel>('/api/v1/monitor/channels', token, { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({name,endpoint,template}) })
export const toggleNotificationChannel = (token: string, id: string) => request<NotificationChannel>(`/api/v1/monitor/channels/${encodeURIComponent(id)}/toggle`, token, {method:'POST'})
export const testNotificationChannel = (token: string, id: string) => request<void>(`/api/v1/monitor/channels/${encodeURIComponent(id)}/test`, token, {method:'POST'})
export const deleteNotificationChannel = (token: string, id: string) => request<void>(`/api/v1/monitor/channels/${encodeURIComponent(id)}`, token, {method:'DELETE'})
export const updateNotificationTemplate = (token:string,id:string,template:string) => request<NotificationChannel>(`/api/v1/monitor/channels/${encodeURIComponent(id)}/template`,token,{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify({template})})
export const fetchNotificationDeliveries = (token: string) => request<NotificationDelivery[]>('/api/v1/monitor/deliveries',token)
export const fetchDeliveryStats = (token: string) => request<DeliveryStats>('/api/v1/monitor/deliveries/stats',token)
export const fetchNotificationQueueStatus = (token:string) => request<NotificationQueueStatus>('/api/v1/monitor/queue/status',token)
export const fetchNotificationDeadLetters = (token:string) => request<NotificationDeadLetter[]>('/api/v1/monitor/queue/dead-letters',token)
export const replayDeadLetter = (token:string,id:string) => request<void>(`/api/v1/monitor/queue/dead-letters/${encodeURIComponent(id)}/replay`,token,{method:'POST'})
export const deleteDeadLetter = (token:string,id:string) => request<void>(`/api/v1/monitor/queue/dead-letters/${encodeURIComponent(id)}`,token,{method:'DELETE'})
export const bulkDeadLetters = (token:string,ids:string[],action:'replay'|'delete') => request<{succeeded:number;failed:string[]}>('/api/v1/monitor/queue/dead-letters/bulk',token,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({ids,action})})
export const fetchEscalationPolicies=(token:string)=>request<EscalationPolicy[]>('/api/v1/monitor/escalations',token)
export const createEscalationPolicy=(token:string,input:Omit<EscalationPolicy,'id'|'enabled'>)=>request<EscalationPolicy>('/api/v1/monitor/escalations',token,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(input)})
export const toggleEscalationPolicy=(token:string,id:string)=>request<EscalationPolicy>(`/api/v1/monitor/escalations/${encodeURIComponent(id)}/toggle`,token,{method:'POST'})
export const deleteEscalationPolicy=(token:string,id:string)=>request<void>(`/api/v1/monitor/escalations/${encodeURIComponent(id)}`,token,{method:'DELETE'})
