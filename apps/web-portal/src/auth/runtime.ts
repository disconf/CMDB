declare global {
  interface Window {
    __CMDB_CONFIG__?: {
      keycloakUrl?: string
      keycloakRealm?: string
      keycloakClientId?: string
    }
  }
}

function trimBase(value?: string) {
  return (value || '').trim().replace(/\/+$/, '')
}

function inferKeycloakUrl() {
  if (typeof window === 'undefined') return ''
  const { protocol, hostname, port } = window.location
  const prefix = 'cmdb.'
  if (!hostname.startsWith(prefix)) return ''
  const suffix = hostname.slice(prefix.length)
  return `${protocol}//keycloak.${suffix}${port ? `:${port}` : ''}`
}

export function runtimeConfig() {
  const injected = typeof window === 'undefined' ? undefined : window.__CMDB_CONFIG__
  return {
    keycloakUrl: trimBase(injected?.keycloakUrl || import.meta.env.VITE_KEYCLOAK_URL || inferKeycloakUrl()),
    realm: (injected?.keycloakRealm || import.meta.env.VITE_KEYCLOAK_REALM || 'cmdb').trim(),
    clientId: (injected?.keycloakClientId || import.meta.env.VITE_KEYCLOAK_CLIENT_ID || 'cmdb-web').trim(),
  }
}
