#!/usr/bin/env sh
set -eu
test "$(id -u)" -eq 0 || { echo "run as root" >&2; exit 1; }
test -f ./cmdb-agent || { echo "place cmdb-agent binary in current directory" >&2; exit 1; }
id cmdb-agent >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin cmdb-agent
install -m 0755 ./cmdb-agent /usr/local/bin/cmdb-agent
install -d -m 0750 -o root -g cmdb-agent /etc/cmdb-agent
test -f /etc/cmdb-agent/agent.env || install -m 0640 -o root -g cmdb-agent ../agent.env.example /etc/cmdb-agent/agent.env
if [ -n "${CMDB_AGENT_TYPE:-}" ]; then
  if grep -q '^CMDB_AGENT_TYPE=' /etc/cmdb-agent/agent.env; then
    sed -i 's|^CMDB_AGENT_TYPE=.*|CMDB_AGENT_TYPE='"${CMDB_AGENT_TYPE}"'|' /etc/cmdb-agent/agent.env
  else
    echo "CMDB_AGENT_TYPE=${CMDB_AGENT_TYPE}" >> /etc/cmdb-agent/agent.env
  fi
fi
install -m 0644 ./cmdb-agent.service /etc/systemd/system/cmdb-agent.service
systemctl daemon-reload
systemctl enable --now cmdb-agent
systemctl --no-pager status cmdb-agent
