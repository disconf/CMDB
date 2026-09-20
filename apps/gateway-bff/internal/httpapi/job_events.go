package httpapi

import (
	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

func handleJobTerminal(job jobs.Job, monitorService *monitor.Service, ticketsService *tickets.Service, cmdbService *cmdb.Service) {
	if job.Status != "failed" && job.Status != "success" {
		return
	}
	target := ""
	if len(job.Targets) > 0 {
		target = job.Targets[0]
	}
	targetName := target
	owner := ""
	projectGroup := ""
	if target != "" {
		if asset, err := cmdbService.GetAsset(target); err == nil {
			targetName = asset.Name
			owner = asset.Owner
			projectGroup = asset.ProjectGroup
			target = asset.ID
		}
	}
	message := "自动化任务执行失败"
	if len(job.Logs) > 0 {
		message = job.Logs[len(job.Logs)-1].Message
	}
	alertID := "job-failed-" + job.ID
	title := "自动化任务失败: " + job.Name
	if job.Status == "success" {
		_, _ = monitorService.ResolveJobFailure(monitor.JobFailureInput{JobID: job.ID, JobName: job.Name, Target: target, TargetName: targetName, Owner: owner, ProjectGroup: projectGroup})
		_, _, _ = ticketsService.SyncAlert(tickets.AlertInput{ID: alertID, Title: title, Severity: "info", Status: "resolved", Target: target, Message: "任务后续执行成功，系统自动关闭告警和关联工单", Owner: owner})
		return
	}
	severity := "warning"
	if job.Operator == "scheduler" {
		severity = "critical"
	}
	if targetName != "" {
		message = targetName + " " + message
	}
	_, _ = monitorService.ReceiveJobFailure(monitor.JobFailureInput{JobID: job.ID, JobName: job.Name, Target: target, TargetName: targetName, Message: message, Severity: severity, Owner: owner, ProjectGroup: projectGroup, StartedAt: job.StartedAt})
	_, _, _ = ticketsService.SyncAlert(tickets.AlertInput{ID: alertID, Title: title, Severity: severity, Status: "firing", Target: target, Message: message, Owner: owner})
}
