#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ROOT_DIR}/.env"
COMPOSE=(docker compose --project-directory "${ROOT_DIR}" --env-file "${ENV_FILE}" -f "${ROOT_DIR}/docker-compose.yml")

for command in docker openssl ssh-keygen curl jq; do
	command -v "${command}" >/dev/null 2>&1 || {
		echo "缺少命令 ${command}" >&2
		exit 1
	}
done
docker compose version >/dev/null

if [[ ! -f "${ENV_FILE}" ]]; then
	cp "${ROOT_DIR}/.env.example" "${ENV_FILE}"
	sed -i 's/^CMDB_VERSION=.*/CMDB_VERSION=1.0.228/' "${ENV_FILE}"
fi

set -a
source "${ENV_FILE}"
set +a

if [[ "${SERVER_HOST}" == "192.168.10.10" ]]; then
	echo "请先编辑 ${ENV_FILE}，把 SERVER_HOST、KEYCLOAK_PUBLIC_URL、CMDB_PUBLIC_URL 改为实际地址。" >&2
	exit 1
fi

randomize() {
	local key="$1"
	if grep -q "^${key}=CHANGE_ME" "${ENV_FILE}"; then
		local value
		value="$(openssl rand -hex 24)"
		sed -i "s|^${key}=.*|${key}=${value}|" "${ENV_FILE}"
	fi
}

for key in \
	POSTGRES_ADMIN_PASSWORD PLATFORM_DB_PASSWORD KEYCLOAK_DB_PASSWORD REDIS_PASSWORD \
	KEYCLOAK_BOOTSTRAP_ADMIN_PASSWORD CMDB_ADMIN_PASSWORD MINIO_ROOT_PASSWORD \
	NEO4J_PASSWORD GRAFANA_ADMIN_PASSWORD GATEWAY_INTERNAL_TOKEN RUNNER_API_TOKEN \
	AGENT_SHARED_TOKEN SESSION_SECRET RELEASE_WEBHOOK_TOKEN; do
	randomize "${key}"
done

set -a
source "${ENV_FILE}"
set +a

sed -i "s|^KEYCLOAK_PUBLIC_URL=.*|KEYCLOAK_PUBLIC_URL=http://${SERVER_HOST}:${KEYCLOAK_PORT:-8081}|" "${ENV_FILE}"
sed -i "s|^CMDB_PUBLIC_URL=.*|CMDB_PUBLIC_URL=http://${SERVER_HOST}:${WEB_PORT:-8088}|" "${ENV_FILE}"
set -a
source "${ENV_FILE}"
set +a

mkdir -p \
	"${CMDB_DATA_DIR}/postgres" "${CMDB_DATA_DIR}/redis" "${CMDB_DATA_DIR}/kafka" \
	"${CMDB_DATA_DIR}/vault" "${CMDB_DATA_DIR}/minio" "${CMDB_DATA_DIR}/neo4j/data" \
	"${CMDB_DATA_DIR}/neo4j/logs" "${CMDB_DATA_DIR}/opensearch" \
	"${CMDB_DATA_DIR}/prometheus" "${CMDB_DATA_DIR}/alertmanager" \
	"${CMDB_DATA_DIR}/grafana" "${CMDB_DATA_DIR}/loki" "${CMDB_DATA_DIR}/tempo" \
	"${CMDB_DATA_DIR}/runner" "${ROOT_DIR}/secrets"
chown -R 999:999 "${CMDB_DATA_DIR}/postgres"
chmod 700 "${CMDB_DATA_DIR}/postgres"

if [[ ! -f "${RUNNER_SSH_KEY_FILE}" ]]; then
	ssh-keygen -q -t ed25519 -N "" -C "cmdb-runner" -f "${RUNNER_SSH_KEY_FILE}"
fi
if [[ ! -f "${RUNNER_KNOWN_HOSTS_FILE}" ]]; then
	touch "${RUNNER_KNOWN_HOSTS_FILE}"
	chmod 644 "${RUNNER_KNOWN_HOSTS_FILE}"
fi
printf '%s' "${AGENT_SHARED_TOKEN}" >"${ROOT_DIR}/secrets/agent-token"
chmod 600 "${ROOT_DIR}/secrets/agent-token"

cat >"${ROOT_DIR}/secrets/gateway.env" <<EOF
DATABASE_URL=postgres://${PLATFORM_DB_USER}:${PLATFORM_DB_PASSWORD}@postgres:5432/${PLATFORM_DB_NAME}?sslmode=disable
REDIS_URL=redis://:${REDIS_PASSWORD}@redis:6379/0
KAFKA_BROKERS=kafka:19092
KEYCLOAK_BASE_URL=${KEYCLOAK_PUBLIC_URL}
KEYCLOAK_REALM=cmdb
KEYCLOAK_CLIENT_ID=cmdb-gateway
KEYCLOAK_ISSUER_URL=${KEYCLOAK_PUBLIC_URL}/realms/cmdb
SESSION_SECRET=${SESSION_SECRET}
AGENT_SHARED_TOKEN=${AGENT_SHARED_TOKEN}
AGENT_OFFLINE_AFTER=3m
CMDB_NODE_EXPORTER_PORT=9100
ANSIBLE_RUNNER_URL=http://ansible-runner:8090
ANSIBLE_RUNNER_TOKEN=${RUNNER_API_TOKEN}
PROMETHEUS_URL=http://prometheus:9090
ALERTMANAGER_URL=http://alertmanager:9093
LOKI_URL=http://loki:3100
TEMPO_URL=http://tempo:3200
OTEL_EXPORTER_OTLP_ENDPOINT=http://tempo:4317
NEO4J_URL=http://neo4j:7474
NEO4J_USERNAME=${NEO4J_USER}
NEO4J_PASSWORD=${NEO4J_PASSWORD}
OPENSEARCH_URL=http://opensearch:9200
S3_ENDPOINT_URL=http://minio:9000
S3_ACCESS_KEY_ID=${MINIO_ROOT_USER}
S3_SECRET_ACCESS_KEY=${MINIO_ROOT_PASSWORD}
S3_BUCKET=${S3_BUCKET}
S3_REGION=us-east-1
VAULT_ADDR=http://vault:8200
VAULT_TOKEN=BOOTSTRAP_TOKEN_PLACEHOLDER
TOOLBOX_TIMEOUT=12s
TOOLBOX_ALLOW_LOOPBACK=false
WEBHOOK_ALLOWED_HOSTS=hooks.example.com
RELEASE_WEBHOOK_TOKEN=${RELEASE_WEBHOOK_TOKEN}
CICD_INSECURE_SKIP_VERIFY=true
CICD_HARBOR_URL=
CICD_HARBOR_USERNAME=
CICD_HARBOR_PASSWORD=
CICD_ARGOCD_URL=
CICD_ARGOCD_USERNAME=
CICD_ARGOCD_PASSWORD=
CICD_GITLAB_URL=
CICD_GITLAB_TOKEN=
CICD_HEALTH_WEBHOOK_TOKEN=
RELEASE_CHANGE_WINDOWS=production=Mon-Fri,09:00-18:00;qa=*;staging=*
CICD_ARGOCD_SYNC_INTERVAL=10m
CICD_ARGOCD_AUTO_SYNC=false
AIOPS_ALLOW_CHANGE_RUNBOOKS=false
AI_LLM_BASE_URL=${AI_LLM_BASE_URL:-}
AI_LLM_MODEL=${AI_LLM_MODEL:-}
AI_LLM_API_KEY=${AI_LLM_API_KEY:-}
AI_EMBEDDING_BASE_URL=${AI_EMBEDDING_BASE_URL:-}
AI_EMBEDDING_MODEL=${AI_EMBEDDING_MODEL:-}
AI_EMBEDDING_API_KEY=${AI_EMBEDDING_API_KEY:-}
AI_EMBEDDING_DIMENSIONS=${AI_EMBEDDING_DIMENSIONS:-256}
EOF
chmod 600 "${ROOT_DIR}/secrets/gateway.env"

cat >"${ROOT_DIR}/secrets/runner.env" <<EOF
RUNNER_API_TOKEN=${RUNNER_API_TOKEN}
CMDB_BASE_URL=http://gateway:8080
CMDB_TOKEN=
CMDB_INTERNAL_TOKEN=${RUNNER_API_TOKEN}
RUNNER_COMMAND_MAP_JSON={"health-check --full":"health-check.yml","install-node-exporter":"install-node-exporter.yml"}
MAX_TARGETS=100
SSH_USER=${RUNNER_SSH_USER}
SSH_PORT=22
SSH_PASSWORD=
SSH_PRIVATE_KEY_FILE=/run/secrets/runner_id_ed25519
KNOWN_HOSTS_FILE=/run/secrets/runner_known_hosts
VAULT_PASSWORD_FILE=
S3_ENDPOINT_URL=http://minio:9000
S3_ACCESS_KEY_ID=${MINIO_ROOT_USER}
S3_SECRET_ACCESS_KEY=${MINIO_ROOT_PASSWORD}
S3_BUCKET=cmdb-job-artifacts
S3_REGION=us-east-1
EOF
chmod 600 "${ROOT_DIR}/secrets/runner.env"

"${COMPOSE[@]}" build --pull
"${COMPOSE[@]}" up -d postgres redis kafka keycloak vault minio neo4j opensearch \
	prometheus alertmanager grafana loki tempo node-exporter postgres-exporter redis-exporter snmp-exporter

for _ in $(seq 1 60); do
	if "${COMPOSE[@]}" exec -T vault vault version >/dev/null 2>&1; then
		break
	fi
	sleep 2
done

if [[ ! -f "${ROOT_DIR}/secrets/vault-init.json" ]]; then
	"${COMPOSE[@]}" exec -T vault vault operator init \
		-key-shares=1 -key-threshold=1 -format=json \
		>"${ROOT_DIR}/secrets/vault-init.json"
	chmod 600 "${ROOT_DIR}/secrets/vault-init.json"
fi

if ! "${COMPOSE[@]}" exec -T vault vault status >/dev/null 2>&1; then
	VAULT_UNSEAL_KEY="$(jq -r '.unseal_keys_b64[0]' "${ROOT_DIR}/secrets/vault-init.json")"
	"${COMPOSE[@]}" exec -T vault vault operator unseal "${VAULT_UNSEAL_KEY}" >/dev/null
fi

VAULT_TOKEN="$(jq -r '.root_token' "${ROOT_DIR}/secrets/vault-init.json")"
"${COMPOSE[@]}" exec -T -e VAULT_TOKEN="${VAULT_TOKEN}" vault sh -ec \
	'vault secrets enable -path=secret -version=2 kv >/dev/null 2>&1 || true'
sed -i "s|^VAULT_TOKEN=.*|VAULT_TOKEN=${VAULT_TOKEN}|" "${ROOT_DIR}/secrets/gateway.env"

"${ROOT_DIR}/scripts/init-keycloak.sh"

"${COMPOSE[@]}" up -d gateway web ansible-runner kafka-init minio-init

for _ in $(seq 1 90); do
	if curl -fsS "http://127.0.0.1:${GATEWAY_PORT}/api/v1/ready" >/dev/null 2>&1; then
		break
	fi
	sleep 2
done

"${COMPOSE[@]}" ps
echo "平台地址: ${CMDB_PUBLIC_URL}"
echo "Keycloak: ${KEYCLOAK_PUBLIC_URL}"
echo "管理账号: ${CMDB_ADMIN_USERNAME}"
echo "密码保存在 ${ENV_FILE} 的 CMDB_ADMIN_PASSWORD"
