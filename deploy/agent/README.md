# Linux Agent安装

在构建机执行 `GOOS=linux GOARCH=amd64 go build -o cmdb-agent ./`，将二进制、`install.sh` 和 `cmdb-agent.service` 上传到目标主机同一目录。配置网关的 `AGENT_SHARED_TOKEN`，再编辑目标主机 `/etc/cmdb-agent/agent.env`，确保Agent令牌完全一致。

日志查看：`journalctl -u cmdb-agent -f`。停止三分钟（或 `AGENT_OFFLINE_AFTER` 指定时间）后，网关会把Agent和对应CMDB资产标记为离线。
