#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ROOT_DIR}/.env"
if [[ ! -f "${ENV_FILE}" ]]; then
	echo "缺少 ${ENV_FILE}，请先执行 bootstrap.sh" >&2
	exit 1
fi
set -a
source "${ENV_FILE}"
set +a

KC_URL="${KEYCLOAK_PUBLIC_URL%/}"
ADMIN_USER="${KEYCLOAK_BOOTSTRAP_ADMIN_USERNAME}"
ADMIN_PASSWORD="${KEYCLOAK_BOOTSTRAP_ADMIN_PASSWORD}"
CMDB_USER="${CMDB_ADMIN_USERNAME}"
CMDB_PASSWORD="${CMDB_ADMIN_PASSWORD}"

for command in curl jq; do
	command -v "${command}" >/dev/null 2>&1 || {
		echo "缺少命令 ${command}" >&2
		exit 1
	}
done

for _ in $(seq 1 60); do
	if curl -fsS "${KC_URL}/realms/master" >/dev/null 2>&1; then
		break
	fi
	sleep 2
done

TOKEN="$(curl -fsS -X POST \
	"${KC_URL}/realms/master/protocol/openid-connect/token" \
	-H "Content-Type: application/x-www-form-urlencoded" \
	--data-urlencode "grant_type=password" \
	--data-urlencode "client_id=admin-cli" \
	--data-urlencode "username=${ADMIN_USER}" \
	--data-urlencode "password=${ADMIN_PASSWORD}" | jq -r '.access_token')"

if [[ -z "${TOKEN}" || "${TOKEN}" == "null" ]]; then
	echo "Keycloak 管理员认证失败" >&2
	exit 1
fi

auth=(-H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json")

if [[ "$(curl -s -o /dev/null -w '%{http_code}' "${auth[@]}" "${KC_URL}/admin/realms/cmdb")" != "200" ]]; then
	curl -fsS -X POST "${auth[@]}" "${KC_URL}/admin/realms" -d '{
    "realm": "cmdb",
    "enabled": true,
    "displayName": "CMDB 智能运维平台",
    "loginWithEmailAllowed": true,
    "duplicateEmailsAllowed": false,
    "registrationAllowed": false
  }' >/dev/null
fi

PERMISSIONS=(
	"dashboard:view"
	"cmdb:view" "cmdb:manage"
	"discovery:view" "discovery:manage"
	"topology:view"
	"monitor:view" "monitor:manage"
	"job:view" "job:manage"
	"ticket:view" "ticket:manage"
	"release:view" "release:manage"
	"toolbox:use"
	"aiops:view" "aiops:execute"
	"system:manage"
	"knowledge:view" "knowledge:create" "knowledge:edit" "knowledge:publish"
	"knowledge:delete" "knowledge:manage_acl" "knowledge:manage_folder" "knowledge:manage"
)

for role in "${PERMISSIONS[@]}"; do
	encoded="$(jq -rn --arg value "${role}" '$value|@uri')"
	if [[ "$(curl -s -o /dev/null -w '%{http_code}' "${auth[@]}" "${KC_URL}/admin/realms/cmdb/roles/${encoded}")" != "200" ]]; then
		curl -fsS -X POST "${auth[@]}" "${KC_URL}/admin/realms/cmdb/roles" \
			-d "$(jq -cn --arg name "${role}" '{name:$name,description:$name}')" >/dev/null
	fi
done

if [[ "$(curl -s -o /dev/null -w '%{http_code}' "${auth[@]}" "${KC_URL}/admin/realms/cmdb/roles/platform-admin")" != "200" ]]; then
	curl -fsS -X POST "${auth[@]}" "${KC_URL}/admin/realms/cmdb/roles" \
		-d '{"name":"platform-admin","description":"平台管理员"}' >/dev/null
fi

COMPOSITES="$(for role in "${PERMISSIONS[@]}"; do
	jq -cn --arg name "${role}" '{name:$name}'
done | jq -s '.')"
curl -fsS -X POST "${auth[@]}" \
	"${KC_URL}/admin/realms/cmdb/roles/platform-admin/composites" \
	-d "${COMPOSITES}" >/dev/null || true

CLIENT_ID="$(curl -fsS "${auth[@]}" "${KC_URL}/admin/realms/cmdb/clients?clientId=cmdb-web" | jq -r '.[0].id // empty')"
if [[ -z "${CLIENT_ID}" ]]; then
	curl -fsS -X POST "${auth[@]}" "${KC_URL}/admin/realms/cmdb/clients" -d "$(jq -cn \
		--arg root "${CMDB_PUBLIC_URL:-http://${SERVER_HOST}:${WEB_PORT:-8088}}" \
		'{
      clientId:"cmdb-web",
      name:"CMDB Web",
      enabled:true,
      publicClient:true,
      standardFlowEnabled:true,
      directAccessGrantsEnabled:true,
      redirectUris:[$root,$root+"/",$root+"/silent-check-sso.html"],
      webOrigins:[$root],
      rootUrl:$root,
      attributes:{"pkce.code.challenge.method":"S256","post.logout.redirect.uris":$root}
    }')" >/dev/null
	CLIENT_ID="$(curl -fsS "${auth[@]}" "${KC_URL}/admin/realms/cmdb/clients?clientId=cmdb-web" | jq -r '.[0].id')"
fi

MAPPERS="$(curl -fsS "${auth[@]}" "${KC_URL}/admin/realms/cmdb/clients/${CLIENT_ID}/protocol-mappers/models")"
if ! grep -q '"project_groups"' <<<"${MAPPERS}"; then
	curl -fsS -X POST "${auth[@]}" \
		"${KC_URL}/admin/realms/cmdb/clients/${CLIENT_ID}/protocol-mappers/models" \
		-d '{
      "name":"project-groups",
      "protocol":"openid-connect",
      "protocolMapper":"oidc-group-membership-mapper",
      "config":{
        "claim.name":"project_groups",
        "full.path":"false",
        "id.token.claim":"true",
        "access.token.claim":"true",
        "userinfo.token.claim":"true"
      }
    }' >/dev/null
fi

GROUP_ID="$(curl -fsS "${auth[@]}" "${KC_URL}/admin/realms/cmdb/groups?search=%E5%8D%8E%E4%B8%9C%E4%B8%9A%E5%8A%A1%E7%BB%84" | jq -r '.[0].id // empty')"
if [[ -z "${GROUP_ID}" ]]; then
	curl -fsS -X POST "${auth[@]}" "${KC_URL}/admin/realms/cmdb/groups" \
		-d '{"name":"华东业务组"}' >/dev/null
	GROUP_ID="$(curl -fsS "${auth[@]}" "${KC_URL}/admin/realms/cmdb/groups?search=%E5%8D%8E%E4%B8%9C%E4%B8%9A%E5%8A%A1%E7%BB%84" | jq -r '.[0].id')"
fi

USER_ID="$(curl -fsS "${auth[@]}" "${KC_URL}/admin/realms/cmdb/users?username=${CMDB_USER}&exact=true" | jq -r '.[0].id // empty')"
if [[ -z "${USER_ID}" ]]; then
	curl -fsS -X POST "${auth[@]}" "${KC_URL}/admin/realms/cmdb/users" -d "$(jq -cn \
		--arg username "${CMDB_USER}" \
		--arg password "${CMDB_PASSWORD}" \
		'{
      username:$username,
      enabled:true,
      emailVerified:true,
      firstName:"平台",
      lastName:"管理员",
      credentials:[{type:"password",value:$password,temporary:false}]
    }')" >/dev/null
	USER_ID="$(curl -fsS "${auth[@]}" "${KC_URL}/admin/realms/cmdb/users?username=${CMDB_USER}&exact=true" | jq -r '.[0].id')"
fi

ROLE="$(curl -fsS "${auth[@]}" "${KC_URL}/admin/realms/cmdb/roles/platform-admin")"
curl -fsS -X POST "${auth[@]}" \
	"${KC_URL}/admin/realms/cmdb/users/${USER_ID}/role-mappings/realm" \
	-d "[$(jq -c '{id,name}' <<<"${ROLE}")]" >/dev/null || true
curl -fsS -X PUT "${auth[@]}" \
	"${KC_URL}/admin/realms/cmdb/users/${USER_ID}/groups/${GROUP_ID}" >/dev/null || true

echo "Keycloak realm=cmdb, client=cmdb-web, user=${CMDB_USER} 初始化完成"
