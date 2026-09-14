# 远程运维多实例协同设计

## 目标

- 远程会话可在任意网关 Pod 首次接入，不要求浏览器一直命中同一副本。
- 同一会话的终端输入、输出、窗口缩放、关闭和在线人数在副本间保持一致。
- 实际 SSH 连接始终由租约 owner 持有；owner 异常退出时会话自动中断并关闭代理端。
- 会话、命令、审批和录像仍以 PostgreSQL 为审计事实来源。

## 组件

| 组件 | 职责 |
|---|---|
| Gateway Pod | 处理 API、WebSocket、终端代理、SSH 连接和会话状态维护 |
| Redis Cluster | 跨 Pod 发布/订阅终端事件、在线状态和事件序号 |
| PostgreSQL | 保存会话、租约、终端事件、审批和归档状态 |
| Web Portal | 独立远程运维工作台、会话状态、终端与文件传输入口 |

## 会话接入流程

1. 用户创建远程会话。此时会话为 `active`，owner 为空。
2. 第一次打开 WebSocket 时，Gateway 使用数据库条件更新抢占会话租约。
3. 抢占成功的 Pod 成为 owner，建立真实 SSH/PTY 连接，并周期性续租。
4. 其他 Pod 接入同一会话时订阅 Redis output channel，输入和 resize 通过 control channel 转发给 owner。
5. 最后一个本地或远程客户端断开后，owner 在确认在线人数为 0 时关闭 SSH 连接。
6. owner 租约过期或 Pod 退出时，后台任务把会话标记为 `interrupted`，代理端收到 `owner_offline` 并关闭。

## Redis 键设计

同会话键使用 hash tag，确保 Cluster 下位于同一 slot：

```text
cmdb:remote:terminal:{sessionID}:control
cmdb:remote:terminal:{sessionID}:output
cmdb:remote:terminal:{sessionID}:presence
cmdb:remote:terminal:{sessionID}:sequence
```

- `control`：输入、resize、主动关闭。
- `output`：终端输出和 owner 离线通知。
- `presence`：参与者心跳，45 秒过期，用于统计 online/controller。
- `sequence`：会话内单调递增，写入 `remote_terminal_events` 时用于幂等去重。

## 环境变量

```text
REMOTE_TERMINAL_REDIS_URL=redis://:PASSWORD@redis-cluster-leader.ops-qa.svc.cluster.local:6379/0
REDIS_CLUSTER_MODE=true
REMOTE_TERMINAL_BUS=enabled
REMOTE_SESSION_LEASE_TTL=45s
GATEWAY_INSTANCE_ID=<pod-name>
```

认证缓存可能使用单节点 Redis Client，因此不要把全局 `REDIS_URL` 直接改为 Cluster Service；远程终端总线使用独立的 `REMOTE_TERMINAL_REDIS_URL`。

## 故障模型

| 场景 | 行为 |
|---|---|
| Gateway A 持有 SSH，客户端下一次命中 Gateway B | B 通过 Redis 代理输入输出，不迁移 SSH 连接 |
| Redis 短暂不可用 | owner 保持本地终端；新跨实例接入会失败，页面提示重试 |
| owner Pod 正常退出 | 发布 owner_offline，数据库会话置为 interrupted |
| owner Pod 异常失联 | 租约到期后由存活 Pod 回收并中断会话 |
| 会话从未接入且已过期 | 后台任务直接关闭，避免长期占用并发配额 |
| 两个 Pod 同时抢租约 | 数据库条件更新只允许一个 Pod 成功，另一个转为代理 |

## 当前边界

- 本方案解决“任意副本接入”，不迁移已经建立的 SSH 连接；owner 故障后会话中断，用户需重新连接。
- Redis Pub/Sub 用于实时转发，不作为录像存储；录像仍写入 PostgreSQL。
- 文件上传下载接口由收到请求的 Pod 直接执行，后续可复用同一会话租约机制扩展为跨实例文件代理。

## 验证要点

- Gateway 至少 2 副本，Pod 均 Running。
- Redis 中存在 `cmdb:remote:terminal:{...}:*` 键或频道订阅。
- 同一会话分别从两个副本打开 WebSocket，输入和输出一致。
- 删除 owner Pod 后，会话状态为 `interrupted`，代理端收到关闭。
- 会话页面显示 owner、心跳时间、租约剩余时间和跨实例状态。
