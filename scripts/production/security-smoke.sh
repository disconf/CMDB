#!/usr/bin/env bash
set -euo pipefail
BASE=${BASE_URL:-http://172.28.69.164:30080}
fail=0
check(){ if eval "$1"; then echo "PASS $2"; else echo "FAIL $2"; fail=1; fi }
check "[[ $(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/v1/cmdb/assets") == 401 ]]" "未认证访问资产被拒绝"
check "curl -s "$BASE/api/v1/health" | grep -q '\"status\":\"ok\"'" "健康检查正常"
if [[ -n "${VIEWER_TOKEN:-}" ]]; then check "[[ $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $VIEWER_TOKEN" -X POST "$BASE/api/v1/jobs") == 403 ]]" "只读用户不能创建任务"; fi
if [[ -n "${ADMIN_TOKEN:-}" ]]; then check "[[ $(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $ADMIN_TOKEN" "$BASE/api/v1/system/production-readiness") == 200 ]]" "管理员可读取生产就绪报告"; fi
exit $fail
