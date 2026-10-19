#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ROOT_DIR}/.env"
set -a
source "${ENV_FILE}"
set +a
COMPOSE=(docker compose --project-directory "${ROOT_DIR}" --env-file "${ENV_FILE}" -f "${ROOT_DIR}/docker-compose.yml")

LABEL="${1:-manual-$(date +%Y%m%d-%H%M%S)}"
BACKUP_DIR="${CMDB_BACKUP_DIR:-${CMDB_DATA_DIR}/backups}/${LABEL}"
mkdir -p "${BACKUP_DIR}"

"${COMPOSE[@]}" exec -T postgres \
	pg_dumpall -U "${POSTGRES_ADMIN_USER}" \
	>"${BACKUP_DIR}/postgres.sql"

tar -C "${CMDB_DATA_DIR}" -czf "${BACKUP_DIR}/minio.tar.gz" minio
tar -C "${CMDB_DATA_DIR}" -czf "${BACKUP_DIR}/vault.tar.gz" vault
tar -C "${CMDB_DATA_DIR}" -czf "${BACKUP_DIR}/neo4j.tar.gz" neo4j
tar -C "${CMDB_DATA_DIR}" -czf "${BACKUP_DIR}/opensearch.tar.gz" opensearch
cp "${ROOT_DIR}/.env" "${BACKUP_DIR}/env.backup"
chmod -R 600 "${BACKUP_DIR}"

echo "备份完成: ${BACKUP_DIR}"
