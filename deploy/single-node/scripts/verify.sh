#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ROOT_DIR}/.env"
set -a
source "${ENV_FILE}"
set +a
COMPOSE=(docker compose --project-directory "${ROOT_DIR}" --env-file "${ENV_FILE}" -f "${ROOT_DIR}/docker-compose.yml")

fail=0
check_http() {
	local name="$1" url="$2"
	if curl -fsS --max-time 5 "${url}" >/dev/null; then
		printf '%-20s OK\n' "${name}"
	else
		printf '%-20s FAIL %s\n' "${name}" "${url}"
		fail=1
	fi
}

"${COMPOSE[@]}" ps
check_http "web" "http://127.0.0.1:${WEB_PORT}/healthz"
check_http "gateway-ready" "http://127.0.0.1:${GATEWAY_PORT}/api/v1/ready"
check_http "keycloak" "${KEYCLOAK_PUBLIC_URL}/realms/master"
check_http "prometheus" "http://127.0.0.1:9090/-/ready"
check_http "alertmanager" "http://127.0.0.1:9093/-/ready"
check_http "grafana" "http://127.0.0.1:3000/api/health"
check_http "loki" "http://127.0.0.1:3100/ready"
check_http "tempo" "http://127.0.0.1:3200/ready"
check_http "opensearch" "http://127.0.0.1:9200/_cluster/health"

exit "${fail}"
