import Keycloak from 'keycloak-js'
import { runtimeConfig } from './runtime'

const config = runtimeConfig()
if (!config.keycloakUrl) {
  console.error('Keycloak runtime configuration is missing')
}

export const keycloak = new Keycloak({
  url: config.keycloakUrl || window.location.origin,
  realm: config.realm,
  clientId: config.clientId,
})

export const initializeKeycloak = () =>
  keycloak.init({ onLoad: 'check-sso', pkceMethod: 'S256', checkLoginIframe: false })

export const refreshAccessToken = async () => {
  if (keycloak.authenticated) await keycloak.updateToken(30)
  return keycloak.token || ''
}
