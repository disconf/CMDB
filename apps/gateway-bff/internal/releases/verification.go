package releases

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type VerificationInput struct {
	Release         Release
	Now             time.Time
	Argo            *GateArgo
	PrometheusReady bool
	TargetsUp       int
	TargetsDown     int
	TargetsUnknown  int
	CriticalAlerts  int
	WarningAlerts   int
}

type VerificationCheck struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type VerificationResult struct {
	ReleaseID       string              `json:"releaseId"`
	Decision        string              `json:"decision"`
	Reason          string              `json:"reason"`
	CanAutoRollback bool                `json:"canAutoRollback"`
	CheckedAt       string              `json:"checkedAt"`
	Checks          []VerificationCheck `json:"checks"`
}

func EvaluateVerification(input VerificationInput) VerificationResult {
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	result := VerificationResult{ReleaseID: input.Release.ID, Decision: "success", CheckedAt: now.Format(time.RFC3339)}
	blocked := false
	warned := false
	add := func(id, name, status, message string) {
		result.Checks = append(result.Checks, VerificationCheck{ID: id, Name: name, Status: status, Message: message})
		if status == "block" {
			blocked = true
		} else if status == "warn" {
			warned = true
		}
	}
	if input.Release.Status != StatusRunning && input.Release.Status != StatusSuccess {
		add("release-state", "发布状态", "warn", "当前发布状态不适合执行发布后验证")
	} else {
		add("release-state", "发布状态", "pass", "发布已进入运行或完成状态")
	}
	if input.Argo == nil {
		add("argocd-health", "Argo CD 健康", "warn", "未找到同名 Argo CD Application")
	} else if strings.EqualFold(input.Argo.HealthStatus, "Degraded") || strings.EqualFold(input.Argo.SyncStatus, "OutOfSync") {
		add("argocd-health", "Argo CD 健康", "block", fmt.Sprintf("应用状态为 %s / %s", input.Argo.HealthStatus, input.Argo.SyncStatus))
	} else if strings.EqualFold(input.Argo.HealthStatus, "Progressing") {
		add("argocd-health", "Argo CD 健康", "warn", "应用仍在同步中，建议继续观察")
	} else {
		add("argocd-health", "Argo CD 健康", "pass", "Argo CD 应用同步且健康")
	}
	if !input.PrometheusReady {
		add("prometheus-targets", "监控目标", "warn", "Prometheus 暂时无法查询发布相关目标")
	} else if input.TargetsDown > 0 {
		add("prometheus-targets", "监控目标", "block", fmt.Sprintf("有 %d 个关联监控目标不可达", input.TargetsDown))
	} else if input.TargetsUp == 0 {
		add("prometheus-targets", "监控目标", "warn", "暂未查询到关联监控目标数据")
	} else if input.TargetsUnknown > 0 {
		add("prometheus-targets", "监控目标", "warn", fmt.Sprintf("%d 个监控目标样本无法解析", input.TargetsUnknown))
	} else {
		add("prometheus-targets", "监控目标", "pass", fmt.Sprintf("%d 个关联监控目标正常", input.TargetsUp))
	}
	if input.CriticalAlerts > 0 {
		add("active-alerts", "活动告警", "block", fmt.Sprintf("存在 %d 条严重活动告警", input.CriticalAlerts))
	} else if input.WarningAlerts > 0 {
		add("active-alerts", "活动告警", "warn", fmt.Sprintf("存在 %d 条关注级告警", input.WarningAlerts))
	} else {
		add("active-alerts", "活动告警", "pass", "没有关联的活动告警")
	}
	if blocked {
		result.Decision = "rollback"
		result.Reason = "发布后健康验证发现阻断项，建议发起回滚"
	} else if warned {
		result.Decision = "observe"
		result.Reason = "发布后存在关注项，建议延长观察窗口"
	} else {
		result.Reason = "发布后健康验证通过"
	}
	result.CanAutoRollback = result.Decision == "rollback" && (input.Release.Status == StatusRunning || input.Release.Status == StatusSuccess || input.Release.Status == StatusFailed)
	sort.SliceStable(result.Checks, func(i, j int) bool { return result.Checks[i].ID < result.Checks[j].ID })
	return result
}
