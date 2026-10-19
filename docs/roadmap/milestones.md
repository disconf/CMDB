# CMDB 平台里程碑

> 更新时间：2026-10-19
> 当前环境：`ops-qa` 已上线，网关 `1.0.225`，门户 `1.0.177`

## 总体进度

| 维度 | 进度 | 说明 |
|---|---:|---|
| 功能开发 | 约 90% | 原计划 12 个阶段的主体能力均已实现 |
| 生产就绪 | 约 88% | 认证、审计、备份、灾备、可观测性、K8s 容量和 5,000 资产查询压测已具备；Agent/任务规模和 RTO/RPO 待验证 |
| 综合进度 | 约 90% | 当前处于上线后稳定性治理和验收收口阶段 |

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
| 12 | 安全、压测和生产验收 | 进行中 | 备份已恢复；5,000 资产 200 并发查询通过，1,000 Agent、500 并发任务和 RTO/RPO 仍待验证 |

## 线上运行快照

| 组件 | 状态 |
|---|---|
| gateway | 2/2 Ready，`1.0.225` |
| web | 2/2 Ready，`1.0.177` |
| cmdb-ansible-runner | 1/1 Ready，`1.0.5` |
| Keycloak | 2/2 Ready |
| Kafka | 3 broker + 3 controller Ready |
| Redis Cluster | 3 主 3 从 Ready |
| Neo4j / OpenSearch | Ready |
| Loki / Tempo / Thanos | Ready |
| Vault | 3/3 Ready，已解除 `vault-2` 封印 |
| PostgreSQL | 3/3 Ready；`postgres-ha-4`、`postgres-ha-5` 已重建，持续归档和基础备份成功 |
| kube-state-metrics | 1/1 Ready；Prometheus 已抓取 Pod、节点和资源预留指标 |

## 下一批事项

1. 执行 1,000 Agent 同时心跳和 500 并发任务压测，形成完整容量报告。
2. 完成一次数据库主从切换和恢复演练，记录 RTO/RPO。
3. 拆分远程运维前端工作区，补齐文件断点续传和录像命令定位。
4. 在 CNPG 1.29 前评估迁移到 Barman Cloud Plugin，替代已弃用的 `barmanObjectStore` 配置。

## 已完成压测

- 2026-10-19：`GET /api/v1/cmdb/assets?pageSize=50` 在集群内 200 并发下，错误率 0%，P95 219.35ms，平均 56.48ms。
- 2026-10-19：5,000 资产隔离环境，200 并发下错误率 0%，P95 671.92ms，平均 225.98ms。
- 详细结果：[docs/testing/2026-10-19-压测结果.md](../testing/2026-10-19-压测结果.md)
