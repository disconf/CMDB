# CMDB 与 Prometheus/监控/拓扑 的结合方式

> 解决“CMDB、node_exporter、Prometheus、SNMP、监控告警、链路拓扑如何分工与联动”的问题。

## 0. 当前落地状态（2026-09-10）

- 已在 Kubernetes `monitoring` 命名空间部署 Prometheus、Alertmanager、Grafana，并配置持久化存储。
- Prometheus 已通过 CMDB HTTP 服务发现自动获取主机目标；当前 8 台 K8s 主机（`172.28.69.161-168`）的 `node_exporter:9100` 全部为 `up`。
- Agent 与发现支持一次扫描多个候选端口（如 `9100,19100`），按每台主机实际监听的端口写入 `node_exporter_port`；Prometheus 使用该资产端口抓取，不要求所有主机统一端口。
- CMDB 资产扩展属性会保留 `node_exporter_status`、`node_exporter_port`、`node_exporter_version`，Agent 周期上报不会覆盖 exporter 信息。
- Alertmanager 告警触发与恢复事件均已通过网关 Webhook 回写 CMDB，告警中心可查询 `firing/resolved` 状态。
- Grafana 的 Prometheus 数据源已创建，并可通过数据源代理查询 `up` 指标。
- Prometheus 服务发现接口已启用 Agent Token 鉴权；Alertmanager Webhook 对 `endsAt`、`generatorURL` 等标准字段做兼容接收。
- 已部署 `snmp_exporter:0.30.1`，CMDB 新增 `/api/v1/monitor/service-discovery/snmp`；真实 SNMP 设备 `172.31.42.124` 已作为 Prometheus 动态目标，设备和接口指标采集正常。
- 拓扑中心已改为读取 CMDB 真实资产与关系；SNMP 扫描支持采集 LLDP 邻居并生成设备链路，未识别的邻居会作为待完善节点展示。
- Grafana 已增加“CMDB 网络设备监控”大盘，展示设备状态、接口流量、错误、丢弃和接口 Up/Down。
- SNMP 采集模块已支持华为、华三、Cisco、锐捷常见 CPU/内存/温度 OID；模块选择由 CMDB 根据厂商和 OID 探测结果动态下发，接口指标始终保留。

## 1. 核心结论（先记住三条）

1. **CMDB 是资产/关系的唯一事实源（“有什么、是什么、和谁相关”）**；Prometheus 只负责指标（“现在每秒/每分钟是多少”）。
2. **不要**让 CMDB 去轮询 Prometheus 来当资产来源，也**不要**把 Prometheus 的 target 手工配成资产源。
   - 正确方向：**CMDB → Prometheus（给目标清单）**；监控数据不回灌成资产。
   - 反向回写只做“状态/事件”：Prometheus/Alertmanager → CMDB 更新资产健康与告警，而不是把指标存进 CMDB。
3. **SNMP 采集拆成两类**：
   - 资产类（型号/SN/接口清单/邻居）→ 低频，由 CMDB 采集器负责（snmp 一次性/定时，走 `/discovery/ingest`）；
   - 指标类（流量/丢包/CPU/温度）→ 高频，由 Prometheus + **snmp_exporter** 负责，**目标清单来自 CMDB**。

## 2. 两段式目标发现：Prometheus 怎么知道“采谁”

现状：CMDB 已登记 node_exporter 主机（source=linux-agent / node-exporter），并已有
`GET /api/v1/monitor/service-discovery/node-exporter` 返回 Prometheus 风格的 targets（IP:9100 + labels）。

推荐做法（标准、无需逐台手配）：

```
Prometheus (http_sd_config)
        │ 定期 GET
        ▼
CMDB 服务发现 API
   /api/v1/monitor/service-discovery/node-exporter   → [{targets:["ip:9100"],labels:{asset,project_group,idc,...}}]
   /api/v1/monitor/service-discovery/snmp            → [{targets:["ip:9116?target=..."],labels:{module:"if_mib"}}]（新增）
```

- Prometheus 侧配置示例：
```yaml
scrape_configs:
  - job_name: node
    http_sd_configs:
      - url: http://gateway:8080/api/v1/monitor/service-discovery/node-exporter
        bearer_token_file: /etc/prometheus/token/agent-token
  - job_name: snmp
    metrics_path: /snmp
    http_sd_configs:
      - url: http://gateway:8080/api/v1/monitor/service-discovery/snmp
        bearer_token_file: /etc/prometheus/token/agent-token
    params:
      auth: ["public_v2"]
    relabel_configs:
      - source_labels: [__param_target]      # CMDB 给出真实设备IP
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
```
- 好处：主机/设备在 CMDB 增删（纳管/下线）后，Prometheus 自动跟随，**不需要在 Prometheus 配主机**。
- 如果不想让 Prometheus 直连 CMDB，可用 CMDB 定时导出 `file_sd` 文件/ConfigMap，二选一即可（推荐 http_sd）。

## 3. 反向：状态/告警怎么回到 CMDB

已有基础（代码里 monitor 模块已具备）：Alertmanager webhook → `POST /api/v1/monitor/alerts/webhook` → 网关告警中心表，并支持 rules/silences/routing/escalation 的 API。

建议闭环：
```
Prometheus Rules(up==0/阈值)
   → Alertmanager
   → webhook → CMDB 告警中心（入库）
   → 状态同步：对主机资产置 warning/offline（低频率、事件式，勿用定时全量轮询）
```
- 即：**监控负责“判异常”，CMDB 负责“记录状态与历史”，平台 UI 聚合展示**。

## 4. 部署组件清单（相对现有 CMDB）

| 组件 | 用途 | 数据方向 |
|---|---|---|
| node_exporter（主机） | 主机指标 | Prometheus 抓取 |
| snmp_exporter（每网络区域或独立） | SNMP 转 Prometheus 指标 | Prometheus 抓取 |
| Prometheus | 指标存储/告警计算 | targets 由 CMDB http_sd 提供 |
| Alertmanager | 告警分组/路由/静默 | webhook → CMDB |
| （可选）黑盒 exporter | ICMP/TCP/HTTP 探测 | Prometheus |
| Grafana | 展示 | 读 Prometheus |

## 5. 拓扑/链路怎么实现（项目→应用→服务→实例→基础设施）

### 5.1 拓扑 = CMDB 的“关系图”（静态骨架） + Prometheus 的“状态着色”（动态）
Prometheus **不是**拓扑数据来源；它提供的是“某个节点现在是否健康”，用来在大屏上着色和做影响分析。

分层关系由这些来源维护进 CMDB（现有 `relations`/未来 relation 表）：
- 业务层（项目→应用→服务）：发布/部署登记、手工维护（P1 已支持自定义字段/关系扩展）。
- 实例层：Agent 上报（主机）、K8s 采集（Namespace→Deployment→Pod→Node，后续 K8s 采集器）、进程/端口探测。
- 网络/基础设施层：
  - **LLDP/CDP 邻居**：SNMP v2 采集（`1.0.8802.1.1.2.1.4` 等）→ CMDB 生成“设备↔设备”链路；
  - 机柜/端口：手工或 CMDB SNMP 资产采集补充。

### 5.2 前端展示与影响分析
- 展示：现有 `TopologyPage`（五层）继续用 CMDB `/api/v1/topology/graph` 与 `/api/v1/topology/nodes/{id}/impact` 渲染。
- 影响分析算法：以 CMDB 关系图做 BFS 上下游扩散（如：某台主机 down → 其上实例/服务/应用受影响清单），再叠加 Prometheus 状态（哪些节点现在异常）给出“受影响且异常”排序。
- 动态刷新：CMDB 变更事件（outbox/Kafka：`cmdb.asset.changed.v1` / `cmdb.asset.status.v1`）驱动拓扑增量更新与监控纳管（P4 预留）。

## 6. 推荐的落地顺序

1. **部署监控底座**：Prometheus + Alertmanager（+ Grafana），复用现有监控 namespace/编排。
2. **打通服务发现**：网关新增 `/monitor/service-discovery/snmp`，node-exporter 已有；Prometheus 用 http_sd 拉 CMDB。
3. **告警闭环**：Alertmanager → CMDB webhook，资产状态回写（warning/offline），大屏联动。
4. **网络设备试点**：给 CMDB 纳管交换机（SNMP 资产采集 → ingest）→ Prometheus snmp_exporter 出指标 → LLDP 生成链路 → 拓扑着色。
5. **开启 Kafka 事件**：资产变更/状态 → 监控纳管与拓扑自动刷新。

## 7. 一句话总结
**用 CMDB 管“发现与关系”，用 Prometheus 管“指标与告警”；Prometheus 的目标清单由 CMDB 动态下发（http_sd），CMDB 的状态由告警事件回写；拓扑以 CMDB 关系为骨架、以 Prometheus 状态为着色与影响因子。**
