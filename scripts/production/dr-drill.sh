#!/usr/bin/env bash
set -euo pipefail
NAMESPACE=${NAMESPACE:-ops-qa}
POD=${POD:-postgres-ha-2}
SOURCE_DB=${SOURCE_DB:-ops_platform}
DRILL_DB=${DRILL_DB:-ops_platform_dr_$RANDOM}
BACKUP_DIR=${BACKUP_DIR:-./backups}
SCHEMA_ONLY=${SCHEMA_ONLY:-0}
mkdir -p "$BACKUP_DIR"
STAMP=$(date +%Y%m%d-%H%M%S)
FILE="$BACKUP_DIR/dr-$STAMP.sql.gz"
cleanup(){ kubectl -n "$NAMESPACE" exec "$POD" -- psql -U postgres -d postgres -c "DROP DATABASE IF EXISTS \"$DRILL_DB\";" >/dev/null 2>&1 || true; }
trap cleanup EXIT
args=(pg_dump -U postgres -d "$SOURCE_DB")
if [[ "$SCHEMA_ONLY" == "1" ]]; then args+=(--schema-only); fi
kubectl -n "$NAMESPACE" exec "$POD" -- "${args[@]}" | gzip -9 > "$FILE"
sha256sum "$FILE" > "$FILE.sha256"
kubectl -n "$NAMESPACE" exec "$POD" -- psql -U postgres -d postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"$DRILL_DB\";"
gunzip -c "$FILE" | kubectl -n "$NAMESPACE" exec -i "$POD" -- psql -U postgres -d "$DRILL_DB" -v ON_ERROR_STOP=1 >/dev/null
TABLES=$(kubectl -n "$NAMESPACE" exec "$POD" -- psql -U postgres -d "$DRILL_DB" -tAc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';")
ASSETS=$(kubectl -n "$NAMESPACE" exec "$POD" -- psql -U postgres -d "$DRILL_DB" -tAc "SELECT count(*) FROM cmdb_assets;" 2>/dev/null || echo 0)
echo "DR_DRILL_SUCCESS backup=$FILE tables=$TABLES assets=$ASSETS schema_only=$SCHEMA_ONLY"
