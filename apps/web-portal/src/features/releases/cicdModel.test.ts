import { describe, expect, it } from 'vitest'
import { integrationDetailLabel, integrationStatusLabel, integrationTypeLabel, summarizeIntegrations, type CICDIntegration } from './cicdModel'

const integration = (overrides: Partial<CICDIntegration>): CICDIntegration => ({
  id: 'harbor', name: 'Harbor', type: 'container-registry', endpoint: 'https://harbor.example.com', enabled: true, status: 'unknown', message: '',
  ...overrides,
})

describe('CI/CD integration model', () => {
  it('summarizes connection states', () => {
    expect(summarizeIntegrations([
      integration({ status: 'connected' }),
      integration({ status: 'unknown' }),
      integration({ status: 'error' }),
      integration({ status: 'not_configured' }),
    ])).toEqual({ total: 4, connected: 1, pending: 1, errors: 1, notConfigured: 1 })
  })

  it('provides readable labels', () => {
    expect(integrationStatusLabel('connected')).toBe('已连通')
    expect(integrationTypeLabel('gitops')).toBe('GitOps 持续部署')
    expect(integrationDetailLabel('authMode')).toBe('认证方式')
  })
})
