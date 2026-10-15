# Neo4j 与 OpenSearch 部署

拓扑投影与全文检索依赖两个独立组件：

| 组件 | 作用 | 命名空间 | 端口 |
| --- | --- | --- | --- |
| Neo4j | 资产关系图（`(:Asset)-[:RELATED]->(:Asset)`），支撑拓扑与影响面分析 | `ops-qa` | 7474 / 7687 |
| OpenSearch | 资产全文检索索引 `cmdb-assets` | `ops-qa` | 9200 / 9300 |

CMDB 依然是唯一底账：PostgreSQL 保存主数据，网关把资产投影到 Neo4j 与 OpenSearch，两者都是可重建的副本。

## 1. 镜像入库

Harbor 项目 `cmdb` 中保存以下镜像：

```
harbor.qa.bcavt.com/cmdb/neo4j:5.26-community
harbor.qa.bcavt.com/cmdb/opensearch:2.19.1
```

从可联网节点导入并推送（示例在 `node01` 上执行）：

```bash
nerdctl -n k8s.io pull docker.m.daocloud.io/library/neo4j:5.26-community
nerdctl -n k8s.io tag  docker.m.daocloud.io/library/neo4j:5.26-community harbor.qa.bcavt.com/cmdb/neo4j:5.26-community
nerdctl login -u admin -p '******' harbor.qa.bcavt.com
nerdctl -n k8s.io push harbor.qa.bcavt.com/cmdb/neo4j:5.26-community

nerdctl -n k8s.io pull hub.rat.dev/opensearchproject/opensearch:2.19.1
nerdctl -n k8s.io tag  hub.rat.dev/opensearchproject/opensearch:2.19.1 harbor.qa.bcavt.com/cmdb/opensearch:2.19.1
nerdctl -n k8s.io push harbor.qa.bcavt.com/cmdb/opensearch:2.19.1
```

推送完成后 Harbor 可看到制品大小（Neo4j 约 335MiB，OpenSearch 约 981MiB）。

## 2. 部署

```bash
kubectl apply -f deploy/k8s/neo4j-opensearch.yaml
kubectl -n ops-qa rollout status statefulset/neo4j
kubectl -n ops-qa rollout status statefulset/opensearch
```

清单要点：

- 两者都使用 Longhorn 持久卷（Neo4j `10Gi` 数据 + `2Gi` 日志，OpenSearch `20Gi`）。
- `enableServiceLinks: false`：Kubernetes 注入的 Service 环境变量会被 Neo4j 当作配置项解析，导致启动失败。
- OpenSearch 需要 `fsGroup/runAsUser/runAsGroup: 1000`，否则 Longhorn 卷属主不匹配会导致容器无法写入。
- OpenSearch 单节点部署使用 `discovery.type=single-node` 并关闭安全插件；索引副本数设置为 `0`（单节点下 `1` 副本会让索引长期处于 yellow）。
- 两者都带读写/liveness 探针，镜像来自 Harbor 并使用 `cmdb-registry` 拉取密钥。

## 3. 网关注入

网关 Deployment 需要以下环境变量（`ops-qa` 已配置）：

```
NEO4J_URL=http://neo4j.ops-qa.svc.cluster.local:7474
NEO4J_USERNAME=neo4j
NEO4J_PASSWORD=ops_platform_neo4j
OPENSEARCH_URL=http://opensearch.ops-qa.svc.cluster.local:9200
```

## 4. 重建投影与验证

```bash
# 管理员令牌
curl -X POST http://172.28.69.164:30080/api/v1/system/projections/rebuild -H "Authorization: Bearer $TOKEN"
# => {"assets":567,"neo4j":567,"openSearch":567}

curl -H "Authorization: Bearer $TOKEN" http://172.28.69.164:30080/api/v1/system/projections
# => {"neo4jConfigured":true,"openSearchConfigured":true,"neo4jAssets":567,"openSearchAssets":567}

curl -H "Authorization: Bearer $TOKEN" http://172.28.69.164:30080/api/v1/system/production-readiness
# Neo4j / OpenSearch 两项均为 passed（HTTP 200）

# OpenSearch
curl 'http://opensearch:9200/_cat/indices/cmdb-assets?v'
curl 'http://opensearch:9200/cmdb-assets/_count'

# Neo4j
kubectl -n ops-qa exec neo4j-0 -- cypher-shell -u neo4j -p ops_platform_neo4j "MATCH (a:Asset) RETURN count(a);"
```

## 5. 投影实现说明

- **OpenSearch 使用 `_bulk` 写入**。资产 ID 形如 `k8s-pod/ops-qa/gateway-xxxx`，直接拼进 URL 路径会被 OpenSearch 解析成多级路径并返回 `400 Bad Request`；批量接口把 ID 放在请求体里，既保留原始 ID，也把 567 次往返压缩成几次。
- **Neo4j 使用 `UNWIND` 批量事务**。唯一约束 `asset_id_unique` 需要在独立事务中创建，与写事务混在一起会触发 `Neo.ClientError.Transaction.ForbiddenDueToTransactionType`。
- 每次全量重建会先清空 `:RELATED` 关系再按当前关系数据重建，保证不再存在的历史连线被清理。
- 写入错误会把 OpenSearch/Neo4j 的原始响应正文带回接口，便于定位。

## 6. 故障排查

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| 重建返回 `opensearch returned 400 Bad Request` | 资产 ID 含 `/` 直接拼 URL | 升级到使用 `_bulk` 的网关版本 |
| 重建返回 `ForbiddenDueToTransactionType` | 约束与写语句在同一事务 | 约束单独提交 |
| Neo4j Pod `CrashLoopBackOff` | Service 环境变量被解析成配置 | 设置 `enableServiceLinks: false` |
| OpenSearch 启动报权限错误 | Longhorn 卷属主为 root | 设置 `fsGroup/runAsUser/runAsGroup: 1000` |
| 索引健康状态 yellow | 单节点 + 1 副本 | `PUT /cmdb-assets/_settings {"index":{"number_of_replicas":0}}` |
