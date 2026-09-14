# Agent 与自动发现：模块设计（现状→优化→扩展）

## 1. 现状能力（代码已具备）
- Agent 注册/上报/心跳/离线判定；资产按 Agent 自动纳管并更新属性
- 采集器：Agent(主机本地上报)、SSH(远程采集)、node_exporter(网段扫描 9100)
- 入账链路：发现→去重(按IP)→合并刷新→CMDB（可人工字段不被覆盖）
- Agent 批量安装（网关自分发二进制，SSH 部署 systemd）＋ 资产自动归类(物理/虚拟)
- 发现任务表、真实采集任务记录，以及按固定周期执行的采集计划（node_exporter / SSH / SNMP）
- Agent 与发现页：总览(总数/在线/任务)、Agent 列表、发现任务、node_exporter 扫描、SSH 采集、批量安装

## 2. 现状痛点
1. “发现任务/执行扫描/同步至CMDB”按钮仍是**演示数据**（sampleItems），容易误导
2. 缺少：批量卸载/升级、Agent 版本管理、计划(定时)采集
3. 缺少：凭据管理（SSH 账号/密钥入库到 Vault）、按凭据分组的采集
4. 缺少：SNMP 网络设备采集器、K8s/云资源同步采集器
5. 缺少：审批流（发现→待审批→入账）、审计、任务进度/结果明细
6. 缺少：Agent 安装结果与失败重试、批量执行(远程命令)能力

## 3. 目标设计
把“Agent 与自动发现”做成统一的**“纳管与采集控制台”**，三层：
- 纳管层：Agent 安装/升级/卸载/版本/状态（生命周期）
- 采集层：适配器(Agent/SSH/SNMP/node_exporter/K8s/云) + 目标(网段/清单) + 计划(定时) + 凭据(Vault)
- 数据层：发现暂存 → 去重合并 → 入账(可审批) → 变更事件 → 监控/拓扑联动

## 4. 推荐功能与优先级

### P0（建议尽快，价值高改动小）
- [x] 批量安装（已完成）
- [x] **批量卸载/升级**（卸载脚本；升级=可校验的当前服务端版本，避免伪造历史二进制）
- [ ] 发现任务区“去伪存真”：隐藏演示执行按钮，展示真实扫描/采集记录
- [x] Agent 列表与版本台账：搜索/筛选/详情、版本分布、过期/未知版本统计

### P1（核心扩展）
- [x] **采集计划/定时**：在平台维护 node_exporter / SSH / SNMP 计划，支持分钟级到天级周期、暂停启用、立即执行和后台多实例抢占调度
- [x] **凭据中心**：SSH 账号密码、SNMP Community 存 Vault，支持分组/说明；私钥导入后续按需扩展
- [ ] **SNMP 采集器**：网络设备资产采集（型号/SN/接口/LLDP 邻居→拓扑）
- [x] Agent 版本管理：服务端当前版本、在线/离线、当前/过期/未知统计与批量升级

### P2（治理与体验）
- [x] 发现审批流：新发现 → 待审批 → 入账（可选自动；支持批准、拒绝、重复审批保护、重启后持久化）
- [x] 批量远程执行：服务端只读命令白名单、提交审批、逐台 SSH 执行与结果留存
- [x] 审计：谁/何时/对哪些主机做了什么（覆盖审批、凭据及主要人类管理操作）
- [x] 任务结果页：每次扫描的发现/合并/冲突/失败明细、逐项结果和任务日志

### P3（更广覆盖）
- [x] K8s 资源采集器：集群→Node→Namespace→Workload→Pod→Service→Ingress 自动入 CMDB（只读 ServiceAccount）
- [ ] 云资源同步（阿里/腾讯/AWS/VMware API）
- [x] Exporter 管理：一键装/卸 node_exporter，支持自定义端口，并将状态/端口回写 CMDB
- [x] 与监控联动：Agent/exporter 离线风险与采集质量报表；Prometheus 使用 up == 0 / absent(up) 做实际告警

## 5. 采集计划落地（2026-09-15）

已新增 `discovery_schedules` 持久化表和以下 API：

- `GET /api/v1/discovery/schedules`：查询采集计划与上次/下次执行状态。
- `POST /api/v1/discovery/schedules`：创建计划，采集类型支持 `node-exporter`、`ssh`、`snmp`。
- `PUT /api/v1/discovery/schedules/{id}`：编辑计划。
- `POST /api/v1/discovery/schedules/{id}/toggle`：暂停或恢复计划。
- `POST /api/v1/discovery/schedules/{id}/run`：立即执行一次，不改变原周期。
- `DELETE /api/v1/discovery/schedules/{id}`：删除计划。

调度器由 Gateway 后台协程运行，使用 PostgreSQL `FOR UPDATE SKIP LOCKED` 抢占到期计划，因此两个 Gateway 副本不会重复执行同一计划。每次执行会创建独立发现任务，任务详情、逐项结果和日志继续进入现有任务结果页；需要审批的采集结果会保持 `pending-approval`，审批通过后再入账。

执行周期支持 5/15/30 分钟、1/6/12 小时和 1 天。node_exporter 计划可配置多个候选端口，实际端口仍按资产发现的监听结果写回 CMDB，供 Prometheus 动态服务发现使用。

## 6. 采集质量与监控联动落地（2026-09-15）

新增 GET /api/v1/discovery/quality，从 Agent、Exporter、发现任务和采集计划四个维度生成实时质量报表：

- Agent：总数、在线/离线/待连接数量、在线率和版本分布。
- Exporter：服务器资产总数、激活/异常/未检测数量、覆盖率和实际端口分布（兼容 9100、19100 等自定义端口）。
- 任务：完成、失败、运行中、待执行数量和成功率。
- 计划：启用、运行中、失败数量和执行健康度。
- 问题清单：按严重/警告排序输出 Agent 离线、Exporter 异常、任务失败和计划失败，并给出处理建议。

前端“Agent 与发现”新增“采集质量”标签页，可直接跳转监控告警。Prometheus 侧建议至少维护两类规则：

```promql
up{job="cmdb-hosts"} == 0
absent(up{job="cmdb-hosts"})
```

第一条覆盖采集目标存在但抓取失败，第二条覆盖目标列表中完全没有该实例的情况；告警通过现有 Alertmanager Webhook 回写 CMDB 资产健康状态。

## 7. 数据模型建议（后续）
- agent_installs(host,status,version,installed_at) 安装记录
- collector_targets(id,kind,address,credential_ref,plan)
- discovery_runs(run_id,source,scope,started,finished,found,imported,merged,failed)
- credentials(id,name,kind,vault_path)

## 8. 页面信息架构建议
Agent 与发现页重排为：
1) 总览卡：Agent 在线/总数、纳管资产、采集任务、今日采集数
2) 标签页：
   - Agent（列表+安装/升级/卸载）
   - 网段扫描（node_exporter）
   - SSH 采集
   - SNMP 采集(后续)
   - 采集计划（周期、启停、立即执行、上次结果）
   - 任务记录（真实，含明细）
3) 右侧：选中主机/任务详情抽屉


