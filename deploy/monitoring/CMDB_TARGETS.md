# CMDB 动态主机监控

Gateway 提供 Prometheus HTTP 服务发现接口：

```text
GET /api/v1/monitor/service-discovery/node-exporter
```

接口仅返回状态为 `online`、来源为 `linux-agent` 且具有 IP 的资产。Prometheus
每 30 秒刷新目标，不需要重启。每个目标包含 `cmdb_asset_id`、资产名称、环境和
项目组标签，可用于指标查询与告警关联。

## 被监控主机准备

每台运行 CMDB Agent 的 Linux 主机还需要运行 node_exporter：

```bash
mkdir -p /opt/cmdb/node-exporter
cp node-exporter-compose.yml /opt/cmdb/node-exporter/docker-compose.yml
cd /opt/cmdb/node-exporter
docker compose up -d
curl -fsS http://127.0.0.1:9100/metrics >/dev/null
```

确保 Prometheus 所在服务器可以访问目标主机 TCP `9100` 端口。如果使用其他端口，
在 Gateway 环境变量中设置 `CMDB_NODE_EXPORTER_PORT`，并在所有被监控主机保持一致。

## 验证链路

```bash
curl -fsS http://127.0.0.1:8080/api/v1/monitor/service-discovery/node-exporter
curl -fsS http://127.0.0.1:9090/api/v1/targets
curl -GfsS http://127.0.0.1:9090/api/v1/query --data-urlencode 'query=up{job="cmdb-node-exporter"}'
```

Prometheus 的 Targets 页面中，`cmdb-node-exporter` 任务应出现已被 Agent 纳管的在线资产。
