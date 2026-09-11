# CMDB 监控底座部署

监控组件部署在 `monitoring` 命名空间，包含 Prometheus、Alertmanager、snmp_exporter 和 Grafana。Prometheus 通过 CMDB HTTP 服务发现获取 `node_exporter` 目标，Alertmanager 通过网关 Webhook 将告警写回 CMDB。

## 前置配置

先创建镜像仓库 Secret，并准备 Agent Token、Grafana 管理员密码：

```powershell
kubectl -n monitoring create secret docker-registry cmdb-registry `
  --docker-server=harbor.qa.bcavt.com `
  --docker-username=admin `
  --docker-password='<harbor-password>'

kubectl -n monitoring create secret generic prometheus-cmdb-token `
  --from-literal=agent-token='<agent-shared-token>'

kubectl -n monitoring create secret generic grafana-admin `
  --from-literal=admin-user=admin `
  --from-literal=admin-password='<grafana-password>'
```

将 `snmp-exporter.yml.template` 复制为 `snmp-exporter.yml`，替换 `__SNMP_COMMUNITY__` 后创建 Secret：

```powershell
kubectl -n monitoring create secret generic snmp-exporter-config `
  --from-file=snmp.yml=snmp-exporter.yml
```

## 配置与部署

```powershell
kubectl apply -f workloads.yaml

kubectl -n monitoring create configmap prometheus-config `
  --from-file=prometheus.yml=prometheus.yml --dry-run=client -o yaml | kubectl apply -f -
kubectl -n monitoring create configmap prometheus-rules `
  --from-file=rules.yml=rules.yml --dry-run=client -o yaml | kubectl apply -f -
kubectl -n monitoring create configmap alertmanager-config `
  --from-file=alertmanager.yml=alertmanager.yml --dry-run=client -o yaml | kubectl apply -f -
kubectl -n monitoring create configmap grafana-datasources `
  --from-file=grafana-datasource.yml=grafana-datasource.yml --dry-run=client -o yaml | kubectl apply -f -
kubectl -n monitoring create configmap grafana-dashboards `
  --from-file=grafana-dashboards.yml=grafana-dashboards.yml `
  --from-file=cmdb-host-overview.json=grafana-host-dashboard.json `
  --from-file=cmdb-network-overview.json=grafana-network-dashboard.json --dry-run=client -o yaml | kubectl apply -f -

kubectl -n monitoring rollout status deployment/prometheus
kubectl -n monitoring rollout status deployment/alertmanager
kubectl -n monitoring rollout status deployment/snmp-exporter
kubectl -n monitoring rollout status deployment/grafana
```

Prometheus 与 Alertmanager 使用单副本和 RWO 存储，Deployment 已使用 `Recreate` 策略，避免滚动更新时新旧 Pod 同时占用存储卷。

## 验证

```powershell
kubectl -n monitoring port-forward svc/prometheus 19090:9090
kubectl -n monitoring port-forward svc/alertmanager 19093:9093
kubectl -n monitoring port-forward svc/grafana 13000:3000
```

- Prometheus Targets 中 `cmdb-node-exporter` 应只包含 CMDB 中标记为已启用 exporter 的主机。
- Prometheus `up{job="cmdb-node-exporter"}` 用于确认主机采集状态。
- Alertmanager 触发和恢复通知会调用 `/api/v1/monitor/alerts/webhook`。
- 告警 Webhook 会同步 CMDB 资产健康：critical=firing 置 offline、warning=firing 置 warning、全部恢复后置 online。
- Grafana 默认数据源 `Prometheus` 指向 `http://prometheus.monitoring.svc.cluster.local:9090`。
- `cmdb-snmp` 任务通过 `snmp-exporter.monitoring.svc.cluster.local:9116` 抓取 CMDB 中真实 SNMP 发现的设备。
- PostgreSQL 使用 CNPG 原生 `9187` 指标端口，Keycloak 使用 `keycloak-metrics:9000`，均已接入 Prometheus。
- Alertmanager 对 critical 告警每 30 分钟重复通知，对 warning 告警每 4 小时重复通知，并与 CMDB 资产健康状态联动。
- SNMP 模块支持 `if_mib`、`host_resources`、`huawei`、`h3c`、`cisco_device`、`ruijie`；CMDB 根据厂商和 OID 探测结果自动选择模块，未识别厂商私有 OID 时回退 `host_resources` 或 `if_mib`。
- Agent 与发现可通过 `CMDB_AUTO_SCAN_NODE_CIDRS`、`CMDB_AUTO_SCAN_NODE_PORTS`、`CMDB_AUTO_SCAN_INTERVAL_MIN` 定期扫描 node_exporter；探测到的实际端口会写回资产的 `node_exporter_port`，Prometheus 直接使用该端口。
- 当前 CMDB 设备 `172.31.42.124` 使用 `host_resources` 模块，已采集 CPU、内存、磁盘、运行时长和接口指标。
- Grafana 自动加载“CMDB 主机监控”和“CMDB 网络设备监控”大盘。
## Grafana 访问入口

Grafana 通过现有 APISIX 域名暴露在：

```text
https://cmdb.jzq.com:31443/grafana/
```

APISIX 路由定义保存在 `apisix-grafana-route.json`。系统会通过 `grafana-dashboards` ConfigMap 自动加载“CMDB 主机监控”大盘，包含主机总数、在线数、离线数、CPU、内存、根分区和采集状态。