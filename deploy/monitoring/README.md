# Prometheus 与 Alertmanager

## 启动

```bash
cd /opt/cmdb/deploy/monitoring
cp .env.example .env
docker compose config
docker compose build --pull
docker compose up -d
docker compose ps
```

访问地址：

- Prometheus：`http://192.168.85.134:9090`
- Alertmanager：`http://192.168.85.134:9093`

## 验证

```bash
docker compose exec prometheus promtool check config /etc/prometheus/prometheus.yml
docker compose exec prometheus promtool check rules /etc/prometheus/rules/cmdb-alerts.yml
docker compose exec alertmanager amtool check-config /etc/alertmanager/alertmanager.yml

curl -fsS http://127.0.0.1:9090/-/healthy
curl -fsS http://127.0.0.1:9093/-/healthy
curl -fsS http://127.0.0.1:9090/api/v1/targets
```

Alertmanager Webhook 地址为：

```text
http://host.docker.internal:8080/api/v1/monitor/alerts/webhook
```

Linux Compose 通过 `host-gateway` 将该地址解析到宿主机。CMDB 网关必须监听宿主机 `8080` 端口。

CMDB 网关已提供 `/metrics` 和 Alertmanager Webhook 接收接口；部署新版 Gateway 后，`cmdb-gateway` target 应显示为 up。

## PostgreSQL 与 Redis Exporter

先在 PostgreSQL中创建专用只读监控账号：

```bash
docker exec -it cmdb-postgres psql -U postgres -d postgres
```

```sql
CREATE ROLE postgres_exporter LOGIN PASSWORD '请替换为独立强密码';
GRANT CONNECT ON DATABASE postgres TO postgres_exporter;
GRANT pg_monitor TO postgres_exporter;
```

将密码写入本目录 `.env` 中的 `POSTGRES_EXPORTER_DSN`，并设置 `REDIS_EXPORTER_PASSWORD`。不要使用 PostgreSQL管理员账号作为长期监控账号。

```bash
docker compose up -d postgres-exporter redis-exporter
docker compose restart prometheus
```
