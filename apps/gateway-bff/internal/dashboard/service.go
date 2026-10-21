package dashboard

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/audit"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/discovery"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/releases"
	"cmdb/gateway-bff/internal/tickets"
)

type Service struct {
	cmdb      *cmdb.Service
	discovery *discovery.Service
	monitor   *monitor.Service
	jobs      *jobs.Service
	audit     *audit.Service
	tickets   *tickets.Service
	releases  *releases.Service
}

// SetActivitySources wires the job and audit services used by the activity and
// timeline panels. They are optional: without them those panels stay empty.
func (s *Service) SetActivitySources(jobsService *jobs.Service, auditService *audit.Service) {
	s.jobs = jobsService
	s.audit = auditService
}

func (s *Service) SetOperationsSources(ticketsService *tickets.Service, releasesService *releases.Service) {
	s.tickets = ticketsService
	s.releases = releasesService
}

func NewService() *Service { return &Service{} }

func NewServiceWithRuntime(cmdbService *cmdb.Service, discoveryService *discovery.Service, monitorService *monitor.Service) *Service {
	return &Service{cmdb: cmdbService, discovery: discoveryService, monitor: monitorService}
}

func (s *Service) Overview() Overview {
	if s.cmdb != nil && s.discovery != nil && s.monitor != nil {
		return s.liveOverview()
	}
	return Overview{
		UpdatedAt: time.Now(),
		Capacity:  []CapacityItem{}, Health: []HealthItem{}, Sources: []DataSource{},
		Topology: Topology{Layers: []TopologyLayer{}}, AlertTrend: []TrendPoint{},
		Alerts: []Alert{}, Jobs: []Job{}, Timeline: []TimelineEvent{}, Deployments: []Deployment{},
		Releases: ReleaseSnapshot{Recent: []Deployment{}},
	}
}

// liveOverview builds the dashboard from real platform data: CMDB inventory,
// Prometheus-backed alerts, job executions and audit records. Every panel is
// derived from data the platform actually owns instead of a static demo sample.
func (s *Service) liveOverview() Overview {
	now := time.Now()
	assets := s.cmdb.InventoryAssets()
	alerts := s.monitor.Alerts()
	assetSnapshot := buildAssetSnapshot(assets)
	agentSnapshot := buildAgentSnapshot(s.discovery.Summary())
	monitorSnapshot := buildMonitorSnapshot(s.monitor.Coverage(monitorExpectedHosts(s.cmdb.MonitoringAssets())))
	applyAlertSummary(&monitorSnapshot, alerts)
	taskSnapshot := s.buildTaskSnapshot(now)
	ticketSnapshot := s.buildTicketSnapshot(now)
	releaseSnapshot := s.buildReleaseSnapshot(now)
	command := buildCommandCenter(assetSnapshot, agentSnapshot, monitorSnapshot, taskSnapshot, ticketSnapshot, releaseSnapshot)
	return Overview{
		UpdatedAt:   now,
		Metrics:     Metrics{AssetTotal: s.cmdb.Summary().Total, OnlineAgents: s.discovery.Summary().Online, ActiveAlerts: s.monitor.ActiveCount(), TodayJobs: s.todayJobs(now)},
		Command:     command,
		Assets:      assetSnapshot,
		Agents:      agentSnapshot,
		Monitoring:  monitorSnapshot,
		Tasks:       taskSnapshot,
		Tickets:     ticketSnapshot,
		Releases:    releaseSnapshot,
		Sources:     buildDataSources(now, assetSnapshot, agentSnapshot, monitorSnapshot, taskSnapshot, ticketSnapshot, releaseSnapshot),
		Capacity:    buildCapacity(assets),
		Health:      buildHealth(assets),
		Topology:    buildTopology(assets),
		AlertTrend:  buildAlertTrend(alerts, now),
		Alerts:      buildAlertList(alerts, 10),
		Jobs:        s.activeJobs(),
		Timeline:    s.recentTimeline(8),
		Deployments: releaseSnapshot.Recent,
	}
}

func buildAssetSnapshot(assets []cmdb.Asset) AssetSnapshot {
	projects := map[string]bool{}
	environments := map[string]bool{}
	sources := map[string]bool{}
	result := AssetSnapshot{Total: len(assets)}
	for _, asset := range assets {
		status := strings.ToLower(strings.TrimSpace(asset.Status))
		switch status {
		case "online", "healthy", "running", "ready":
			result.Online++
		case "warning", "degraded":
			result.Warning++
		case "offline", "down", "failed", "error":
			result.Offline++
		default:
			result.Unknown++
		}
		if value := strings.TrimSpace(asset.ProjectGroup); value != "" {
			projects[value] = true
		}
		if value := strings.TrimSpace(asset.Environment); value != "" {
			environments[value] = true
		}
		if value := strings.TrimSpace(asset.Source); value != "" {
			sources[value] = true
		}
	}
	result.ProjectGroups = len(projects)
	result.Environments = len(environments)
	result.Sources = len(sources)
	return result
}

func buildAgentSnapshot(summary discovery.Summary) AgentSnapshot {
	item := AgentSnapshot{Total: summary.Total, Online: summary.Online, Offline: summary.Offline, Pending: summary.Pending}
	if item.Total > 0 {
		item.Coverage = percentage(item.Online, item.Total)
	}
	return item
}

func buildMonitorSnapshot(coverage monitor.Coverage) MonitorSnapshot {
	return buildMonitorSnapshotFromCoverage(coverage)
}

func monitorExpectedHosts(assets []cmdb.Asset) []monitor.ExpectedHost {
	expected := make([]monitor.ExpectedHost, 0, len(assets))
	for _, asset := range assets {
		expected = append(expected, monitor.ExpectedHost{
			AssetID: asset.ID, Name: asset.Name, IP: asset.IP, Environment: asset.Environment,
			ProjectGroup: asset.ProjectGroup, AssetStatus: asset.Status,
		})
	}
	return expected
}

func (s *Service) buildTaskSnapshot(now time.Time) TaskSnapshot {
	if s.jobs == nil {
		return TaskSnapshot{}
	}
	summary := s.jobs.Summary()
	return buildTaskSnapshotFromItems(s.jobs.Jobs(), summary.Schedules, now)
}

func (s *Service) buildTicketSnapshot(now time.Time) TicketSnapshot {
	if s.tickets == nil {
		return TicketSnapshot{SLACompliance: 100}
	}
	items, err := s.tickets.List()
	if err != nil {
		return TicketSnapshot{SLACompliance: 100}
	}
	return buildTicketSnapshotFromItems(items, now)
}

func (s *Service) buildReleaseSnapshot(now time.Time) ReleaseSnapshot {
	if s.releases == nil {
		return ReleaseSnapshot{Recent: []Deployment{}}
	}
	releasesList, err := s.releases.List()
	if err != nil {
		return ReleaseSnapshot{Recent: []Deployment{}}
	}
	return buildReleaseSnapshotFromItems(releasesList, now)
}

func buildCommandCenter(assets AssetSnapshot, agents AgentSnapshot, monitoring MonitorSnapshot, tasks TaskSnapshot, tickets TicketSnapshot, releases ReleaseSnapshot) CommandCenter {
	item := CommandCenter{
		PlatformStatus: "operational", AgentCoverage: agents.Coverage, MonitorCoverage: monitoring.Coverage,
		TaskSuccessRate: tasks.SuccessRate, SLACompliance: tickets.SLACompliance,
		ReleaseSuccessRate: releases.SuccessRate, ActiveIncidents: monitoring.Critical, CriticalAlerts: monitoring.Critical,
	}
	if assets.Total > 0 {
		item.AssetHealth = percentage(assets.Online, assets.Total)
	}
	if monitoring.Critical > 0 || assets.Offline > 0 {
		item.PlatformStatus = "attention"
	}
	return item
}

func buildDataSources(now time.Time, assets AssetSnapshot, agents AgentSnapshot, monitoring MonitorSnapshot, tasks TaskSnapshot, tickets TicketSnapshot, releases ReleaseSnapshot) []DataSource {
	updatedAt := now.Format(time.RFC3339)
	status := func(count int) string {
		if count > 0 {
			return "live"
		}
		return "idle"
	}
	return []DataSource{
		{Code: "cmdb", Name: "CMDB 主数据", Status: status(assets.Total), Detail: fmt.Sprintf("%d 个项目组 · %d 个环境", assets.ProjectGroups, assets.Environments), Count: assets.Total, UpdatedAt: updatedAt},
		{Code: "agents", Name: "Agent 舰队", Status: status(agents.Total), Detail: fmt.Sprintf("在线 %d · 离线 %d", agents.Online, agents.Offline), Count: agents.Total, UpdatedAt: updatedAt},
		{Code: "prometheus", Name: "Prometheus", Status: status(monitoring.Targets), Detail: fmt.Sprintf("%d/%d 目标健康", monitoring.Healthy, monitoring.Targets), Count: monitoring.Targets, UpdatedAt: updatedAt},
		{Code: "jobs", Name: "自动化任务", Status: status(tasks.Today + tasks.Running + tasks.PendingApproval + tasks.Schedules), Detail: fmt.Sprintf("今日 %d 次 · 运行中 %d · 待审批 %d", tasks.Today, tasks.Running, tasks.PendingApproval), Count: tasks.Today + tasks.Running + tasks.PendingApproval + tasks.Schedules, UpdatedAt: updatedAt},
		{Code: "tickets", Name: "工单与审批", Status: status(tickets.Total), Detail: fmt.Sprintf("待处理 %d · SLA预警 %d", tickets.Pending+tickets.Processing, tickets.SLAWarning), Count: tickets.Total, UpdatedAt: updatedAt},
		{Code: "releases", Name: "发布流水线", Status: status(releases.Today + releases.Running + len(releases.Recent)), Detail: fmt.Sprintf("今日 %d 次 · 运行中 %d · 成功率 %.1f%%", releases.Today, releases.Running, releases.SuccessRate), Count: releases.Today + releases.Running + len(releases.Recent), UpdatedAt: updatedAt},
	}
}

func percentage(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(int(float64(part)*1000/float64(total)+0.5)) / 10
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
