export type TicketStep = { id: string; name: string; status: string; assignee: string; completedAt?: string }
export type TicketEvent = { time: string; actor: string; action: string; comment: string }
export type Ticket = {
  id: string
  title: string
  type: string
  priority: string
  status: string
  applicant: string
  assignee: string
  description: string
  createdAt: string
  updatedAt?: string
  sla: string
  slaDueAt?: string
  risk: string
  relatedAsset: string
  relatedAlert: string
  relatedTask: string
  relatedRelease: string
  projectGroup: string
  automationTemplate: string
  escalationLevel: number
  escalatedAt?: string
  taskAuthorizationExpiresAt?: string
  taskAuthorizationUsedAt?: string
  taskAuthorizationToken?: string
  steps: TicketStep[]
  timeline: TicketEvent[]
}
export type TicketSummary = { total: number; pending: number; processing: number; approvedToday: number; slaWarning: number }
export type TicketInput = {
  title: string
  type: string
  priority: string
  description: string
  relatedAsset: string
  relatedAlert: string
  relatedTask: string
  relatedRelease: string
  projectGroup: string
  automationTemplate: string
}

async function request<T>(path: string, token: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json', ...(init?.headers || {}) },
  })
  const data = await response.json().catch(() => ({ message: '请求失败' }))
  if (!response.ok) throw new Error((data as { message?: string }).message || `请求失败 (${response.status})`)
  return data as T
}

export const fetchTickets = (token: string) => Promise.all([
  request<TicketSummary>('/api/v1/tickets/summary', token),
  request<Ticket[]>('/api/v1/tickets', token),
])

export const fetchTicket = (token: string, id: string) => request<Ticket>(`/api/v1/tickets/${id}`, token)
export const createTicket = (token: string, input: TicketInput) => request<Ticket>('/api/v1/tickets', token, { method: 'POST', body: JSON.stringify(input) })
export const decideTicket = (token: string, id: string, action: 'approve' | 'reject' | 'close' | 'cancel', comment: string) => request<Ticket>(`/api/v1/tickets/${id}/${action}`, token, { method: 'POST', body: JSON.stringify({ comment }) })
export const commentTicket = (token: string, id: string, comment: string) => request<Ticket>(`/api/v1/tickets/${id}/comment`, token, { method: 'POST', body: JSON.stringify({ comment }) })
export const assignTicket = (token: string, id: string, assignee: string) => request<Ticket>(`/api/v1/tickets/${id}/assign`, token, { method: 'POST', body: JSON.stringify({ assignee }) })