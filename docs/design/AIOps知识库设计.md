# AIOps 知识库设计

## 目标

知识库用于沉淀告警分析、故障复盘、排查步骤和 Runbook 经验，为后续 AI 检索、故障推荐和工单复盘提供可维护的数据源。

## 数据模型

`aiops_knowledge` 保存知识条目：

- 标题、分类、摘要、正文和标签。
- 草稿/发布状态。
- 来源类型和来源 ID，支持从 AIOps 分析自动生成草稿。
- 作者、创建时间、更新时间、浏览量和有帮助次数。

## 生命周期

```text
人工新建/历史内置知识
  → 草稿
  → 编辑和校验
  → 发布
  → 检索、浏览和点赞

AIOps 分析
  → 从分析生成知识草稿
  → 人工补充复盘结论
  → 发布并复用于后续故障
```

## API 与权限

- `GET /api/v1/aiops/knowledge`：普通用户只查询已发布知识，支持关键词搜索。
- `GET /api/v1/aiops/knowledge?all=1`：具备 `aiops:execute` 的用户可查看草稿和全部知识。
- `GET /api/v1/aiops/knowledge/{id}`：查看详情并增加浏览量。
- `POST /api/v1/aiops/knowledge`：新建知识，需要 `aiops:execute`。
- `PUT /api/v1/aiops/knowledge/{id}`：编辑知识，需要 `aiops:execute`。
- `DELETE /api/v1/aiops/knowledge/{id}`：删除知识，需要 `aiops:execute`。
- `POST /api/v1/aiops/knowledge/{id}/helpful`：标记有帮助。
- `POST /api/v1/aiops/knowledge/synthesize`：从分析记录生成知识草稿。

所有新增、编辑、删除和从分析沉淀操作都写入审计日志。

## 后续方向

- 将知识和工单、告警、CMDB 资产建立关联。
- 增加 Markdown 渲染、附件和历史版本。
- 接入向量化检索和 RAG，但敏感字段和凭据不进入向量库。

## 关联与检索增强

- 知识条目支持关联 CMDB 资产、工单和告警，服务端保存对象快照（类型、ID、名称、状态），避免查询时重复解析。
- 创建和更新知识时，服务端会根据 ID 从现有资产、工单和告警服务刷新名称和状态。
- 搜索采用分层评分：标题、分类、标签、摘要、正文按权重排序，再按有帮助次数和更新时间排序。
- AIOps 分析生成后会根据根因关键词匹配最多 3 条已发布知识，作为排查和复盘建议。
- 当前检索是本地可解释评分，后续可在不改变知识模型的前提下增加 pgvector 向量召回和 RAG 重排。
