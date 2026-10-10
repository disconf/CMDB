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
## 根因反馈与准确率

- `confirmed`：人工确认根因正确。
- `partial`：根因部分正确。
- `rejected`：根因错误，可填写修正根因。
- 反馈写入 `aiops_analyses.feedback`。
- 准确率按 `confirmed / (confirmed + rejected)` 计算；没有反馈时显示 0，不拿置信度冒充准确率。

## 受控 Runbook 执行闭环

```text
AIOps 推荐 Runbook
  → 创建 change 工单和一次性授权
  → 人工在工单中心审批
  → AIOps 消费一次性授权
  → 查找白名单模板
  → 创建任务并启动
  → 任务结束回调 AIOps
  → 写回 running / success / failed / rejected
```

安全约束：

- 只允许白名单 Runbook 映射到已存在的任务模板。
- 当前 Runbook 默认映射到只读 `tpl-health`。
- 拒绝任意命令、任意模板和浏览器直接执行。
- 工单驳回不会创建任务。
- 任务失败原因会回写到 AIOps 执行记录。

## 稳定性

Gateway 处理大量 CMDB 资产、告警和 Agent 心跳时需要更高的 Go 内存预算：

- 容器内存请求：512Mi。
- 容器内存限制：4Gi。
- `GOMEMLIMIT=3500MiB`。
- `GOGC=75`。
## 只读证据补充

分析生成后可以调用：

```text
POST /api/v1/aiops/analyses/{id}/evidence
```

默认证据策略：

- 资产名称/DNS 可用时执行 DNS 解析。
- 资产 ID 可用且 Prometheus 已配置时查询 `up{cmdb_asset_id="..."}`。
- Pod/Kubernetes 告警从 `namespace/pod` 目标补充 Kubernetes 诊断。
- 用户显式传入工具时，只允许 `dns`、`tcp`、`http`、`tls`、`prometheus`、`k8s-pod`。
- 工具执行结果写入排查工具箱历史，并转换为 AIOps 证据链条目。

## 报告导出

```text
GET /api/v1/aiops/analyses/{id}/report
GET /api/v1/aiops/analyses/{id}/report?format=markdown
```

报告包含：

- 分析编号、告警、根因、置信度和严重级别
- 分析摘要和人工反馈
- 证据链
- 关联资产和拓扑影响
- 故障时间线
- Runbook 建议
- 受控执行记录

Markdown 模式可直接下载，作为故障复盘和工单附件的基础格式。
## 反馈学习与置信度调整

- 系统统计同一根因的 `confirmed`、`rejected`、`partial` 反馈。
- 完成样本达到 3 条后：
  - 确认率高于 50% 时提高后续分析置信度，最高 `+10`。
  - 确认率低于 50% 时降低后续分析置信度，最低 `-10`。
  - 部分正确反馈会轻微降低调整值。
- 同一告警标题存在人工修正根因时，最近一次修正根因会优先应用到后续分析。
- 分析结果记录：
  - `confidenceAdjustment`
  - `confidenceReason`
  - `appliedFeedbackCount`
- 反馈统计接口：

```text
GET /api/v1/aiops/feedback/stats
```

返回整体准确率，以及按根因聚合的确认、否决、部分正确和调整值。
## Runbook 注册中心与变更门禁

Runbook 不再硬编码在代码中，而是持久化到 `aiops_runbook_policies`：

- `templateId`：只允许引用任务中心已存在的模板。
- `mode`：`read-only` 或 `change`。
- `risk`：`low`、`medium`、`high`。
- `enabled`：控制 Runbook 是否可用。
- `maxTargets`：限制单次执行目标数量。
- `allowedEnvironments`：限制允许执行的环境。
- `changeWindow`：限制允许执行的时间窗口。

默认策略：

- 7 个只读 Runbook 默认启用。
- 日志清理、滚动重启、安全补丁默认禁用。
- 变更类 Runbook 必须同时满足：
  - `AIOPS_ALLOW_CHANGE_RUNBOOKS=true`
  - Runbook 已启用
  - 工单审批通过
  - 一次性授权校验通过
  - 模板在任务中心存在
  - 环境、目标数量、变更窗口全部满足限制

任何一项不满足都会拒绝执行并写入 AIOps 执行失败原因。
## 变更演练模式

Runbook 支持两种执行方式：

- `simulate`：不创建真实任务，不修改目标主机，但仍执行审批、一次性授权、环境和变更窗口校验。
- `live`：通过所有门禁后创建任务并执行，必须开启 `AIOPS_ALLOW_CHANGE_RUNBOOKS=true`。

生产默认策略：

- 只读 Runbook 使用 `live` 并通过只读任务模板执行。
- 变更类 Runbook 使用 `simulate`，默认禁用。
- 只有明确完成演练和审批后，才允许管理员把指定 Runbook 切换为 `live`。

## 执行审计与受控回滚

- `GET /api/v1/aiops/execution-stats` 汇总演练、真实执行、待审批、运行中、成功、失败和执行成功率，并按 Runbook 聚合。
- Runbook 策略新增 `rollbackRunbookId`，仅变更类 Runbook 可以关联另一个变更类回滚 Runbook，不能关联自身。
- 失败的真实执行可调用 `POST /api/v1/aiops/executions/{id}/rollback` 申请回滚。
- 回滚申请只创建独立审批工单，不自动执行；审批通过后仍复用原有一套授权、环境、变更窗口和全局真实变更开关。
- 演练执行不会触发回滚，禁止对回滚执行再次申请回滚，并对同一原执行做重复申请保护。
