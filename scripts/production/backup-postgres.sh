#!/usr/bin/env bash
set -euo pipefail
NAMESPACE=${NAMESPACE:-ops-qa}
POD=${POD:-postgres-ha-2}
DATABASE=${DATABASE:-ops_platform}
BACKUP_DIR=${BACKUP_DIR:-./backups}
mkdir -p "$BACKUP_DIR"
STAMP=$(date +%Y%m%d-%H%M%S)
FILE="$BACKUP_DIR/ops-platform-$STAMP.sql.gz"
kubectl -n "$NAMESPACE" exec "$POD" -- pg_dump -U postgres -d "$DATABASE" | gzip -9 > "$FILE"
sha256sum "$FILE" > "$FILE.sha256"
echo "backup completed: $FILE"
