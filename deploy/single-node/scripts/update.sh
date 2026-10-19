#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ROOT_DIR}/.env"
if [[ ! -f "${ENV_FILE}" ]]; then
	echo "缺少 ${ENV_FILE}" >&2
	exit 1
fi

set -a
source "${ENV_FILE}"
set +a
COMPOSE=(docker compose --project-directory "${ROOT_DIR}" --env-file "${ENV_FILE}" -f "${ROOT_DIR}/docker-compose.yml")

"${ROOT_DIR}/scripts/backup.sh" "before-update-$(date +%Y%m%d-%H%M%S)"
"${COMPOSE[@]}" pull
"${COMPOSE[@]}" build
"${COMPOSE[@]}" up -d --remove-orphans
"${ROOT_DIR}/scripts/unseal-vault.sh" || true

for _ in $(seq 1 90); do
	if curl -fsS "http://127.0.0.1:${GATEWAY_PORT}/api/v1/ready" >/dev/null 2>&1; then
		echo "更新完成: ${CMDB_PUBLIC_URL}"
		exit 0
	fi
	sleep 2
done

echo "更新后健康检查失败，请查看 docker compose logs gateway web" >&2
exit 1
