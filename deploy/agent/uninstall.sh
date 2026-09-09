#!/usr/bin/env sh
set -eu
test "$(id -u)" -eq 0 || { echo "run as root" >&2; exit 1; }
systemctl disable --now cmdb-agent 2>/dev/null || true
rm -f /etc/systemd/system/cmdb-agent.service
rm -f /usr/local/bin/cmdb-agent
rm -rf /etc/cmdb-agent
userdel cmdb-agent 2>/dev/null || true
systemctl daemon-reload
echo "cmdb-agent removed"
