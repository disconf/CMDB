package httpapi

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/discovery"
)

type discoveryQualityReport struct {
	GeneratedAt string               `json:"generatedAt"`
	Agents      qualityAgentStats    `json:"agents"`
	Exporter    qualityExporter      `json:"exporter"`
	Tasks       qualityTaskStats     `json:"tasks"`
	Schedules   qualityScheduleStats `json:"schedules"`
	Issues      []qualityIssue       `json:"issues"`
}

type qualityAgentStats struct {
	Total      int            `json:"total"`
	Online     int            `json:"online"`
	Offline    int            `json:"offline"`
	Pending    int            `json:"pending"`
	OnlineRate float64        `json:"onlineRate"`
	Versions   []qualityCount `json:"versions"`
}

type qualityExporter struct {
	Total        int            `json:"total"`
	Active       int            `json:"active"`
	Inactive     int            `json:"inactive"`
	Unknown      int            `json:"unknown"`
	CoverageRate float64        `json:"coverageRate"`
	Ports        []qualityCount `json:"ports"`
}

type qualityTaskStats struct {
	Total       int     `json:"total"`
	Completed   int     `json:"completed"`
	Failed      int     `json:"failed"`
	Running     int     `json:"running"`
	Pending     int     `json:"pending"`
	SuccessRate float64 `json:"successRate"`
}

type qualityScheduleStats struct {
	Total       int     `json:"total"`
	Enabled     int     `json:"enabled"`
	Running     int     `json:"running"`
	Failed      int     `json:"failed"`
	SuccessRate float64 `json:"successRate"`
}

type qualityCount struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type qualityIssue struct {
	Severity       string `json:"severity"`
	Kind           string `json:"kind"`
	Target         string `json:"target"`
	Message        string `json:"message"`
	Recommendation string `json:"recommendation"`
}

func buildDiscoveryQuality(agents []discovery.Agent, tasks []discovery.Task, assets []cmdb.Asset, schedules []discovery.CollectionSchedule, now time.Time) discoveryQualityReport {
	report := discoveryQualityReport{
		GeneratedAt: now.UTC().Format(time.RFC3339),
		Agents:      qualityAgentStats{Versions: []qualityCount{}},
		Exporter:    qualityExporter{Ports: []qualityCount{}},
		Issues:      []qualityIssue{},
	}

	versions := map[string]int{}
	for _, agent := range agents {
		report.Agents.Total++
		switch strings.ToLower(strings.TrimSpace(agent.Status)) {
		case "online":
			report.Agents.Online++
		case "offline":
			report.Agents.Offline++
		default:
			report.Agents.Pending++
		}
		version := strings.TrimSpace(agent.Version)
		if version == "" || version == "-" {
			version = "未知"
		}
		versions[version]++
		if strings.EqualFold(agent.Status, "offline") {
			report.Issues = append(report.Issues, qualityIssue{
				Severity:       "critical",
				Kind:           "agent_offline",
				Target:         agent.Hostname + " (" + agent.IP + ")",
				Message:        "Agent 心跳超时，主机当前无法上报采集数据",
				Recommendation: "检查主机网络和 cmdb-agent 服务状态，恢复后确认下次心跳",
			})
		}
	}
	report.Agents.OnlineRate = qualityPercent(report.Agents.Online, report.Agents.Total)
	report.Agents.Versions = sortedQualityCounts(versions)

	ports := map[string]int{}
	for _, asset := range assets {
		if !qualityHostAsset(asset.Type) {
			continue
		}
		report.Exporter.Total++
		status := strings.ToLower(strings.TrimSpace(attributeValue(asset.Attributes, "node_exporter_status")))
		port := strings.TrimSpace(attributeValue(asset.Attributes, "node_exporter_port"))
		if status == "active" {
			report.Exporter.Active++
			if port == "" {
				port = "未知"
			}
			ports[port]++
			continue
		}
		if status == "removed" || status == "failed" || status == "inactive" {
			report.Exporter.Inactive++
		} else {
			report.Exporter.Unknown++
		}
		if status != "" && status != "active" {
			report.Issues = append(report.Issues, qualityIssue{
				Severity:       "warning",
				Kind:           "exporter_status",
				Target:         asset.Name + " (" + asset.IP + ")",
				Message:        "node_exporter 状态为 " + status + "，Prometheus 暂时不会采集该主机",
				Recommendation: "在 Exporter 管理中重新检测或安装，并确认候选端口 9100/19100",
			})
		}
	}
	report.Exporter.CoverageRate = qualityPercent(report.Exporter.Active, report.Exporter.Total)
	report.Exporter.Ports = sortedQualityCounts(ports)

	for _, task := range tasks {
		report.Tasks.Total++
		switch strings.ToLower(strings.TrimSpace(task.Status)) {
		case "completed", "success":
			report.Tasks.Completed++
		case "failed", "error":
			report.Tasks.Failed++
			report.Issues = append(report.Issues, qualityIssue{
				Severity:       "warning",
				Kind:           "task_failed",
				Target:         task.Name + " (" + task.ID + ")",
				Message:        "最近一次采集任务执行失败，未完成数据入库",
				Recommendation: "打开任务结果查看失败原因，修复凭据、端口或网络后重试",
			})
		case "running":
			report.Tasks.Running++
		default:
			report.Tasks.Pending++
		}
	}
	report.Tasks.SuccessRate = qualityPercent(report.Tasks.Completed, report.Tasks.Completed+report.Tasks.Failed)

	for _, schedule := range schedules {
		report.Schedules.Total++
		if schedule.Enabled {
			report.Schedules.Enabled++
		}
		if schedule.Running {
			report.Schedules.Running++
		}
		switch strings.ToLower(strings.TrimSpace(schedule.LastStatus)) {
		case "failed", "error":
			report.Schedules.Failed++
			report.Issues = append(report.Issues, qualityIssue{
				Severity:       "warning",
				Kind:           "schedule_failed",
				Target:         schedule.Name + " (" + schedule.ID + ")",
				Message:        "采集计划最近一次执行失败：" + firstNonEmpty(schedule.LastError, "未返回具体错误"),
				Recommendation: "检查计划目标网段、采集端口和凭据，然后点击立即执行验证",
			})
		}
	}
	report.Schedules.SuccessRate = qualityPercent(report.Schedules.Total-report.Schedules.Failed, report.Schedules.Total)

	sort.SliceStable(report.Issues, func(i, j int) bool {
		left, right := qualitySeverityRank(report.Issues[i].Severity), qualitySeverityRank(report.Issues[j].Severity)
		if left == right {
			return report.Issues[i].Target < report.Issues[j].Target
		}
		return left < right
	})
	return report
}

func registerDiscoveryQualityRoute(mux *http.ServeMux, authService *auth.Service, discoveryService *discovery.Service, cmdbService *cmdb.Service) {
	mux.HandleFunc("GET /api/v1/discovery/quality", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r, authService, "discovery:view") {
			return
		}
		schedules, err := discoveryService.CollectionSchedules()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"code": "QUALITY_SCHEDULE_READ_FAILED", "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, buildDiscoveryQuality(discoveryService.Agents(), discoveryService.Tasks(), cmdbService.InventoryAssets(), schedules, time.Now()))
	})
}

func qualityHostAsset(assetType string) bool {
	switch assetType {
	case "physical-server", "virtual-machine", "k8s-node":
		return true
	default:
		return false
	}
}

func attributeValue(attributes []cmdb.Attribute, name string) string {
	for _, attribute := range attributes {
		if attribute.Name == name {
			return attribute.Value
		}
	}
	return ""
}

func qualityPercent(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func sortedQualityCounts(values map[string]int) []qualityCount {
	result := make([]qualityCount, 0, len(values))
	for label, count := range values {
		result = append(result, qualityCount{Label: label, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count == result[j].Count {
			return result[i].Label < result[j].Label
		}
		return result[i].Count > result[j].Count
	})
	return result
}

func qualitySeverityRank(value string) int {
	switch strings.ToLower(value) {
	case "critical":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
