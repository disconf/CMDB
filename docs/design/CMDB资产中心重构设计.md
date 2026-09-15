# CMDB 资产中心重构设计

## 1. 定位

CMDB 是运维自动化平台的资产事实源和关系枢纽：

- 统一纳管服务器、K8s、中间件、数据库、网络设备。
- 维护资产、项目组、负责人、机房/机柜/U 位和关联关系。
- 将资产的关键 Exporter 端点下发为 Prometheus 监控目标。
- 不直接承担指标采集、告警计算和 K8s 控制台功能。

## 2. 前端信息架构

| 工作区 | 职责 |
|---|---|
| 资产总览 | 资产规模、健康状态、监控目标、最近资产和快捷入口 |
| K8s 集群 / Rancher | 集群入口、Rancher 跳转、集群同步和 K8s 资源浏览 |
| 中间件 | Kafka、Redis、Nginx 等实例与集群信息 |
| 数据库 | MySQL、MongoDB、PostgreSQL、Redis 等实例信息 |
| 网络设备 | 交换机、路由器、防火墙、SNMP 和端口信息 |
| 关系拓扑 | 应用、中间件、数据库、主机和网络设备关系 |
| 监控目标 | Exporter 地址、采集状态和 Prometheus 联动 |
| 系统配置 | 模型、数据源、导入导出、机房和权限 |

## 3. 数据来源

- Agent / SSH：主机、操作系统、网卡和基础运行信息。
- Rancher：K8s 集群及后续节点、命名空间、工作负载元数据。
- SNMP：网络设备、接口、VLAN、LLDP 邻居。
- 手工 / CSV：机房位置、负责人、项目组、中间件和数据库补充信息。
- Prometheus 回写：只更新资产健康和告警状态，不改变资产主数据。

## 4. 中间件和数据库字段

中间件字段：类型、集群名称、节点列表、监听端口、JVM 端口、数据目录、日志路径、进程名、启动用户、Exporter 地址和端口。

数据库字段：数据库类型、版本、实例角色、连接地址、监听端口、数据目录、日志路径、备份策略、集群名称、Exporter 地址和端口。

## 5. 监控联动

CMDB 向 Prometheus 提供 HTTP-SD：

```text
GET /api/v1/monitor/service-discovery/node-exporter
GET /api/v1/monitor/service-discovery/exporters
GET /api/v1/monitor/service-discovery/snmp
```

Prometheus 新增 `cmdb-exporter` job，读取中间件、数据库和主机 Exporter 目标。资产新增或变化后，Prometheus 在下一次 service discovery 刷新时自动跟进。

## 6. Rancher 集成

配置：

```text
RANCHER_URL=https://rancher.example.com
RANCHER_TOKEN=<api-token>
RANCHER_DASHBOARD_URL=https://rancher.example.com
RANCHER_INSECURE_SKIP_VERIFY=false
```

接口：

```text
GET  /api/v1/cmdb/rancher/config
GET  /api/v1/cmdb/rancher/clusters
POST /api/v1/cmdb/rancher/sync
```

当前实现支持 Rancher 集群列表、跳转入口和 K8s 集群资产同步。节点、命名空间和工作负载可继续复用现有 K8s 采集器，后续再按 Rancher API 扩展同步。