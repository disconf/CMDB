# CMDB 监控底座部署

监控组件部署在 `monitoring` 命名空间，包含 Prometheus、Alertmanager 和 Grafana。Prometheus 通过 CMDB HTTP 服务发现获取 `node_exporter` 目标，Alertmanager 通过网关 Webhook 将告警写回 CMDB。

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

kubectl -n monitoring rollout status deployment/prometheus
kubectl -n monitoring rollout status deployment/alertmanager
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
- Grafana 默认数据源 `Prometheus` 指向 `http://prometheus.monitoring.svc.cluster.local:9090`。