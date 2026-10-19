# CMDB 单机 Docker Compose 部署包

该目录提供平台运行所需的业务服务、核心中间件和可观测性组件，用于测试、演示、POC 和边缘单机环境。

## 快速开始

```bash
cd deploy/single-node
cp .env.example .env
vi .env
sudo ./scripts/bootstrap.sh
sudo ./scripts/verify.sh
```

`.env` 中至少需要修改：

- `SERVER_HOST`：服务器的内网 IP 或 DNS。
- `KEYCLOAK_PUBLIC_URL`：浏览器访问 Keycloak 的地址。
- `CMDB_PUBLIC_URL`：浏览器访问平台的地址。
- `CMDB_DATA_DIR`：本地数据目录，默认 `/opt/cmdb`。

`bootstrap.sh` 会生成数据库、Redis、Keycloak、MinIO、Neo4j、Grafana、Runner 和 Agent 所需随机密钥，初始化 Vault，创建 Keycloak Realm、客户端、角色和平台管理员。

## 常用命令

```bash
sudo ./scripts/verify.sh
sudo ./scripts/backup.sh
sudo ./scripts/update.sh
sudo ./scripts/unseal-vault.sh
docker compose --env-file .env -f docker-compose.yml ps
docker compose --env-file .env -f docker-compose.yml logs -f gateway web
```

## 说明

- 单机模式不提供高可用，不能替代生产 Kubernetes 集群。
- Vault 使用文件存储，主机重启后需要执行 `unseal-vault.sh`。
- Argo CD、Harbor、GitLab、Rancher、Kubernetes 和外部 LLM 属于集成目标，不在单机 Compose 内启动。
- 正式部署前请阅读 [平台部署手册](../../docs/deployment/平台部署手册-单机与Kubernetes.md)。
