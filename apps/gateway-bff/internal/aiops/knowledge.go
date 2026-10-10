package aiops

func defaultKnowledge() []Knowledge {
	return []Knowledge{
		{ID: "kb-001", Title: "Java 服务 OOM 排查手册", Category: "故障处理", Summary: "从堆转储、GC 日志和容器限制定位内存异常", Content: "确认内存增长曲线 → 采集堆和 GC 证据 → 检查容器 limit 与近发布 → 审批后缓解。", Tags: []string{"OOM", "Java", "内存"}, Helpful: 48, Status: "published", Source: "builtin", Author: "system", CreatedAt: "2026-07-01 10:00:00", UpdatedAt: "2026-07-01 10:00:00"},
		{ID: "kb-002", Title: "数据库连接池耗尽处理", Category: "数据库", Summary: "识别慢查询与连接泄漏并安全恢复", Content: "检查连接数、等待事件、慢查询和连接池配置，禁止直接 Kill 未知会话。", Tags: []string{"MySQL", "PostgreSQL", "连接池", "延迟"}, Helpful: 36, Status: "published", Source: "builtin", Author: "system", CreatedAt: "2026-07-01 10:00:00", UpdatedAt: "2026-07-01 10:00:00"},
		{ID: "kb-003", Title: "Kubernetes Pod 重启诊断", Category: "Kubernetes", Summary: "分析 OOMKilled、探针失败和节点驱逐", Content: "读取 Pod 状态、事件、上次退出原因和容器限制，再关联节点和存储。", Tags: []string{"Pod", "OOM", "K8s"}, Helpful: 29, Status: "published", Source: "builtin", Author: "system", CreatedAt: "2026-07-01 10:00:00", UpdatedAt: "2026-07-01 10:00:00"},
		{ID: "kb-004", Title: "网络接口 Down 处置", Category: "网络", Summary: "确认物理链路、SFP、聚合端口和 VLAN 状态", Content: "先核对接口和聚合端口，再检查光模块、MAC、VLAN 与流量计数，变更前审批。", Tags: []string{"交换机", "接口", "SNMP", "链路"}, Helpful: 21, Status: "published", Source: "builtin", Author: "system", CreatedAt: "2026-07-01 10:00:00", UpdatedAt: "2026-07-01 10:00:00"},
		{ID: "kb-005", Title: "Linux 磁盘空间清理", Category: "主机", Summary: "定位大目录、日志增长和 inode 使用异常", Content: "定位增长目录和文件句柄，核对保留策略与备份后再清理。", Tags: []string{"磁盘", "Linux", "日志"}, Helpful: 44, Status: "published", Source: "builtin", Author: "system", CreatedAt: "2026-07-01 10:00:00", UpdatedAt: "2026-07-01 10:00:00"},
	}
}

func recommendationFor(rootCause string) []Recommendation {
	switch rootCause {
	case "内存资源压力":
		return []Recommendation{{RunbookID: "rb-memory-relief", Title: "内存压力只读排查与缓解", Risk: "medium", Impact: "先采集内存证据，确认后滚动重启或扩容", Duration: "约 5 分钟", Steps: []string{"确认告警持续时间和影响实例", "采集内存、进程和容器指标", "检查近期发布和配置变更", "由人工审批后执行单实例滚动缓解", "持续观察内存与业务指标"}}}
	case "磁盘空间耗尽风险":
		return []Recommendation{{RunbookID: "rb-disk-cleanup", Title: "磁盘空间治理", Risk: "medium", Impact: "定位增长目录，清理前必须确认文件归属", Duration: "约 8 分钟", Steps: []string{"查询分区使用率和 inode", "定位增长最快目录与日志文件", "核对备份与保留策略", "人工审批后清理可回收文件", "复核空间释放和业务状态"}}}
	case "服务延迟异常":
		return []Recommendation{{RunbookID: "rb-latency-check", Title: "服务延迟链路排查", Risk: "low", Impact: "仅执行只读链路与指标检查", Duration: "约 4 分钟", Steps: []string{"确认 P95/P99 延迟和请求量", "检查上游、下游和数据库延迟", "关联近期发布和配置变更", "根据结论申请限流、回滚或扩容", "观察恢复并记录故障结论"}}}
	case "网络链路异常":
		return []Recommendation{{RunbookID: "rb-network-path", Title: "网络链路与端口排查", Risk: "medium", Impact: "先验证链路和端口，变更网络配置必须审批", Duration: "约 6 分钟", Steps: []string{"确认接口和聚合端口状态", "检查 MAC、VLAN 和流量计数", "执行 TCP、DNS、HTTP 只读诊断", "根据结论申请链路切换或端口修复", "复核业务恢复情况"}}}
	case "Kubernetes 工作负载异常":
		return []Recommendation{{RunbookID: "rb-pod-diagnose", Title: "Pod 异常诊断与恢复", Risk: "medium", Impact: "先查看事件、日志和资源限制", Duration: "约 5 分钟", Steps: []string{"读取 Pod 状态、事件和最近日志", "检查探针、镜像和资源限制", "确认节点和存储状态", "人工审批后执行重启或回滚", "验证副本就绪和业务流量"}}}
	case "数据库连接或慢查询压力":
		return []Recommendation{{RunbookID: "rb-db-slow-query", Title: "数据库连接与慢查询治理", Risk: "medium", Impact: "只读采集连接和慢查询证据，禁止直接终止未知会话", Duration: "约 6 分钟", Steps: []string{"检查连接数、等待事件和慢查询", "关联应用流量与近期发布", "确认连接池和 SQL 变更", "人工审批后处理异常会话", "复核连接池和延迟"}}}
	default:
		return []Recommendation{{RunbookID: "rb-generic-triage", Title: "通用故障只读排查", Risk: "low", Impact: "仅执行只读证据采集", Duration: "约 4 分钟", Steps: []string{"确认告警持续时间和影响范围", "关联资产、拓扑和近期变更", "执行只读指标与日志检查", "形成根因假设和人工审核建议", "记录结论并持续观察"}}}
	}
}
