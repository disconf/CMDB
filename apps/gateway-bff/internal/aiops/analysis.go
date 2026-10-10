package aiops

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/topology"
)

func (s *Service) Analyze(ctx context.Context, alertID string) (Analysis, error) {
	if err := s.ready(); err != nil {
		return Analysis{}, err
	}
	alert, ok := s.findAlert(strings.TrimSpace(alertID))
	if !ok {
		return Analysis{}, ErrNotFound
	}
	analysisID := "ana-" + safeID(alert.ID)
	now := time.Now()
	item := Analysis{
		ID: analysisID, AlertID: alert.ID, AlertTitle: alert.Title, Title: alert.Title + " 归因分析",
		Status: "completed", Severity: alert.Severity, Target: alert.Target,
		CreatedAt: now.Format("2006-01-02 15:04:05"), UpdatedAt: now.Format("2006-01-02 15:04:05"),
		Evidence: []Evidence{}, Recommendations: []Recommendation{}, RelatedAssets: []RelatedAsset{},
		Timeline: []TimelineEvent{}, Tickets: []RelatedTicket{}, Releases: []RelatedRelease{}, DataSources: []string{},
	}
	if existing, err := s.Get(analysisID); err == nil {
		item.CreatedAt = existing.CreatedAt
	}
	addSource(&item, "monitor_alerts")
	addEvidence(&item, "alert", "monitor", "告警状态与严重级别", fmt.Sprintf("%s / %s，目标 %s，持续时间 %s，消息：%s", alert.Status, alert.Severity, alert.Target, alert.Duration, alert.Message), 20)
	item.Timeline = append(item.Timeline, TimelineEvent{Time: alert.StartedAt, Type: "alert", Source: "monitor", Message: alert.Title + "：" + alert.Message})

	metrics := []monitor.Metric{}
	if s.alerts != nil {
		host := s.alerts.Host(alert.TargetID)
		metrics = host.Metrics
		if len(metrics) > 0 {
			addSource(&item, "prometheus")
		}
		for index, metric := range metrics {
			if index >= 3 {
				break
			}
			level := "info"
			if metric.Critical > 0 && metric.Value >= metric.Critical {
				level = "critical"
			} else if metric.Warning > 0 && metric.Value >= metric.Warning {
				level = "warning"
			}
			addEvidence(&item, "metric", "prometheus", metric.Name, fmt.Sprintf("当前值 %.2f%s，阈值 warning=%.2f critical=%.2f（%s）", metric.Value, metric.Unit, metric.Warning, metric.Critical, level), 25)
		}
		events, err := s.alerts.Events(alert.ID)
		if err == nil && len(events) > 0 {
			addSource(&item, "monitor_alert_events")
			for index, event := range events {
				if index >= 8 {
					break
				}
				item.Timeline = append(item.Timeline, TimelineEvent{Time: event.CreatedAt, Type: event.Type, Source: "alert-event", Message: event.Message})
			}
			addEvidence(&item, "event", "monitor", "告警处置时间线", fmt.Sprintf("存在 %d 条告警事件，最近事件：%s", len(events), events[0].Message), 10)
		}
	}

	asset, found := s.resolveAsset(alert)
	if found {
		addSource(&item, "cmdb_assets")
		item.RelatedAssets = append(item.RelatedAssets, relatedAsset(asset, "alert-target"))
		addEvidence(&item, "asset", "cmdb", "CMDB 资产上下文", fmt.Sprintf("%s（%s/%s，IP %s，状态 %s，负责人 %s，位置 %s）", asset.Name, asset.TypeName, asset.Environment, asset.IP, asset.Status, asset.Owner, asset.Location), 15)
		item.Timeline = append(item.Timeline, TimelineEvent{Time: asset.LastSeenAt, Type: "asset", Source: "cmdb", Message: "资产最后上报：" + asset.Name})
		for index, relation := range asset.Relations {
			if index >= 5 {
				break
			}
			relationAsset, err := s.assets.GetAsset(relation.TargetID)
			if err == nil {
				item.RelatedAssets = append(item.RelatedAssets, relatedAsset(relationAsset, relation.Type))
			}
		}
		if s.impact != nil {
			if impact, err := s.impact.Impact(asset.ID); err == nil {
				addSource(&item, "topology")
				item.Impact = impactSummary(impact)
				if item.Impact.Total > 0 {
					addEvidence(&item, "topology", "topology", "拓扑影响范围", fmt.Sprintf("从根资产出发发现 %d 个关联节点，严重 %d，关注 %d", item.Impact.Total, item.Impact.Critical, item.Impact.Warning), 15)
				}
			}
		}
	}

	if s.tickets != nil {
		if items, err := s.tickets.List(); err == nil {
			for _, ticket := range items {
				if !matchesAlertOrAsset(ticket.RelatedAlert, ticket.RelatedAsset, alert.ID, asset.ID) {
					continue
				}
				addSource(&item, "tickets")
				item.Tickets = append(item.Tickets, RelatedTicket{ID: ticket.ID, Title: ticket.Title, Status: ticket.Status, Assignee: ticket.Assignee})
			}
			if len(item.Tickets) > 0 {
				addEvidence(&item, "ticket", "tickets", "关联工单", fmt.Sprintf("发现 %d 个关联工单，最近工单：%s（%s）", len(item.Tickets), item.Tickets[0].Title, item.Tickets[0].Status), 10)
			}
		}
	}

	if s.releases != nil {
		if items, err := s.releases.List(); err == nil {
			for _, release := range items {
				if !releaseMatches(release.Application, release.Environment, release.StartedAt, alert, asset) {
					continue
				}
				addSource(&item, "releases")
				item.Releases = append(item.Releases, RelatedRelease{ID: release.ID, Application: release.Application, Version: release.Version, Status: release.Status, StartedAt: release.StartedAt})
				if len(item.Releases) >= 5 {
					break
				}
			}
			if len(item.Releases) > 0 {
				release := item.Releases[0]
				addEvidence(&item, "change", "releases", "近期发布变更", fmt.Sprintf("%s %s 当前状态 %s，开始时间 %s", release.Application, release.Version, release.Status, release.StartedAt), 10)
			}
		}
	}

	inferredCause := inferRootCause(alert, metrics, asset)
	correctedCause, adjustment, reason, feedbackCount := s.feedbackAdjustment(item.AlertTitle, inferredCause)
	item.RootCause = correctedCause
	item.Confidence = clampConfidence(confidenceFromEvidence(item.Evidence) + adjustment)
	item.ConfidenceAdjustment = adjustment
	item.ConfidenceReason = reason
	item.AppliedFeedbackCount = feedbackCount
	item.Recommendations = recommendationFor(item.RootCause)
	if reason != "" {
		item.Timeline = append(item.Timeline, TimelineEvent{Time: time.Now().Format("2006-01-02 15:04:05"), Type: "feedback", Source: "feedback", Message: reason})
	}
	item.Summary = fmt.Sprintf("基于 %d 项证据生成根因假设：%s；置信度调整 %+d，参考历史反馈 %d 条；关联资产 %d 个，关联工单 %d 个，近期发布 %d 个。", len(item.Evidence), item.RootCause, adjustment, feedbackCount, len(item.RelatedAssets), len(item.Tickets), len(item.Releases))
	sort.SliceStable(item.Timeline, func(i, j int) bool { return item.Timeline[i].Time > item.Timeline[j].Time })
	if err := s.repo.SaveAnalysis(ctx, item); err != nil {
		return Analysis{}, err
	}
	s.mu.Lock()
	replaced := false
	for i := range s.analyses {
		if s.analyses[i].ID == item.ID {
			s.analyses[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		s.analyses = append([]Analysis{item}, s.analyses...)
	}
	s.mu.Unlock()
	return item, nil
}
func (s *Service) findAlert(id string) (monitor.Alert, bool) {
	if s.alerts == nil {
		return monitor.Alert{}, false
	}
	for _, item := range s.alerts.Alerts() {
		if item.ID == id {
			return item, true
		}
	}
	return monitor.Alert{}, false
}

func (s *Service) resolveAsset(alert monitor.Alert) (cmdb.Asset, bool) {
	if s.assets == nil {
		return cmdb.Asset{}, false
	}
	if asset, err := s.assets.GetAsset(alert.TargetID); err == nil {
		return asset, true
	}
	if ip := extractNodeExporterIP(alert.TargetID); ip != "" {
		if id := s.assets.FindHostByIP(ip, ""); id != "" {
			if asset, err := s.assets.GetAsset(id); err == nil {
				return asset, true
			}
		}
	}
	if id := s.assets.FindHostByIP(alert.Target, ""); id != "" {
		if asset, err := s.assets.GetAsset(id); err == nil {
			return asset, true
		}
	}
	page := s.assets.ListAssets(cmdb.AssetQuery{Search: alert.Target, Page: 1, PageSize: 10, ScopeAll: true})
	if len(page.Data) > 0 {
		return page.Data[0], true
	}
	for _, assetID := range []string{alert.TargetID, alert.Target} {
		if id := s.assets.FindHostByIP(assetID, ""); id != "" {
			if asset, err := s.assets.GetAsset(id); err == nil {
				return asset, true
			}
		}
	}
	return cmdb.Asset{}, false
}

func relatedAsset(asset cmdb.Asset, relation string) RelatedAsset {
	return RelatedAsset{ID: asset.ID, Name: asset.Name, Type: asset.TypeName, Status: asset.Status, IP: asset.IP, Environment: asset.Environment, ProjectGroup: asset.ProjectGroup, Owner: asset.Owner, Location: asset.Location, Relation: relation, CredentialID: assetCredentialID(asset)}
}

func assetCredentialID(asset cmdb.Asset) string {
	for _, item := range asset.Attributes {
		switch item.Name {
		case "ssh_credential_id", "credential_id":
			if value := strings.TrimSpace(item.Value); value != "" {
				return value
			}
		}
	}
	return ""
}
func matchesAlertOrAsset(relatedAlert, relatedAsset, alertID, assetID string) bool {
	if relatedAlert != "" && relatedAlert == alertID {
		return true
	}
	if relatedAsset != "" && assetID != "" && relatedAsset == assetID {
		return true
	}
	return false
}

func releaseMatches(application, environment, startedAt string, alert monitor.Alert, asset cmdb.Asset) bool {
	started, err := time.ParseInLocation("2006-01-02 15:04:05", startedAt, time.Local)
	if err != nil {
		return false
	}
	if time.Since(started) > 24*time.Hour {
		return false
	}
	haystack := strings.ToLower(alert.Title + " " + alert.Message + " " + alert.Target + " " + asset.Name + " " + asset.ProjectGroup)
	name := strings.TrimSpace(strings.ToLower(application))
	if name != "" && strings.Contains(haystack, name) {
		return true
	}
	return name != "" && strings.TrimSpace(environment) != "" && strings.TrimSpace(asset.Environment) != "" && strings.EqualFold(environment, asset.Environment) && strings.Contains(haystack, name)
}

func inferRootCause(alert monitor.Alert, metrics []monitor.Metric, asset cmdb.Asset) string {
	alertText := strings.ToLower(alert.Title + " " + alert.Message + " " + alert.Rule + " " + alert.Target)
	assetText := strings.ToLower(asset.Name + " " + asset.Type + " " + asset.TypeName)
	metricText := ""
	for _, metric := range metrics {
		metricText += " " + strings.ToLower(metric.Name)
	}
	switch {
	case containsAny(alertText, "oom", "内存", "memory"):
		return "内存资源压力"
	case containsAny(alertText, "磁盘", "disk", "inode"):
		return "磁盘空间耗尽风险"
	case containsAny(alertText, "接口 down", "link", "链路", "网络设备接口", "mac", "漂移", "多个端口", "端口出现"):
		return "网络链路异常"
	case containsAny(alertText, "pod", "k8s", "kubernetes"):
		return "Kubernetes 工作负载异常"
	case containsAny(alertText, "p95", "延迟", "latency", "timeout"):
		return "服务延迟异常"
	case containsAny(alertText, "数据库", "mysql", "postgres", "连接池", "slow query"):
		return "数据库连接或慢查询压力"
	case containsAny(metricText, "内存", "memory"):
		return "内存资源压力"
	case containsAny(metricText, "磁盘", "disk", "inode"):
		return "磁盘空间耗尽风险"
	case containsAny(metricText, "延迟", "latency"):
		return "服务延迟异常"
	case containsAny(assetText, "数据库", "mysql", "postgres"):
		return "数据库连接或慢查询压力"
	default:
		return "待进一步确认的异常"
	}
}

func clampConfidence(value int) int {
	if value > 98 {
		return 98
	}
	if value < 10 {
		return 10
	}
	return value
}
func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func addEvidence(item *Analysis, evidenceType, source, title, detail string, weight int) {
	item.Evidence = append(item.Evidence, Evidence{Type: evidenceType, Source: source, Title: title, Detail: detail, Weight: weight})
}

func addSource(item *Analysis, source string) {
	if !containsString(item.DataSources, source) {
		item.DataSources = append(item.DataSources, source)
	}
}

func confidenceFromEvidence(evidence []Evidence) int {
	score := 30
	for _, item := range evidence {
		score += item.Weight
	}
	if score > 96 {
		return 96
	}
	if score < 35 {
		return 35
	}
	return score
}
func impactSummary(impact topology.Impact) ImpactSummary {
	result := ImpactSummary{Total: impact.Summary.Total, Healthy: impact.Summary.Healthy, Warning: impact.Summary.Warning, Critical: impact.Summary.Critical, Items: []string{}}
	for index, node := range impact.Affected {
		if index >= 20 {
			break
		}
		result.Items = append(result.Items, fmt.Sprintf("%s（%s，距离 %d）", node.Name, node.Status, node.Distance))
	}
	return result
}

func safeID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
		} else {
			builder.WriteRune('-')
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "unknown"
	}
	return result
}

func extractNodeExporterIP(value string) string {
	value = strings.TrimSpace(value)
	const prefix = "node-exporter-"
	if !strings.HasPrefix(value, prefix) {
		return ""
	}
	value = strings.TrimPrefix(value, prefix)
	parts := strings.Split(value, "-")
	if len(parts) != 4 {
		return ""
	}
	for _, part := range parts {
		if part == "" {
			return ""
		}
	}
	return strings.Join(parts, ".")
}
