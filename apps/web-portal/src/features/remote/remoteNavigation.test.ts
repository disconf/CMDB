import { describe, expect, it } from 'vitest'
import { remoteSections, resolveRemoteSection } from './remoteNavigation'

describe('remote navigation', () => {
  it('defines the remote submenu in user workflow order', () => {
    expect(remoteSections.map(item => item.id)).toEqual(['overview', 'sessions', 'files', 'execution', 'approvals', 'security'])
  })

  it('resolves route paths to remote sections', () => {
    expect(resolveRemoteSection('/remote')).toBe('overview')
    expect(resolveRemoteSection('/remote/sessions')).toBe('sessions')
    expect(resolveRemoteSection('/remote/unknown')).toBe('overview')
  })
})