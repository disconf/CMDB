# CMDB 资产模块优化设计

> 状态：**P0/P1/P2 已实施，P3 待实施**。
> 日期：2026-10-16（2026-10-19 更新实施状态）

## 实施状态

| 阶段 | 状态 | 说明 |
| --- | --- | --- |
| P0 模型停用过滤、编辑/删除、字段配置 | 已完成 | 停用模型资产全局隐藏，模型可编辑、删除空模型，支持按模型新增资产 |
| P1 项目组实体与下拉选择 | 已完成 | 新增项目组管理、未纳管项目组归集和资产筛选 |
| P2 中间件/数据库模型与字段模板 | 已完成主体 | 已有模型字段模板、模型驱动资产页和监控联动，后续按实际数据继续补充字段 |
| P3 K8s 集群容量 | 待实施 | 尚未接入 kube-state-metrics，容量卡片和预留资源口径仍需实现 |

## 0. 现状核对（基于代码事实）

| 事项 | 现状 | 结论 |
| --- | --- | --- |
| CI 模型自定义字段 | `Model{Code,Name,Category,Icon,Count,Description,Enabled,Fields[]}`，`ModelField{Name,Label,Type,Required,DefaultValue,Options}`，支持 text/number/boolean/date/select | **已具备模型驱动能力**，中间件/数据库的字段用模型配置即可，不需要改代码 |
| 模型停用 | `ToggleModel` 只改 `cmdb_models.enabled` 并落库 | **`ListAssets` 完全没有读 enabled**，所以停用后资产照旧显示 → 需要改 |
| 模型编辑/删除 | 后端有 `UpdateModel`、`ToggleModel`，**没有 DeleteModel** | 需要补删除能力 + 前端入口 |
| 项目组 | 资产 `ProjectGroup` 只是一个自由文本字段，没有独立实体 | 需要新增项目组实体与下拉选择 |
| K8s 资产 | Rancher 同步把集群/节点/命名空间/工作负载全部写成资产（当前命名空间 56 条、Pod 129 条） | 命名空间这类派生资源需要从资产清单里隐去 |
| K8s 容量 | 目前只有节点/工作负载清单，没有容量与使用率 | 需要接 Prometheus 指标 |
| 中间件/数据库 | 有中间件自动发现写入的资产，字段靠 `Attributes` 键值对，没有统一的字段模板 | 需要定义两个模型 + 默认字段模板 |

---

## 1. 资产清单

### 1.1 停用模型后不再显示其资产

**问题**：模型管理里点"停用"，资产清单里该模型的资产依然显示。

**设计**
- 后端统一过滤：`ListAssets`/`Summary`/`Analytics`/`MonitorTargets`/`PrometheusTargetGroups` 等所有按资产列表输出的地方，先取"启用模型 code 集合"，再过滤 `asset.Type`。
- 前端：资产清单、资产总览统计同步过滤；当存在被隐藏的资产时，在清单顶部给一条提示
  > 「已按模型配置隐藏 N 个停用模型的资产 · 查看模型管理」
- 停用操作加二次确认：提示"停用后该模型下 N 个资产将从清单与监控目标中隐藏"。
- 反向：启用后自动恢复显示与监控目标，不需要重建。

**待确认**：停用是"全局隐藏"还是"仅资产清单隐藏"？推荐**全局隐藏**（含监控目标），语义最清晰。

### 1.2 CI 模型管理补全编辑与删除

**设计**
- 列表每行操作：`编辑` `字段配置` `停用/启用` `删除`
- 字段配置：增删字段、改标签/类型/是否必填/默认值/下拉选项，支持拖拽排序
- 删除规则（重要）：
  - 模型下资产数为 0 → 允许删除，二次确认
  - 模型下还有资产 → 禁止删除，提示"该模型下还有 N 个资产，请先迁移或删除资产"
  - 内置模型（服务器/K8s/网络设备/中间件/数据库）标记 `内置`，只允许停用不允许删除
- 新增后端 `DELETE /api/v1/cmdb/models/{code}`
- 所有模型变更写审计（谁在什么时候改了字段/停用/删除）

### 1.3 批量配置增强

现有批量配置支持宿主机、环境等。本次增加：
- 项目组（下拉，来自项目组实体）
- 维护人 / 维护人手机号
- 资产类型重分类（物理机/虚拟机互转）

---

## 2. 项目组管理

### 2.1 数据模型

新增 `cmdb_project_groups`：

| 字段 | 说明 |
| --- | --- |
| code | 唯一标识，如 `core-system` |
| name | 显示名，如 `核心系统组` |
| description | 描述 |
| owner / owner_phone | 负责人、联系电话 |
| enabled | 是否启用（停用的不再出现在下拉里，但历史资产保留原值） |
| sort_order | 排序 |
| created_by / created_at / updated_at | 审计字段 |

### 2.2 接口

```
GET    /api/v1/cmdb/project-groups          列表（含每个项目组的资产数）
POST   /api/v1/cmdb/project-groups          新建
PUT    /api/v1/cmdb/project-groups/{code}   编辑
DELETE /api/v1/cmdb/project-groups/{code}   删除（有资产时禁止）
```

### 2.3 前端

- 管理入口：**CMDB 资产 → 系统配置 → 项目组管理**（与模型管理并列）
- 交互：表格 + 新建/编辑弹窗；每行显示"资产数"，点击可跳转到已筛选的资产清单
- 选择入口：资产编辑、批量配置、新建资产、任务目标选择里的项目组，全部改成**可搜索下拉 + 就地新建**
- 存量数据：首次上线时扫描现有资产的项目组取值，列出"未纳管"的值，提供一键纳管（批量建为项目组）

---

## 3. K8s 集群模块

### 3.1 命名空间不再作为资产

- `k8s-namespace/*` 不再出现在资产清单与资产总览统计里（仍保留在数据库，作为集群详情的下钻维度）
- 同样处理的派生资产：`k8s-service`、`k8s-pod` 等在工作负载页签内展示，不占用资产清单
- 资产清单里 K8s 只保留"集群"这一级（`k8s-cluster/*`）

### 3.2 集群详情（数据来自 Rancher + Prometheus）

**基本信息**（Rancher API，已有同步链路）
- 集群名称、ID、K8s 版本、状态、创建时间
- 节点数量（按角色：control-plane / worker）
- 集群资源总额：CPU 核数、内存总量、Pod 容量
- 「打开 Rancher」按钮，跳转到对应集群

**容量卡片**

| 指标 | 口径 | 数据来源 |
| --- | --- | --- |
| Pod 数量 / 使用率 | `count(kube_pod_info)` ÷ Pod 容量 | Prometheus |
| CPU 已预留 / 占比 | `sum(kube_pod_container_resource_requests{resource="cpu"})` ÷ 总核数 | Prometheus |
| 内存已预留 / 占比 | `sum(kube_pod_container_resource_requests{resource="memory"})` ÷ 总内存 | Prometheus |
| CPU 实际使用率 | `1 - avg(rate(node_cpu_seconds_total{mode="idle"}[5m]))` | Prometheus（node-exporter） |
| 内存实际使用率 | node-exporter 内存指标 | Prometheus |

> 前置检查：Prometheus 是否已抓取 `kube-state-metrics`（`kube_pod_info`、`kube_pod_container_resource_requests`）。
> 当前抓取配置里没有 kube-state-metrics job，**需要先补上**（部署 kube-state-metrics + 加 job），否则预留类指标取不到。

**页签**：概览 / 节点 / 工作负载 / 服务 / 存储 / 事件
- 命名空间用**下拉多选**筛选（选中打勾），不默认展开
- 工作负载显示类型、副本数、状态；Pod 显示重启次数、节点、镜像

### 3.3 人工兜底

Prometheus 不可用时，容量卡片降级为"数据不可用"而不是显示 0，避免误判。

---

## 4. 中间件

### 4.1 CI 模型 `middleware` 与默认字段模板

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| cluster_name | text | 集群名称 |
| cluster_nodes | number | 集群节点数量 |
| deploy_mode | select | 单机 / 集群 |
| version | text | 版本号 |
| process_name | text | 进程名 |
| run_user | text | 启动用户 |
| ports | text | 端口信息（支持多个，逗号分隔） |
| data_path | text | 数据目录 |
| log_path | text | 日志目录 |
| config_path | text | 配置文件路径 |
| start_command | text | 启动命令 |
| stop_command | text | 关闭命令 |
| maintainer | text | 维护人 |
| maintainer_phone | text | 维护人手机号 |

固定字段（所有模型共有）：名称、类型、状态、IP、环境、**项目组（下拉）**、位置

### 4.2 关联关系

- `runs-on` → 关联部署的服务器（可多台，选中即建立关系）
- 资产详情里直接显示"所在机房 / 机柜 / U 位"（由关联服务器推导）
- 项目组 → 下拉选择

### 4.3 列表与详情

- 列表：按类型分组（Kafka / Redis / Nginx / Zookeeper …），列：实例名、模式、规模、部署位置、端口、状态、项目组、维护人
- 详情：基础信息 / 网络与端口 / 存储路径 / 启停命令（只读展示，可复制）/ 关联服务器 / 监控联动（一键注册到 Prometheus）
- 自动发现继续保留：能自动填的字段自动填，手工新增走模型表单

---

## 5. 数据库

### 5.1 CI 模型 `database` 与默认字段模板

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| db_type | select | MySQL / MongoDB / PostgreSQL / ClickHouse / Redis / 其他 |
| version | text | 版本号 |
| instance_name | text | 实例名 |
| ports | number | 端口 |
| deploy_mode | select | 单机 / 主从 / 集群 / 分片 |
| role | select | 主 / 从 / 仲裁 |
| data_path | text | 数据存储位置 |
| config_path | text | 配置文件路径 |
| log_path | text | 日志目录 |
| start_command | text | 启动命令 |
| stop_command | text | 关闭命令 |
| charset / timezone | text | 字符集、时区 |
| backup_policy | text | 备份策略 |
| maintainer / maintainer_phone | text | 负责人与电话 |

### 5.2 复用设计

中间件与数据库共用同一套"服务型资产"页面与详情布局（差异只由模型字段驱动），避免两套前端。

- 列表：按 `db_type` 分组，列：实例名、类型、版本、部署位置、端口、角色、状态、项目组
- 详情：连接信息（仅展示主机端口，不存密码）/ 存储路径 / 启停命令 / 关联服务器 / 监控联动（自动生成 `mysqld_exporter`、`redis_exporter` 等目标）

---

## 6. 前端信息架构调整

CMDB 资产二级导航建议：

```
资产总览
服务器            （物理机 / 虚拟机合并，可按类型筛选）
K8s 集群          （集群 → 概览/节点/工作负载/服务/存储）
中间件            （按类型分组）
数据库            （按类型分组）
网络设备
资产清单          （全量，可按模型筛选，含被停用模型的提示）
责任治理
关系拓扑
监控联动
系统配置          （模型管理 / 项目组管理 / 批量导入 / 采集凭据）
```

要点
- 现有「数据报表」「模型管理」「批量导入」从页面顶部按钮收敛进"系统配置"
- 服务器菜单下不再显示 K8s Pod（Pod 只属于 K8s 集群视图）
- 页面自适应：表格在窄屏下改为卡片列表，避免横向滚动

---

## 7. 实施顺序

| 阶段 | 内容 | 预估 |
| --- | --- | --- |
| P0 | 模型停用过滤 + 模型管理编辑/删除/字段配置 | 1 天 |
| P1 | 项目组实体 + 下拉 + 存量归集 | 1.5 天 |
| P2 | 中间件 / 数据库模型与共用页面 | 2 天 |
| P3 | K8s 集群详情 + 容量（含 kube-state-metrics 接入） | 2–3 天 |

每个阶段都会补单元测试，并在改动后跑一次全站页面巡检（无空白页、无失败接口）。

---

## 8. 需要你确认的点

1. **停用模型的语义**：全局隐藏（含监控目标）还是仅资产清单隐藏？推荐全局隐藏。
2. **项目组管理入口**：放在 CMDB 资产的"系统配置"里，还是放到一级菜单"系统管理"？
3. **字段清单**：中间件、数据库的字段是否按上面定型？还需要增删哪些（例如"所属应用""SLA 等级"）？
4. **K8s 容量口径**：以 Prometheus 的 requests（预留）为准是否可以？还是需要同时展示 Rancher 里的分配值？
5. **模型删除**：只允许删除"空模型"，还是希望支持"归档"（数据保留但不再展示）？
6. **数据库/中间件是否登记密码**：建议不存明文，需要时通过 Vault 引用（当前凭据中心的做法）。
