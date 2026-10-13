#!/usr/bin/env bash
set -euo pipefail
if [[ $# -lt 1 ]]; then echo "usage: $0 <backup.sql.gz>" >&2; exit 2; fi
NAMESPACE=${NAMESPACE:-ops-qa}
POD=${POD:-postgres-ha-2}
DATABASE=${DATABASE:-ops_platform}
FILE=$1
[[ -f "$FILE" ]] || { echo "backup not found: $FILE" >&2; exit 2; }
if [[ -f "$FILE.sha256" ]]; then sha256sum -c "$FILE.sha256"; fi
gunzip -c "$FILE" | kubectl -n "$NAMESPACE" exec -i "$POD" -- psql -U postgres -d "$DATABASE" -v ON_ERROR_STOP=1
echo "restore completed from: $FILE"
