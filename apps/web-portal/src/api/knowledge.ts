export type KnowledgeFormat = 'markdown' | 'html'

export interface KnowledgeSpace {
  id: string
  name: string
  code: string
  description: string
  kind: string
  visibility: string
  owner: string
  createdBy: string
  createdAt: string
  updatedAt: string
  documentCount: number
  folderCount: number
  private: boolean
}

export interface KnowledgeFolder {
  id: string
  spaceId: string
  parentId?: string
  name: string
  path: string
  description: string
  owner: string
  sortOrder: number
  documentCount: number
  children?: KnowledgeFolder[]
  createdAt: string
  updatedAt: string
}

export interface KnowledgeDoc {
  id: string
  spaceId?: string
  folderId?: string
  title: string
  category: string
  summary: string
  content: string
  contentFormat?: KnowledgeFormat
  tags: string[]
  helpful: number
  views: number
  status: string
  source: string
  sourceId?: string
  author: string
  owner?: string
  visibility?: string
  version: number
  publishedVersion?: number
  createdAt: string
  updatedAt: string
  related: Array<{ type: string; id: string; name: string; status?: string }>
}

export interface KnowledgeTree {
  spaces: KnowledgeSpace[]
  folders: KnowledgeFolder[]
  selectedSpaceId: string
}

export interface KnowledgeACL {
  id: string
  resourceType: string
  resourceId: string
  subjectType: string
  subjectId: string
  permission: string
  effect: string
  inherit: boolean
  createdBy: string
  createdAt: string
}

export interface KnowledgeACLInput {
  subjectType: string
  subjectId: string
  permission: string
  effect: string
  inherit: boolean
}

export interface KnowledgePayload {
  spaceId?: string
  folderId?: string
  title: string
  category: string
  summary: string
  content: string
  contentFormat: KnowledgeFormat
  tags: string[]
  status: string
  owner?: string
  visibility?: string
  version?: number
  related?: Array<{ type: string; id: string; name?: string }>
}

async function request<T>(path: string, token: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...(init?.headers || {}),
    },
  })
  if (response.status === 204) return undefined as T
  const data = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error((data as { message?: string }).message || `知识库请求失败 (${response.status})`)
  return data as T
}

export const fetchKnowledgeSpaces = (token: string) => request<KnowledgeSpace[]>('/api/v1/knowledge/spaces', token)

export const fetchKnowledgeTree = (token: string, spaceId = '') =>
  request<KnowledgeTree>(`/api/v1/knowledge/tree?spaceId=${encodeURIComponent(spaceId)}`, token)

export const createKnowledgeSpace = (token: string, payload: Record<string, unknown>) =>
  request<KnowledgeSpace>('/api/v1/knowledge/spaces', token, { method: 'POST', body: JSON.stringify(payload) })

export const updateKnowledgeSpace = (token: string, id: string, payload: Record<string, unknown>) =>
  request<KnowledgeSpace>(`/api/v1/knowledge/spaces/${encodeURIComponent(id)}`, token, { method: 'PUT', body: JSON.stringify(payload) })

export const deleteKnowledgeSpace = (token: string, id: string) =>
  request<void>(`/api/v1/knowledge/spaces/${encodeURIComponent(id)}`, token, { method: 'DELETE' })

export const createKnowledgeFolder = (token: string, payload: Record<string, unknown>) =>
  request<KnowledgeFolder>('/api/v1/knowledge/folders', token, { method: 'POST', body: JSON.stringify(payload) })

export const updateKnowledgeFolder = (token: string, id: string, payload: Record<string, unknown>) =>
  request<KnowledgeFolder>(`/api/v1/knowledge/folders/${encodeURIComponent(id)}`, token, { method: 'PUT', body: JSON.stringify(payload) })

export const moveKnowledgeFolder = (token: string, id: string, parentId: string) =>
  request<KnowledgeFolder>(`/api/v1/knowledge/folders/${encodeURIComponent(id)}/move`, token, {
    method: 'POST',
    body: JSON.stringify({ parentId }),
  })

export const deleteKnowledgeFolder = (token: string, id: string) =>
  request<void>(`/api/v1/knowledge/folders/${encodeURIComponent(id)}`, token, { method: 'DELETE' })

export const fetchKnowledgeDocuments = (
  token: string,
  params: { spaceId?: string; folderId?: string; q?: string; status?: string; includeSubfolders?: boolean },
) => {
  const query = new URLSearchParams()
  if (params.spaceId) query.set('spaceId', params.spaceId)
  if (params.folderId) query.set('folderId', params.folderId)
  if (params.q) query.set('q', params.q)
  if (params.status) query.set('status', params.status)
  if (params.includeSubfolders) query.set('includeSubfolders', 'true')
  return request<KnowledgeDoc[]>(`/api/v1/knowledge/documents?${query.toString()}`, token)
}

export const fetchKnowledgeDocument = (token: string, id: string) =>
  request<KnowledgeDoc>(`/api/v1/knowledge/documents/${encodeURIComponent(id)}`, token)

export const createKnowledgeDocument = (token: string, payload: KnowledgePayload) =>
  request<KnowledgeDoc>('/api/v1/knowledge/documents', token, { method: 'POST', body: JSON.stringify(payload) })

export const updateKnowledgeDocument = (token: string, id: string, payload: KnowledgePayload) =>
  request<KnowledgeDoc>(`/api/v1/knowledge/documents/${encodeURIComponent(id)}`, token, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })

export const deleteKnowledgeDocument = (token: string, id: string) =>
  request<void>(`/api/v1/knowledge/documents/${encodeURIComponent(id)}`, token, { method: 'DELETE' })

export const publishKnowledgeDocument = (token: string, id: string) =>
  request<KnowledgeDoc>(`/api/v1/knowledge/documents/${encodeURIComponent(id)}/publish`, token, { method: 'POST' })

export const archiveKnowledgeDocument = (token: string, id: string) =>
  request<KnowledgeDoc>(`/api/v1/knowledge/documents/${encodeURIComponent(id)}/archive`, token, { method: 'POST' })

export const fetchKnowledgeACL = (token: string, resourceType: 'folder' | 'document', id: string) =>
  request<KnowledgeACL[]>(`/api/v1/knowledge/${resourceType === 'folder' ? 'folders' : 'documents'}/${encodeURIComponent(id)}/permissions`, token)

export const saveKnowledgeACL = (
  token: string,
  resourceType: 'folder' | 'document',
  id: string,
  entries: KnowledgeACLInput[],
) =>
  request<KnowledgeACL[]>(`/api/v1/knowledge/${resourceType === 'folder' ? 'folders' : 'documents'}/${encodeURIComponent(id)}/permissions`, token, {
    method: 'PUT',
    body: JSON.stringify(entries),
  })
