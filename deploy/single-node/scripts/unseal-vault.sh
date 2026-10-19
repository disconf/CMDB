#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INIT_FILE="${ROOT_DIR}/secrets/vault-init.json"
if [[ ! -f "${INIT_FILE}" ]]; then
	echo "缺少 ${INIT_FILE}" >&2
	exit 1
fi

UNSEAL_KEY="$(jq -r '.unseal_keys_b64[0]' "${INIT_FILE}")"
docker compose --project-directory "${ROOT_DIR}" --env-file "${ROOT_DIR}/.env" \
	-f "${ROOT_DIR}/docker-compose.yml" exec -T vault \
	vault operator unseal "${UNSEAL_KEY}"
