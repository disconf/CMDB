package releases

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type GateInput struct {
	Release       Release
	Now           time.Time
	ChangeWindows string
	Alerts        []GateAlert
	Assets        []GateAsset
	Argo          *GateArgo
}

type GateAlert struct {
	ID       string
	Title    string
	Severity string
	Status   string
	Target   string
}

type GateAsset struct {
	ID      string
	Name    string
	Type    string
	Status  string
	Env     string
	Project string
}

type GateArgo struct {
	Name         string
	SyncStatus   string
	HealthStatus string
}

type GateCheck struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type GateResult struct {
	Allowed   bool        `json:"allowed"`
	Decision  string      `json:"decision"`
	CheckedAt string      `json:"checkedAt"`
	Checks    []GateCheck `json:"checks"`
	Blockers  []string    `json:"blockers"`
	Warnings  []string    `json:"warnings"`
}

func EvaluateGate(input GateInput) GateResult {
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	result := GateResult{Allowed: true, Decision: "allow", CheckedAt: now.Format(time.RFC3339)}
	add := func(id, name, status, message string) {
		result.Checks = append(result.Checks, GateCheck{ID: id, Name: name, Status: status, Message: message})
		if status == "block" {
			result.Allowed = false
			result.Blockers = append(result.Blockers, message)
		} else if status == "warn" {
			result.Warnings = append(result.Warnings, message)
		}
	}
	if input.Release.Status != StatusApproved && input.Release.Status != StatusRollbackApproved {
		add("release-state", "发布状态", "block", "当前发布不在可执行授权状态")
	} else {
		add("release-state", "发布状态", "pass", "发布已通过审批并具备执行授权")
	}
	windowAllowed, windowMessage := changeWindowAllowed(input.Release.Environment, input.ChangeWindows, now)
	if windowAllowed {
		add("change-window", "变更窗口", "pass", windowMessage)
	} else {
		add("change-window", "变更窗口", "block", windowMessage)
	}
	critical, warning := activeGateAlerts(input.Release.Application, input.Alerts)
	if critical > 0 {
		add("active-alerts", "活动告警", "block", fmt.Sprintf("存在 %d 条严重活动告警", critical))
	} else if warning > 0 {
		add("active-alerts", "活动告警", "warn", fmt.Sprintf("存在 %d 条关注级告警", warning))
	} else {
		add("active-alerts", "活动告警", "pass", "未发现关联的严重或关注级活动告警")
	}
	if input.Argo == nil {
		add("argocd-health", "Argo CD 健康", "warn", "未找到同名 Argo CD Application，跳过健康校验")
	} else if strings.EqualFold(input.Argo.HealthStatus, "Degraded") || strings.EqualFold(input.Argo.SyncStatus, "OutOfSync") {
		add("argocd-health", "Argo CD 健康", "block", "Argo CD 应用处于 Degraded 或 OutOfSync 状态")
	} else if strings.EqualFold(input.Argo.HealthStatus, "Progressing") {
		add("argocd-health", "Argo CD 健康", "warn", "Argo CD 应用仍在同步中")
	} else {
		add("argocd-health", "Argo CD 健康", "pass", "Argo CD 应用同步且健康")
	}
	if len(input.Assets) == 0 {
		add("asset-risk", "资产风险", "warn", "没有找到与发布应用同名的 CMDB 资产，建议先完善资产关联")
	} else {
		criticalAssets := 0
		warningAssets := 0
		for _, asset := range input.Assets {
			if asset.Status == "offline" || asset.Status == "critical" {
				criticalAssets++
			} else if asset.Status == "warning" {
				warningAssets++
			}
		}
		if criticalAssets > 0 {
			add("asset-risk", "资产风险", "block", fmt.Sprintf("关联资产中有 %d 台离线或严重异常", criticalAssets))
		} else if warningAssets > 0 {
			add("asset-risk", "资产风险", "warn", fmt.Sprintf("关联资产中有 %d 台处于关注状态", warningAssets))
		} else {
			add("asset-risk", "资产风险", "pass", fmt.Sprintf("关联的 %d 台资产状态正常", len(input.Assets)))
		}
	}
	if input.Release.Environment == "production" && strings.TrimSpace(input.Release.TicketID) == "" {
		add("change-ticket", "变更工单", "warn", "生产发布未关联变更工单")
	} else {
		add("change-ticket", "变更工单", "pass", "变更信息完整")
	}
	if !result.Allowed {
		result.Decision = "block"
	}
	sort.SliceStable(result.Checks, func(i, j int) bool { return result.Checks[i].ID < result.Checks[j].ID })
	return result
}

func changeWindowAllowed(environment, policy string, now time.Time) (bool, string) {
	policy = strings.TrimSpace(policy)
	if policy == "" || policy == "*" {
		return true, "当前时间在允许的变更窗口内"
	}
	segment := "*"
	for _, item := range strings.Split(policy, ";") {
		parts := strings.SplitN(strings.TrimSpace(item), "=", 2)
		if len(parts) != 2 {
			continue
		}
		if parts[0] == environment || parts[0] == "*" {
			segment = strings.TrimSpace(parts[1])
			break
		}
	}
	if segment == "*" {
		return true, "当前时间在允许的变更窗口内"
	}
	dayAllowed := false
	timeAllowed := true
	dayNames := map[string]time.Weekday{"Sun": time.Sunday, "Mon": time.Monday, "Tue": time.Tuesday, "Wed": time.Wednesday, "Thu": time.Thursday, "Fri": time.Friday, "Sat": time.Saturday}
	for _, token := range strings.Split(segment, ",") {
		token = strings.TrimSpace(token)
		if token == "" || token == "*" {
			dayAllowed = true
			continue
		}
		if strings.Contains(token, "-") && !strings.Contains(token, ":") {
			parts := strings.SplitN(token, "-", 2)
			start, startOK := dayNames[parts[0]]
			end, endOK := dayNames[parts[1]]
			if startOK && endOK {
				dayAllowed = dayAllowed || weekdayBetween(now.Weekday(), start, end)
			}
			continue
		}
		if day, ok := dayNames[token]; ok {
			dayAllowed = dayAllowed || now.Weekday() == day
			continue
		}
		if strings.Contains(token, ":") {
			parts := strings.SplitN(token, "-", 2)
			if len(parts) == 2 {
				start, startErr := time.Parse("15:04", strings.TrimSpace(parts[0]))
				end, endErr := time.Parse("15:04", strings.TrimSpace(parts[1]))
				if startErr == nil && endErr == nil {
					current := now.Hour()*60 + now.Minute()
					startMinute := start.Hour()*60 + start.Minute()
					endMinute := end.Hour()*60 + end.Minute()
					if startMinute <= endMinute {
						timeAllowed = current >= startMinute && current <= endMinute
					} else {
						timeAllowed = current >= startMinute || current <= endMinute
					}
				}
			}
		}
	}
	if dayAllowed && timeAllowed {
		return true, "当前时间在允许的变更窗口内"
	}
	return false, "当前时间不在允许的变更窗口内"
}

func weekdayBetween(current, start, end time.Weekday) bool {
	if start <= end {
		return current >= start && current <= end
	}
	return current >= start || current <= end
}

func activeGateAlerts(application string, alerts []GateAlert) (critical, warning int) {
	application = strings.ToLower(strings.TrimSpace(application))
	for _, alert := range alerts {
		if strings.EqualFold(alert.Status, "resolved") || strings.EqualFold(alert.Status, "silenced") {
			continue
		}
		target := strings.ToLower(alert.Target + " " + alert.Title)
		if application != "" && !strings.Contains(target, application) {
			continue
		}
		if strings.EqualFold(alert.Severity, "critical") {
			critical++
		} else if strings.EqualFold(alert.Severity, "warning") {
			warning++
		}
	}
	return critical, warning
}
