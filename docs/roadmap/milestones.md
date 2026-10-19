# CMDB 平台里程碑

> 更新时间：2026-10-19
> 当前环境：`ops-qa` 已上线，网关 `1.0.224`，门户 `1.0.176`

## 总体进度

| 维度 | 进度 | 说明 |
|---|---:|---|
| 功能开发 | 约 90% | 原计划 12 个阶段的主体能力均已实现 |
| 生产就绪 | 约 80% | 认证、审计、备份、灾备和可观测性已具备；数据库 HA 和备份已恢复，规模化压测验收待完成 |
| 综合进度 | 约 85% | 当前处于上线后稳定性治理和验收收口阶段 |

## 阶段状态

| 阶段 | 内容 | 状态 | 主要证据 |
|---|---|---|---|
| 0 | 需求收敛和架构规范 | 已完成 | 项目简介、实施方案、边界与设计文档 |
| 1 | 基础设施和开发框架 | 已完成 | K8s、PostgreSQL、Redis、Kafka、Keycloak、Vault、MinIO、Harbor、Argo CD |
| 2 | 认证、权限和系统管理 | 已完成 | Keycloak、RBAC、动态菜单、审计、生产就绪检查 |
| 3 | CMDB 核心 | 已完成主体 | 模型、资产、项目组、IDC、关系、历史、导入、检索 |
| 4 | 自动发现与 Agent | 已完成 | Linux/Windows Agent、mTLS、隔离吊销、采集计划、云和网络采集 |
| 5 | 拓扑中心 | 已完成 | Neo4j/OpenSearch 投影、关系拓扑、影响分析 |
| 6 | 监控中心 | 已完成 | Prometheus、Alertmanager、Thanos、Loki、Tempo、Grafana |
| 7 | 任务管理 | 已完成 | Ansible Runner、审批、调度、日志和重试 |
| 8 | 工单管理 | 已完成 | 持久化、SLA、告警转单和关联资产 |
| 9 | CI/CD 服务管理 | 已完成 | Argo CD、Harbor、GitLab、发布门禁、回滚和漂移检测 |
| 10 | 排查工具中心 | 已完成 | Prometheus、Loki、K8s、主机和网络排查工具 |
| 11 | AIOps | 已完成 | 知识库、RAG、反馈闭环、LLM 网关和 Runbook |
| 12 | 安全、压测和生产验收 | 进行中 | 备份和灾备脚本已有；目标规模压测、RTO/RPO 报告仍待补齐 |

## 线上运行快照

| 组件 | 状态 |
|---|---|
| gateway | 2/2 Ready，`1.0.224` |
| web | 2/2 Ready，`1.0.176` |
| cmdb-ansible-runner | 1/1 Ready，`1.0.5` |
| Keycloak | 2/2 Ready |
| Kafka | 3 broker + 3 controller Ready |
| Redis Cluster | 3 主 3 从 Ready |
| Neo4j / OpenSearch | Ready |
| Loki / Tempo / Thanos | Ready |
| Vault | 3/3 Ready，已解除 `vault-2` 封印 |
| PostgreSQL | 3/3 Ready；`postgres-ha-4`、`postgres-ha-5` 已重建，持续归档和基础备份成功 |

## 下一批事项

1. 更新部署清单到当前线上镜像版本，并提交、推送代码。
2. 执行 5000 资产、1000 Agent、500 并发任务目标压测，形成报告。
3. 完成一次数据库主从切换和恢复演练，记录 RTO/RPO。
4. 实施 CMDB P3：K8s 容量卡片和 kube-state-metrics 接入。
5. 拆分远程运维前端工作区，补齐文件断点续传和录像命令定位。
6. 在 CNPG 1.29 前评估迁移到 Barman Cloud Plugin，替代已弃用的 `barmanObjectStore` 配置。
