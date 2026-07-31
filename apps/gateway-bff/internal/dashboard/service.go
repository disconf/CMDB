package dashboard

import "time"

type Service struct{}

func NewService() *Service { return &Service{} }

func (s *Service) Overview() Overview {
	return Overview{
		UpdatedAt: time.Now(),
		Metrics:   Metrics{AssetTotal: 4286, OnlineAgents: 1024, ActiveAlerts: 37, TodayJobs: 186},
		Capacity:  []CapacityItem{{"CPU 使用率", 63.2, "1012 / 1600 Core"}, {"内存使用率", 71.4, "914.3 / 1280 GB"}, {"存储使用率", 58.7, "58.7 / 100 TB"}, {"网络带宽", 32.6, "3.26 / 10 Gbps"}},
		Health:    []HealthItem{{"计算资源", 92}, {"存储资源", 88}, {"网络资源", 95}, {"中间件", 90}},
		Topology: Topology{Layers: []TopologyLayer{
			{"项目", []TopologyNode{{"project-ecom", "电商平台", "project", "healthy"}, {"project-fin", "金融核心", "project", "healthy"}, {"project-data", "数据中台", "project", "warning"}}},
			{"应用", []TopologyNode{{"app-user", "用户中心", "app", "healthy"}, {"app-order", "订单中心", "app", "healthy"}, {"app-pay", "支付中心", "app", "warning"}, {"app-data", "数据分析", "app", "healthy"}}},
			{"服务", []TopologyNode{{"svc-user", "用户服务", "service", "healthy"}, {"svc-order", "订单服务", "service", "healthy"}, {"svc-pay", "支付服务", "service", "critical"}, {"svc-stock", "库存服务", "service", "healthy"}}},
			{"实例", []TopologyNode{{"ins-vm", "云主机 286", "instance", "healthy"}, {"ins-pod", "Pod 1,842", "instance", "warning"}, {"ins-db", "数据库 76", "instance", "critical"}, {"ins-cache", "缓存 48", "instance", "healthy"}}},
			{"基础设施", []TopologyNode{{"infra-phy", "物理机集群", "infra", "healthy"}, {"infra-cloud", "私有云", "infra", "healthy"}, {"infra-k8s", "Kubernetes", "infra", "warning"}, {"infra-net", "网络设备", "infra", "healthy"}}},
		}},
		AlertTrend:  []TrendPoint{{"00:00", 34, 8}, {"02:00", 48, 12}, {"04:00", 57, 14}, {"06:00", 72, 18}, {"08:00", 63, 15}, {"10:00", 88, 21}, {"12:00", 51, 12}, {"14:00", 62, 14}, {"16:00", 47, 11}, {"18:00", 58, 13}, {"20:00", 44, 9}, {"22:00", 37, 8}},
		Alerts:      []Alert{{"ALT-301", "critical", "数据库连接数过高", "db-prod-01", "10:28:31"}, {"ALT-302", "critical", "支付服务响应超时", "pay-service-02", "10:27:12"}, {"ALT-303", "high", "磁盘使用率超阈值", "vm-ecs-192", "10:25:44"}, {"ALT-304", "high", "K8s Pod 重启频繁", "pod-user-7f8d", "10:24:18"}},
		Jobs:        []Job{{"JOB-101", "数据库备份任务", "备份", 75, "running", "张三"}, {"JOB-102", "安全漏洞扫描", "巡检", 45, "running", "李四"}, {"JOB-103", "日志清理任务", "维护", 90, "running", "王五"}, {"JOB-104", "配置合规检查", "巡检", 20, "queued", "赵六"}},
		Timeline:    []TimelineEvent{{"EVT-1", "10:28:31", "alert", "告警触发：数据库连接数过高", "告警中心"}, {"EVT-2", "10:24:18", "alert", "告警触发：K8s Pod 重启频繁", "告警中心"}, {"EVT-3", "10:15:42", "job", "任务完成：配置合规检查", "任务中心"}, {"EVT-4", "10:12:07", "deploy", "发布成功：订单服务 v2.3.1", "发布管理"}},
		Deployments: []Deployment{{"订单服务", "v2.3.1", "success", "10:12"}, {"用户中心", "v1.8.4", "running", "10:06"}, {"推荐服务", "v3.0.0", "failed", "09:48"}},
	}
}
