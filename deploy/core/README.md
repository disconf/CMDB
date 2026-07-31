# CMDB Core Docker Compose

该目录部署第一批开发/验收中间件：PostgreSQL、Redis、单节点 Kafka KRaft 和 Keycloak。

## 1. 准备配置

```bash
cd deploy/core
cp .env.example .env
```

修改 `.env`：

- 将所有 `CHANGE_ME_*` 替换为不同的高强度随机密码。
- `SERVER_HOST` 设置为应用实际连接 Kafka 时使用的服务器 IP 或 DNS。
- `KEYCLOAK_HOSTNAME` 设置为浏览器访问 Keycloak 的完整地址，例如 `http://10.0.0.8:8081`。
- 如果只允许本机访问，将 `BIND_ADDRESS` 设置为 `127.0.0.1`；远程联调才设为服务器内网 IP 或 `0.0.0.0`。
- `POSTGRES_DATA_DIR` 必须位于服务器本地 ext4/XFS 文件系统，不能位于 NFS、CIFS、SMB 或 FUSE 挂载目录。

准备 PostgreSQL 数据目录（Alpine PostgreSQL 镜像中的 postgres 用户 UID/GID 为 70）：

```bash
sudo mkdir -p /opt/cmdb/data/postgres
sudo chown -R 70:70 /opt/cmdb/data/postgres
sudo chmod 700 /opt/cmdb/data/postgres
stat -f -c '%T %n' /opt/cmdb/data/postgres
```

文件系统类型应为 `ext2/ext3`（Linux 输出通常代表 ext4）或 `xfs`，不能是 `nfs`、`cifs`、`fuseblk`、`9p`。

## 2. 校验并启动

```bash
docker compose --env-file .env -f docker-compose.yaml config --quiet
docker compose --env-file .env -f docker-compose.yaml build --pull
docker compose --env-file .env -f docker-compose.yaml up -d
docker compose --env-file .env -f docker-compose.yaml ps
```

### PostgreSQL 首次初始化出现 Operation not permitted

当前配置使用卷内子目录作为 `PGDATA`，并为 `/var/run/postgresql` 使用独立 tmpfs，以兼容禁止修改卷挂载点权限的 Docker 存储驱动。

如果旧配置已经产生了失败的空数据卷，并且确认其中没有需要保留的数据，可以执行：

```bash
docker compose --env-file .env -f docker-compose.yaml down
docker volume rm cmdb-postgres-data
if [ -d /opt/cmdb/data/postgres/pgdata ]; then
  sudo mv /opt/cmdb/data/postgres/pgdata "/opt/cmdb/data/postgres/pgdata.failed.$(date +%s)"
fi
sudo install -d -m 700 -o 70 -g 70 /opt/cmdb/data/postgres
docker compose --env-file .env -f docker-compose.yaml build --no-cache postgres
docker compose --env-file .env -f docker-compose.yaml up -d postgres
docker compose --env-file .env -f docker-compose.yaml logs -f postgres
```

如果卷中已经有有效数据库数据，不要删除数据卷，应先备份并检查卷的存储驱动和权限。

查看日志：

```bash
docker compose --env-file .env -f docker-compose.yaml logs -f --tail=200
```

## 3. 验收

```bash
# PostgreSQL
docker compose --env-file .env -f docker-compose.yaml exec postgres \
  sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "\\l"'

# Redis
docker compose --env-file .env -f docker-compose.yaml exec redis \
  sh -c 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli ping'

# Kafka
docker compose --env-file .env -f docker-compose.yaml exec kafka \
  /opt/kafka/bin/kafka-topics.sh --bootstrap-server kafka:19092 --list

# Keycloak：浏览器访问 KEYCLOAK_HOSTNAME
```

预期 Kafka 初始化容器执行完成后状态为 `Exited (0)`，这是正常状态。

## 4. 防火墙

只向需要联调的内网网段开放：

- `5432/tcp` PostgreSQL
- `6379/tcp` Redis
- `9092/tcp` Kafka
- `8081/tcp` Keycloak

不要直接向公网开放 PostgreSQL、Redis 和 Kafka。第一版 Kafka 使用 PLAINTEXT，仅允许受控内网访问；生产迁移前必须增加 TLS/SASL。

## 5. 停止与删除

停止但保留数据：

```bash
docker compose --env-file .env -f docker-compose.yaml down
```

删除数据卷会永久删除数据库、Keycloak、Redis 和 Kafka 数据，除非明确重建环境，否则不要执行 `down -v`。

## 6. 交接信息

部署完成后只提供以下脱敏信息，不要发送真实密码：

```text
服务器内网地址：
PostgreSQL 端口：5432
平台数据库：ops_platform
平台数据库用户名：ops_platform_app
Redis 端口：6379
Kafka 外部地址：SERVER_HOST:9092
Keycloak 地址：
Keycloak Realm：待创建/已创建
防火墙允许访问的来源网段：
```

密码保存在服务器 `.env`，后续由你在本地环境参数或 Secret 中引用。
