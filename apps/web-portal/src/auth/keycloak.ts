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

export const initializeKeycloak = async () => {
  try {
    return await keycloak.init({
      onLoad: 'check-sso',
      pkceMethod: 'S256',
      checkLoginIframe: false,
      timeSkew: 30,
    })
  } finally {
    if (window.location.hash) {
      window.history.replaceState(null, '', window.location.pathname + window.location.search)
    }
  }
}

export const refreshAccessToken = async (minValidity = 60) => {
  if (!keycloak.authenticated) return ''
  try {
    await keycloak.updateToken(minValidity)
  } catch (error) {
    if (!keycloak.refreshToken) throw error
    await keycloak.updateToken(-1)
  }
  return keycloak.token || ''
}

export const configureTokenLifecycle = (onToken: (token: string) => void, onExpired: () => void) => {
  keycloak.onTokenExpired = () => {
    void refreshAccessToken(30).then(onToken).catch(onExpired)
  }
}
