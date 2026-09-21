# AIOps 真实分析设计

## 目标

AIOps 第一阶段只做分析和受控建议，不直接执行服务器操作。分析引擎从真实运维数据构建证据链，输出根因假设、置信度、影响范围和 Runbook 建议。

## 数据流

```text
未恢复告警
  → AIOps 分析引擎
      → 告警事件时间线
      → Prometheus 指标
      → CMDB 资产与关系
      → 拓扑影响
      → 关联工单
      → 近期发布变更
  → 根因分类 + 置信度
  → Runbook 建议
  → 人工点击“申请审批”
  → 工单中心创建 change 工单
```

## 持久化

- `aiops_analyses`：保存分析、证据、影响、时间线、关联资产、工单和发布。
- `aiops_execution_requests`：保存 Runbook 审批申请，关联工单 ID。

## 根因规则

当前阶段采用可解释的规则归因，避免把不确定模型结论直接当作事实：

- 内存/OOM/内存指标 → 内存资源压力
- 磁盘/disk/inode → 磁盘空间耗尽风险
- 接口 Down/link/链路 → 网络链路异常
- Pod/K8s/Kubernetes → Kubernetes 工作负载异常
- P95/延迟/latency/timeout → 服务延迟异常
- 数据库/连接池/slow query → 数据库连接或慢查询压力
- 未匹配规则 → 待进一步确认的异常

## 安全边界

- 分析工具全部只读。
- 不调用任意 Shell。
- 不自动修改 CMDB、Kubernetes、网络设备或服务器。
- Runbook 只创建审批工单，后续需要工单审批和受控执行阶段处理。
- Vault 凭据不返回给 AIOps 分析结果。

## API

```text
GET  /api/v1/aiops/summary
GET  /api/v1/aiops/alerts
GET  /api/v1/aiops/analyses
POST /api/v1/aiops/analyses/generate
GET  /api/v1/aiops/analyses/{id}
POST /api/v1/aiops/analyses/{id}/execute
GET  /api/v1/aiops/knowledge
GET  /api/v1/aiops/executions
```

## 后续阶段

- 接入只读日志和链路工具作为分析证据。
- 增加分析反馈，记录“根因被确认/被否定”。
- 按历史确认结果计算真实准确率。
- 接入受控任务平台，在审批通过后执行白名单 Runbook。
- 输出 PDF/JSON 故障报告。