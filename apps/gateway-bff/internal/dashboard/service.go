package dashboard

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/demo"
	"cmdb/gateway-bff/internal/discovery"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
)

type Service struct {
	cmdb      *cmdb.Service
	discovery *discovery.Service
	monitor   *monitor.Service
	jobs      *jobs.Service
	audit     *audit.Service
}

// SetActivitySources wires the job and audit services used by the activity and
// timeline panels. They are optional: without them those panels stay empty.
func (s *Service) SetActivitySources(jobsService *jobs.Service, auditService *audit.Service) {
	s.jobs = jobsService
	s.audit = auditService
}

func NewService() *Service { return &Service{} }

func NewServiceWithRuntime(cmdbService *cmdb.Service, discoveryService *discovery.Service, monitorService *monitor.Service) *Service {
	return &Service{cmdb: cmdbService, discovery: discoveryService, monitor: monitorService}
}

func (s *Service) Overview() Overview {
	if s.cmdb != nil && s.discovery != nil && s.monitor != nil {
		return s.liveOverview()
	}
	if !demo.Enabled() {
		return Overview{UpdatedAt: time.Now(), Capacity: []CapacityItem{}, Health: []HealthItem{}, Topology: Topology{Layers: []TopologyLayer{}}, AlertTrend: []TrendPoint{}, Alerts: []Alert{}, Jobs: []Job{}, Timeline: []TimelineEvent{}, Deployments: []Deployment{}}
	}
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

// liveOverview builds the dashboard from real platform data: CMDB inventory,
// Prometheus-backed alerts, job executions and audit records. Every panel is
// derived from data the platform actually owns instead of a static demo sample.
func (s *Service) liveOverview() Overview {
	now := time.Now()
	assets := s.cmdb.InventoryAssets()
	alerts := s.monitor.Alerts()
	return Overview{
		UpdatedAt:   now,
		Metrics:     Metrics{AssetTotal: s.cmdb.Summary().Total, OnlineAgents: s.discovery.Summary().Online, ActiveAlerts: s.monitor.ActiveCount(), TodayJobs: s.todayJobs(now)},
		Capacity:    buildCapacity(assets),
		Health:      buildHealth(assets),
		Topology:    buildTopology(assets),
		AlertTrend:  buildAlertTrend(alerts, now),
		Alerts:      buildAlertList(alerts, 10),
		Jobs:        s.activeJobs(),
		Timeline:    s.recentTimeline(8),
		Deployments: []Deployment{},
	}
}

type assetGroup struct {
	name  string
	match func(string) bool
}

func containsAny(value string, needles ...string) bool {
	lowered := strings.ToLower(value)
	for _, needle := range needles {
		if strings.Contains(lowered, needle) {
			return true
		}
	}
	return false
}

var capacityGroups = []assetGroup{
	{name: "服务器节点", match: func(code string) bool {
		return code == "physical-server" || code == "virtual-machine" || code == "k8s-node"
	}},
	{name: "K8s 工作负载", match: func(code string) bool {
		return strings.HasPrefix(code, "k8s-") && code != "k8s-node"
	}},
	{name: "网络设备", match: func(code string) bool {
		return containsAny(code, "network-device", "switch", "router", "firewall", "loadbalancer")
	}},
	{name: "中间件与数据库", match: func(code string) bool {
		return containsAny(code, "middleware", "database", "redis", "kafka", "postgres", "mysql", "mongo", "elastic")
	}},
}

func buildCapacity(assets []cmdb.Asset) []CapacityItem {
	items := make([]CapacityItem, 0, len(capacityGroups))
	total := len(assets)
	for _, group := range capacityGroups {
		count := 0
		for _, asset := range assets {
			if group.match(asset.Type) {
				count++
			}
		}
		share := 0.0
		if total > 0 {
			share = float64(count) * 100 / float64(total)
		}
		items = append(items, CapacityItem{Name: group.name, Value: float64(int(share*10+0.5)) / 10, Detail: fmt.Sprintf("%d / %d 资产", count, total)})
	}
	return items
}

func buildHealth(assets []cmdb.Asset) []HealthItem {
	items := make([]HealthItem, 0, len(capacityGroups))
	for _, group := range capacityGroups {
		total, online := 0, 0
		for _, asset := range assets {
			if !group.match(asset.Type) {
				continue
			}
			total++
			if asset.Status == "online" {
				online++
			}
		}
		if total == 0 {
			continue
		}
		items = append(items, HealthItem{Name: group.name, Value: online * 100 / total})
	}
	return items
}

// buildTopology renders the inventory as a five layer breakdown: project group,
// asset type, environment, collection source and location.
func buildTopology(assets []cmdb.Asset) Topology {
	layers := []TopologyLayer{
		buildLayer("项目组", assets, func(asset cmdb.Asset) string { return asset.ProjectGroup }, "project", 6),
		buildLayer("资产类型", assets, func(asset cmdb.Asset) string { return asset.Type }, "app", 8),
		buildLayer("环境", assets, func(asset cmdb.Asset) string { return asset.Environment }, "service", 5),
		buildLayer("采集来源", assets, func(asset cmdb.Asset) string { return asset.Source }, "instance", 5),
		buildLayer("机房位置", assets, func(asset cmdb.Asset) string { return locationZone(asset.Location) }, "infra", 6),
	}
	return Topology{Layers: layers}
}

func locationZone(location string) string {
	trimmed := strings.TrimSpace(location)
	if trimmed == "" {
		return "未登记"
	}
	if index := strings.IndexAny(trimmed, "/｜|"); index > 0 {
		return trimmed[:index]
	}
	return trimmed
}

func buildLayer(name string, assets []cmdb.Asset, key func(cmdb.Asset) string, kind string, limit int) TopologyLayer {
	type bucket struct {
		total, online int
	}
	buckets := map[string]*bucket{}
	for _, asset := range assets {
		value := strings.TrimSpace(key(asset))
		if value == "" {
			value = "未分类"
		}
		entry, ok := buckets[value]
		if !ok {
			entry = &bucket{}
			buckets[value] = entry
		}
		entry.total++
		if asset.Status == "online" {
			entry.online++
		}
	}
	names := make([]string, 0, len(buckets))
	for value := range buckets {
		names = append(names, value)
	}
	sort.SliceStable(names, func(i, j int) bool {
		if buckets[names[i]].total != buckets[names[j]].total {
			return buckets[names[i]].total > buckets[names[j]].total
		}
		return names[i] < names[j]
	})
	nodes := make([]TopologyNode, 0, limit)
	for _, value := range names {
		if len(nodes) >= limit {
			break
		}
		entry := buckets[value]
		status := "healthy"
		switch {
		case entry.online*10 < entry.total*9:
			status = "warning"
		case entry.online*10 < entry.total*6:
			status = "critical"
		}
		nodes = append(nodes, TopologyNode{ID: kind + "-" + value, Name: fmt.Sprintf("%s %d", value, entry.total), Kind: kind, Status: status})
	}
	return TopologyLayer{Name: name, Nodes: nodes}
}

// buildAlertTrend buckets the last 24 hours of alerts into twelve samples.
func buildAlertTrend(alerts []monitor.Alert, now time.Time) []TrendPoint {
	const buckets = 12
	counts := make([]TrendPoint, buckets)
	window := 24 * time.Hour / buckets
	for index := 0; index < buckets; index++ {
		slot := now.Add(-time.Duration(buckets-1-index) * window)
		counts[index] = TrendPoint{Time: slot.Format("15:04")}
	}
	for _, item := range alerts {
		started, err := parseAlertTime(item.StartedAt)
		if err != nil {
			continue
		}
		age := now.Sub(started)
		if age < 0 || age >= 24*time.Hour {
			continue
		}
		index := buckets - 1 - int(age/window)
		if index < 0 {
			index = 0
		}
		if index >= buckets {
			index = buckets - 1
		}
		counts[index].Total++
		if strings.EqualFold(item.Severity, "critical") {
			counts[index].Critical++
		}
	}
	return counts
}

func parseAlertTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty alert time")
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", time.RFC3339} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised alert time %q", value)
}

func buildAlertList(alerts []monitor.Alert, limit int) []Alert {
	firing := make([]monitor.Alert, 0, len(alerts))
	for _, item := range alerts {
		if item.Status == "firing" {
			firing = append(firing, item)
		}
	}
	sort.SliceStable(firing, func(i, j int) bool {
		return severityRank(firing[i].Severity) > severityRank(firing[j].Severity)
	})
	out := make([]Alert, 0, limit)
	for _, item := range firing {
		if len(out) >= limit {
			break
		}
		out = append(out, Alert{ID: item.ID, Level: item.Severity, Title: item.Title, Target: item.Target, OccurredAt: item.StartedAt})
	}
	return out
}

func severityRank(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 3
	case "warning", "high":
		return 2
	default:
		return 1
	}
}

func (s *Service) todayJobs(now time.Time) int {
	if s.jobs == nil {
		return 0
	}
	prefix := now.Format("2006-01-02")
	count := 0
	for _, job := range s.jobs.Jobs() {
		if strings.HasPrefix(strings.TrimSpace(job.StartedAt), prefix) {
			count++
		}
	}
	return count
}

func (s *Service) activeJobs() []Job {
	if s.jobs == nil {
		return []Job{}
	}
	out := make([]Job, 0, 8)
	for _, item := range s.jobs.Jobs() {
		switch item.Status {
		case "running", "queued", "pending", "waiting-approval":
		default:
			continue
		}
		kind := item.TemplateID
		if kind == "" {
			kind = "任务"
		}
		out = append(out, Job{ID: item.ID, Name: item.Name, Type: kind, Progress: item.Progress, Status: item.Status, Owner: item.Operator})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func (s *Service) recentTimeline(limit int) []TimelineEvent {
	if s.audit == nil {
		return []TimelineEvent{}
	}
	entries, err := s.audit.List(limit)
	if err != nil {
		return []TimelineEvent{}
	}
	out := make([]TimelineEvent, 0, len(entries))
	for _, entry := range entries {
		message := strings.TrimSpace(entry.Action)
		if target := strings.TrimSpace(entry.Target); target != "" {
			message += "：" + target
		}
		if detail := strings.TrimSpace(entry.Detail); detail != "" {
			message += "（" + detail + "）"
		}
		out = append(out, TimelineEvent{ID: entry.ID, Time: entry.OccurredAt, Type: timelineType(entry.Action), Message: message, Source: entry.Actor})
	}
	return out
}

func timelineType(action string) string {
	switch {
	case strings.HasPrefix(action, "alert"), strings.Contains(action, "monitor"):
		return "alert"
	case strings.HasPrefix(action, "job"), strings.Contains(action, "execution"):
		return "job"
	case strings.HasPrefix(action, "release"), strings.Contains(action, "deploy"):
		return "deploy"
	default:
		return "system"
	}
}
