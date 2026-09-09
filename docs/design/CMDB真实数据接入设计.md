# CMDB 真实数据接入设计与规划

> 目标：让 CMDB 从“虚拟演示数据”走向“真实可维护、可持续纳管”的资产底座。
> 本期只做设计与规划；实施按文末“分期计划”逐步开展。

## 1. 现状盘点（基于当前代码）

| 能力 | 现状 | 结论 |
|---|---|---|
| CI 模型与自定义字段 | 后端 `cmdb_models.fields`(jsonb) 已存字段定义；前端“CI模型管理”可编辑字段（text/number/boolean/date/select、必填、默认值） | ✅ 基础已有，缺常用模板 |
| 资产增删改查/导入 | 仅内置固定列（id/name/type/ip/环境/项目组/负责人/位置/source/tags）；**新建/编辑/CSV 不能填自定义字段值** | ⚠️ 手工维护自定义字段闭环缺失 |
| Agent 上报 | `cmdb-agent` 上报 hostname/ip/os/kernel/arch/cpu/mem/disk/boot；网关注册/心跳/离线判定有雏形；CMDB 有 `UpsertAgentAsset` 写入 attributes | ⚠️ 已通骨架，字段少、无批量安装/纳管流程 |
| 自动发现/对账 | `discovery_items` 暂存 + `Reconcile` 对账到 `cmdb_assets`，可发 Kafka 事件 | ⚠️ 数据结构在，缺真实采集器 |
| 关系/拓扑/历史 | 关系、变更历史、Import、Outbox 事件均已建表 | ✅ 可复用 |
| 凭据/密钥 | 未设计采集凭据存储（SSH 密码/密钥、SNMP community） | ❌ 需设计 |
| 数据源 | 全部为内置演示数据；真实接入后按 source 区分并保留演示/测试 | ⚠️ |

## 2. 目标与边界

### 2.1 目标
1. **手工可维护**：页面可维护任意 CI 模型的自定义字段（含服务器的带外管理地址、机房、模块、机柜、U 位、负责人等），并按模型做必填/校验；支持 CSV 模板导入自定义列。
2. **Agent 自动维护**：主机安装 agent 后自动采集并上报资产/状态（免密、本地上报）；支持心跳与离线判定。
3. **远程/免装采集**：不能装 agent 的主机可用 **SSH（用户名/密码或密钥）** 采集；跑着 **node_exporter** 的主机可由采集器拉取基础信息。
4. **网络设备 SNMP v2**：通过只读 community 采集交换机/路由器等网络设备。
5. **可持续治理**：来源标识、身份合并、变更历史、冲突优先级，不覆盖人工维护值。

### 2.2 边界（本期不做）
- 云平台 API（阿里云/腾讯/AWS/VMware/K8s）自动同步：后续单独阶段。
- 自动“处置/变更执行”（采集只读，改动走工单/任务审批）。
- 生产级采集高可用（先单采集器 + 定时任务）。

## 3. 数据模型设计

### 3.1 CI 模型（cmdb_models）
继续用 `fields jsonb` 定义每个 CI 类型的字段模板，扩展字段描述：

```jsonc
{
  "code": "physical-server",
  "name": "物理服务器",
  "fields": [
    {"name":"serial","label":"序列号(SN)","type":"text","required":false},
    {"name":"vendor","label":"厂商","type":"text"},
    {"name":"model","label":"型号","type":"text"},
    {"name":"bmc_ip","label":"带外管理IP(BMC)","type":"text"},
    {"name":"idc_name","label":"机房名称","type":"select","options":["上海一号机房",...]},
    {"name":"module_name","label":"模块/区域","type":"text"},
    {"name":"rack_no","label":"机柜号","type":"text"},
    {"name":"u_position","label":"U位","type":"text"},
    {"name":"os_name","label":"操作系统","type":"text"},
    {"name":"kernel","label":"内核","type":"text"},
    {"name":"cpu","label":"CPU","type":"text"},
    {"name":"memory","label":"内存","type":"text"},
    {"name":"disk","label":"磁盘","type":"text"},
    {"name":"mac_addresses","label":"MAC","type":"text"},
    {"name":"mutable_by_agent","label":"是否允许采集覆盖","type":"boolean"}
  ]
}
```

网络设备模型（network-device）字段示例：`vendor/model/serial/software_version/community_ref(引用凭据)/management_ip/site/rack/…`。

> 说明：`fields[].name` 用于 API 与采集映射；`label` 用于界面展示；“是否允许采集覆盖”在字段级实现，采集器只更新允许自动维护的字段。

### 3.2 资产（cmdb_assets）
- 保留固定列用于通用检索（name/ip/status/project_group/owner/location/source/environment）。
- 扩展属性继续用 `attributes jsonb`（name/label/value），`attributes` 与模型 `fields` 对应，前端按模型渲染表单。
- 新增/建议字段：
  - `identity_keys jsonb`：本类型唯一标识集合（如 `["hostname","serial"]`、`["sysname","management_ip"]`），用于查重/合并。
  - `verified_by` / `collected_at`：最近一次采集时间（可用 LastSeenAt 演进）。
  - `approval` 状态（可选：pending/approved），控制自动发现的资产是否直接入账。
- **来源标识**：`source ∈ {manual, csv, linux-agent, node-exporter, ssh, snmp, cloud-xxx}`；`manual` 优先级最高。

### 3.3 自定义字段的三处落点
1. 模型层：`cmdb_models.fields`（字段模板）
2. 资产层：`cmdb_assets.attributes`（实际值）
3. 表单/CSV/采集器：按模板渲染与映射

## 4. 数据来源与采集适配器

```
                     ┌─ 手工维护（页面/CSV）
                     ├─ Agent（Linux/Windows 主机本地上报，免密）
真实资产 ── 采集适配器 ──├─ SSH 采集（存量机/不方便装 agent：用户名/密码 或 密钥）
                     ├─ node_exporter 采集（主机已跑 exporter，走 HTTP）
                     └─ SNMP v2（网络设备：交换机/路由器/防火墙）
                              │
                              ▼
                discovery_items 暂存（含原始 jsonb）
                              │ 对账/去重/审批（可选）
                              ▼
                cmdb_assets（含 attributes/relations/历史）
                              │
                              ▼
                Kafka 变更事件（cmdb.asset.changed.v1 等，驱动监控/拓扑）
```

### 4.1 手工维护（P1）
- 后端：`Create/Update/Import` 支持 `attributes`（模型字段校验、required）。
- CSV：模板 = 固定列 + 该模型字段列，`parseAssetCsv` 按模型动态解析；错误行逐行反馈（复用 Import 接口语义）。
- 前端：资产表单按所选 CI 模型渲染字段（文本/数字/下拉/日期/布尔）；详情抽屉展示属性（已有 CollectedAttributes 可扩展编辑）。
- 模型管理：预置 physical-server / network-device / virtual-machine 等常用字段模板，支持导出/导入模型定义。

### 4.2 Agent 自动采集（P2）
- 部署：`deploy/agent/install.sh` + systemd + `cmdb-agent`（现有二进制），上报 `/api/v1/agent/report`。
- 采集内容扩展（root 或 sudo 读 /sys、dmidecode、lspci 等）：
  - 基础：hostname、ip、os/kernel/arch、cpu 核数、内存、磁盘、启动时间（现有）
  - 扩展：厂商/型号/SN（dmidecode）、网卡+MAC、BMC/带外地址（如可本地读 /sys/class 或配置文件）、agent 版本
- 心跳与离线：agent 每 60s 上报；网关按 `AGENT_OFFLINE_AFTER` 将超时主机标 offline（代码已有雏形，补齐到 DB 资产）。
- 纳管：agent 注册→自动在“发现/Agent”页出现；支持批量安装脚本（IP 列表+SSH 推送 install.sh）后续做。

### 4.3 SSH / node_exporter 采集（P3）
- SSH 采集（面向无法装 agent 的存量机）：采集器使用 SSH（密码或私钥，凭据存 Vault/Secret）执行只读命令（`uname`、`dmidecode -s system-*`、`lscpu`、`lsblk`、`ip -j addr`、`/sbin/ipmitool lan print` 可选），输出 JSON → 生成 `discovery.resource.found.v1`。
- node_exporter 采集：目标主机已在跑 node_exporter，采集器用 HTTP scrape `node_uname_info`、`node_os_info`、`node_memory_*`、`node_filesystem_*` 等，映射为资产基础字段；**不足以拿序列号**，可与 SSH/Agent 互补，或仅做“探测在线 + 基础信息”。
- 网络设备 **SNMP v2** 采集：community 只读（存 Vault）；OID：
  - `1.3.6.1.2.1.1.1` sysDescr（厂商/型号/软件）
  - `1.3.6.1.2.1.1.5` sysName
  - `1.3.6.1.2.1.1.7` sysServices
  - `1.3.6.1.2.1.2.2.1.2/6` ifDescr/ifPhysAddress（接口/MAC）
  - `1.3.6.1.2.1.4.20.1.1` ipAdEntIfIndex（管理/业务 IP）
  - 实体：`1.3.6.1.2.1.47.1.1.1.1.11`（序列号，视厂商支持）等
  - 可选 LLDP `1.0.8802.1.1.2.1.4` 生成资产关系（拓扑）

## 5. 身份、合并与冲突策略（关键设计）

### 5.1 唯一身份
每个 CI 类型定义 `identity_keys`：
- physical-server：`hostname + serial`（或 agent_id）
- virtual-machine：`hostname/instanceId + ip`
- network-device：`sysName + management_ip`（或 SN）
查重用“type + 任一身份键命中即视为同一资产”。

### 5.2 来源优先级与覆盖规则
```
manual(人工) > csv > ssh > agent > node-exporter > snmp
```
- 人工维护的字段（如机房/机柜/U位/负责人/带外管理IP）默认不被采集器覆盖；
- 采集器只更新：状态/在线、系统运行时字段（os/cpu/mem/disk/mac）与允许自动维护的字段；
- 冲突时写入历史 `changes`，不静默覆盖（历史表已具备）。

### 5.3 生命周期
- 新发现 → `discovery_items(state=pending)` → 对账创建资产（可配置为直接入账或待审批）→ `last_seen` 持续刷新 → 超时 `offline` → 长期离线标记 `stale`（人工确认后下线）。

## 6. 安全与凭据设计

- SSH 私钥/密码、SNMP community 一律放 **Vault**（集群已有 vault）或 k8s Secret，**不落 CMDB 明文、不进 Git**。
- 采集使用最小权限只读账号；SSH 优先密钥，禁止 root 密码远程登录（建议 sudo 只读命令白名单）。
- SNMP 只读 community；采集器与目标网段之间由网络策略限定。
- 所有采集/对账动作写入操作审计/资产历史。

## 7. 依赖的现有模块（复用而非重写）
- `cmdb`：模型字段、资产表、历史、Import
- `discovery`：agents、tasks、discovery_items、Reconcile（暂存→入账）
- `events/outbox` + Kafka：变更事件（真实 CMDB 数据接入后开启，topic 见附录）
- `cmdb-agent`/`deploy/agent`：主机 agent
- `ansible-runner-service`/runner：SSH 批量执行（可作为 SSH 采集执行器）
- `monitor`：node_exporter 目标与打标、告警联动

## 8. 分期计划

### P1 手工维护闭环（建议最先做，也是采集落库的前提）
- 后端：Create/Update/Import 支持 attributes 与模型字段校验；模型字段模板预置（server/network-device 等）
- 前端：资产表单按模型渲染自定义字段；CSV 模板按模型列；模型管理增强
- 验收：能新建一台“物理服务器”，填带外管理IP/机房/模块/机柜/U位/负责人，重新编辑保留，详情可见；CSV 导入含自定义列成功，错误行反馈

### P2 Agent 自动纳管
- agent 上报字段扩展；网关把上报落 DB 资产（upsert by identity）；心跳/离线到 DB；Agent 页真实列表
- 批量安装脚本（IP 清单 + SSH 推送）
- 验收：真实主机装 agent 后自动出现在 CMDB（online），卸载/断网后按策略转 offline

### P3 远程采集器与网络设备
- 采集器框架（Go）：SSH / node_exporter / SNMP v2 三个采集源；任务调度（周期/手动）
- 暂存→对账→入账（复用 discovery）；身份去重与冲突策略落地
- Vault/Secret 凭据接入；网络设备关系（LLDP）可选
- 验收：给定 SSH 主机/SNMP 交换机，运行发现任务后资产正确入账且人工字段不被覆盖

### P4 治理与联动
- 审批流（发现→待审批→入账）、离线/下线流程
- 开启 Kafka 变更事件；CMDB 变更驱动监控纳管与拓扑刷新
- 数据质量报表（覆盖率、一致性、重复资产）

## 9. 影响与回滚
- 所有新增为向后兼容：未填 attributes 的旧资产/接口不破坏；演示数据保留 source 区分，可在 P2 后切换“只看真实/只看演示”过滤。
- 采集器为独立部署（ops-qa 新 Deployment/Job），不侵入网关核心，失败不影响现有功能。

## 附录 A：建议 Kafka Topics（启用真实数据后）
- `cmdb.asset.changed.v1`（新增/变更）
- `cmdb.asset.status.v1`（上线/离线/下线）
- `discovery.resource.found.v1`（已有）
- `monitor.notification.requested.v1`（已有，监控侧）

## 附录 B：真实主机示例字段（physical-server）
| name | label | 来源 | 是否允许采集覆盖 |
|---|---|---|---|
| serial | 序列号 | agent/ssh/snmp | 是 |
| vendor/model | 厂商/型号 | agent/ssh/snmp | 是 |
| bmc_ip | 带外管理IP | 手工/agent(可选) | 否(默认手工) |
| idc_name | 机房名称 | 手工 | 否 |
| module_name | 模块/区域 | 手工 | 否 |
| rack_no | 机柜号 | 手工 | 否 |
| u_position | U位 | 手工 | 否 |
| owner | 负责人 | 手工（已为固定列） | 否 |
| os/kernel/cpu/mem/disk/mac | 运行时 | agent/ssh/node_exporter | 是 |
