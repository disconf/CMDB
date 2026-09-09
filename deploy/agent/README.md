# Linux Agent 安装与纳管

Agent 会按周期（默认 60s）采集主机信息上报网关，网关据此在 CMDB 自动新建/更新资产（合并按 agent id），
并支持 `CMDB_AGENT_TYPE` 指定资产归类（`physical-server` / `virtual-machine`）。

## 1. 构建
```bash
cd apps/cmdb-agent
GOOS=linux GOARCH=amd64 go build -trimpath -o cmdb-agent .
```

## 2. 准备令牌
在网关 Secret `cmdb-gateway-env` 中查看/设置 `AGENT_SHARED_TOKEN`，确保与主机上 agent.env 中的令牌一致。
**令牌属敏感信息，只写进主机的 agent.env，不要提交 Git。**

## 3. 安装（推荐：仓库 install.sh）
将 `cmdb-agent`、`install.sh`、`cmdb-agent.service` 放到同一目录，并准备 agent.env（参考 `../agent.env.example`），然后：
```bash
sudo CMDB_AGENT_TYPE=virtual-machine ./install.sh
```
- install.sh 会创建系统用户 `cmdb-agent`、安装到 `/usr/local/bin/cmdb-agent`，
  环境文件 `/etc/cmdb-agent/agent.env`（0640，root:cmdb-agent），并注册 systemd 服务。
- `CMDB_GATEWAY_URL` 必须是**主机可达**的网关地址；K8s 节点上用 NodePort 最稳，例如：
  `CMDB_GATEWAY_URL=http://127.0.0.1:30080`（网关 Service 已开 NodePort 30080）。

## 4. agent.env 示例（不含真实令牌）
```ini
CMDB_GATEWAY_URL=http://127.0.0.1:30080
CMDB_AGENT_TOKEN=<与网关一致的共享令牌>
CMDB_AGENT_TYPE=virtual-machine
CMDB_REPORT_INTERVAL=60s
```

## 5. 运维
- 日志：`journalctl -u cmdb-agent -f`
- 卸载：`sudo ./uninstall.sh`
- 离线：主机停止上报超过 `AGENT_OFFLINE_AFTER`（默认 3 分钟）后，网关会把该 Agent 与其
  CMDB 资产标为离线（`cmdb_assets.status='offline'`）。
