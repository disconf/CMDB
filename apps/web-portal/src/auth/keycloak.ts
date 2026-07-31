import Keycloak from 'keycloak-js'

export const keycloak = new Keycloak({
  url: import.meta.env.VITE_KEYCLOAK_URL || 'http://192.168.85.134:8081',
  realm: import.meta.env.VITE_KEYCLOAK_REALM || 'cmdb',
  clientId: import.meta.env.VITE_KEYCLOAK_CLIENT_ID || 'cmdb-web',
})

export const initializeKeycloak = () =>
  keycloak.init({ onLoad: 'check-sso', pkceMethod: 'S256', checkLoginIframe: false })

export const refreshAccessToken = async () => {
  if (keycloak.authenticated) await keycloak.updateToken(30)
  return keycloak.token || ''
}
