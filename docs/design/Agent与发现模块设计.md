# Agent 与自动发现：模块设计（现状→优化→扩展）

## 1. 现状能力（代码已具备）
- Agent 注册/上报/心跳/离线判定；资产按 Agent 自动纳管并更新属性
- 采集器：Agent(主机本地上报)、SSH(远程采集)、node_exporter(网段扫描 9100)
- 入账链路：发现→去重(按IP)→合并刷新→CMDB（可人工字段不被覆盖）
- Agent 批量安装（网关自分发二进制，SSH 部署 systemd）＋ 资产自动归类(物理/虚拟)
- 发现任务表/执行(演示)与 Ingest 每天每源任务记录
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
- [ ] **批量卸载/升级**（卸载脚本；升级=重装新版）
- [ ] 发现任务区“去伪存真”：隐藏演示执行按钮，展示真实扫描/采集记录
- [ ] Agent 列表：搜索/筛选/详情（主机、版本、上次心跳、采集错误）

### P1（核心扩展）
- [ ] **采集计划/定时**：网段每 N 分钟扫 node_exporter；SSH 每日采集；计划任务后台调度
- [ ] **凭据中心**：SSH 账号/密码/私钥、SNMP community 存 Vault（k8s Secret 兜底），按组绑定采集
- [ ] **SNMP 采集器**：网络设备资产采集（型号/SN/接口/LLDP 邻居→拓扑）
- [ ] Agent 版本管理：版本注册/批量升级到指定版本

### P2（治理与体验）
- [x] 发现审批流：新发现 → 待审批 → 入账（可选自动；支持批准、拒绝、重复审批保护、重启后持久化）
- [x] 批量远程执行：服务端只读命令白名单、提交审批、逐台 SSH 执行与结果留存
- [x] 审计：谁/何时/对哪些主机做了什么（覆盖审批、凭据及主要人类管理操作）
- [x] 任务结果页：每次扫描的发现/合并/冲突/失败明细、逐项结果和任务日志

### P3（更广覆盖）
- [x] K8s 资源采集器：集群→Node→Namespace→Workload→Pod→Service→Ingress 自动入 CMDB（只读 ServiceAccount）
- [ ] 云资源同步（阿里/腾讯/AWS/VMware API）
- [ ] Exporter 管理：一键装/卸 node_exporter(9100) 并自动纳入监控
- [ ] 与监控联动：Agent/exporter 离线即告警；采集质量报表

## 5. 数据模型建议（后续）
- agent_installs(host,status,version,installed_at) 安装记录
- collector_targets(id,kind,address,credential_ref,plan)
- discovery_runs(run_id,source,scope,started,finished,found,imported,merged,failed)
- credentials(id,name,kind,vault_path)

## 6. 页面信息架构建议
Agent 与发现页重排为：
1) 总览卡：Agent 在线/总数、纳管资产、采集任务、今日采集数
2) 标签页：
   - Agent（列表+安装/升级/卸载）
   - 网段扫描（node_exporter）
   - SSH 采集
   - SNMP 采集(后续)
   - 任务记录（真实，含明细）
3) 右侧：选中主机/任务详情抽屉
