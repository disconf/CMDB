# CMDB 智能运维平台

当前版本：**V1.5**。平台已在 `ops-qa` 环境上线，使用 Keycloak、PostgreSQL、Redis、Kafka、Vault、MinIO、Prometheus、Thanos、Loki、Tempo、Neo4j 和 OpenSearch 等真实基础设施。

## 当前状态

| 项目 | 状态 |
|---|---|
| 线上环境 | `ops-qa`，门户与网关双副本 |
| 网关镜像 | `cmdb/gateway-bff:1.0.226` |
| 门户镜像 | `cmdb/web-portal:1.0.178` |
| 任务执行器 | `cmdb/ansible-runner-service:1.0.5` |
| 功能开发 | 约 90% |
| 生产就绪 | 约 92%，数据库高可用、K8s 容量卡片、资产/Agent/任务并发压测已完成，仍需完成 RTO/RPO、真实 Runner 500 并发上限和正式验收报告 |
| 进度清单 | [docs/roadmap/milestones.md](docs/roadmap/milestones.md) |

生产环境设置 `CMDB_ENABLE_DEMO_DATA=false`，不再生成演示资产、告警、工单、发布和分析记录。

## 核心能力

- **CMDB**：CI 模型、资产台账、项目组、机房机柜 U 位、关系拓扑、变更历史、CSV 导入、责任治理和监控联动。
- **Agent 与发现**：Linux/Windows Agent、mTLS、签名上报、隔离吊销、采集计划、健康巡检、SSH/SNMP/Redfish、VMware、云平台和 Kubernetes 采集。
- **监控与可观测性**：Prometheus、Alertmanager、Thanos 长期存储、Loki 日志、Tempo 链路和 Grafana。
- **拓扑与影响分析**：Neo4j/OpenSearch 资产投影、上下游依赖和故障影响路径。
- **任务与工单**：Ansible Runner 真实执行、审批、定时调度、实时日志、工单 SLA 和告警转单。
- **CI/CD 与发布**：Argo CD、Harbor、GitLab 集成，发布门禁、回滚、漂移检测和 CMDB 关联。
- **远程运维**：WebSSH、文件传输、批量执行、高风险命令审批、录像回放、主机指纹和安全策略。
- **AIOps**：知识库、RAG 问答、引用反馈、Embedding 适配、LLM 网关和 Runbook。
- **知识库**：多知识空间、多级目录、Markdown/富文本双编辑器、目录与文档 ACL、版本历史、附件和私有空间。
- **系统管理**：Keycloak 认证、RBAC、数据范围、审计和生产就绪检查。

## 目录

```text
apps/gateway-bff          Go 聚合网关与业务 API
apps/web-portal           Vue 3 运维门户
apps/cmdb-agent           Linux/Windows 主机 Agent
apps/ansible-runner-service  Ansible 任务执行器
deploy/k8s                Kubernetes 部署清单
docs/design               设计文档
docs/deployment           环境部署与发布记录
docs/operations           备份、灾备和运维手册
docs/roadmap              里程碑与进度
scripts                   构建、压测和运维脚本
```

## 本地启动

需要 Node.js 24+、npm 11+ 和 Go 1.26+。

启动后端：

```powershell
cd apps/gateway-bff
go run ./cmd/server
```

后端默认监听 `http://localhost:8080`。未配置 PostgreSQL 等外部依赖时会使用内存数据，适合本地开发。

另开一个终端启动前端：

```powershell
cd apps/web-portal
npm install
npm run dev
```

浏览器访问 `http://localhost:5173`。Vite 会把 `/api` 请求代理到本地 Go 服务。

本地开发可使用内存验证账号：

| 角色 | 用户名 | 密码 |
|---|---|---|
| 平台管理员 | `admin` | `admin123` |
| 只读用户 | `viewer` | `viewer123` |

线上环境统一使用 Keycloak，不再使用上述本地账号。

## 验证

```powershell
cd apps/gateway-bff
go test ./...
go vet ./...

cd ../web-portal
npm test
npm run build
```

## 部署与运维

- 部署形态、镜像和发布步骤：[docs/deployment/v1.5-ops-qa-deployment.md](docs/deployment/v1.5-ops-qa-deployment.md)
- 备份、灾备与压测：[docs/operations/生产就绪与灾备演练.md](docs/operations/生产就绪与灾备演练.md)
- 远程运维设计与验收：[docs/design/远程运维重构设计.md](docs/design/远程运维重构设计.md)
- CMDB 资产优化状态：[docs/design/CMDB资产模块优化设计.md](docs/design/CMDB资产模块优化设计.md)
