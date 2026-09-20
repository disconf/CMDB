package httpapi

import (
	"strings"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

func handleJobTerminal(job jobs.Job, monitorService *monitor.Service, ticketsService *tickets.Service, cmdbService *cmdb.Service) {
	if job.Status != "failed" {
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
	severity := "warning"
	if job.Operator == "scheduler" {
		severity = "critical"
	}
	_, _ = monitorService.ReceiveJobFailure(monitor.JobFailureInput{JobID: job.ID, JobName: job.Name, Target: target, TargetName: targetName, Message: message, Severity: severity, Owner: owner, ProjectGroup: projectGroup, StartedAt: job.StartedAt})
	automation := job.PlaybookID
	if automation == "" {
		automation = job.TemplateID
	}
	description := strings.Join([]string{"调度任务失败", "任务ID: " + job.ID, "目标: " + strings.Join(job.Targets, ", "), "错误: " + message}, "\n")
	_, _ = ticketsService.Create(tickets.CreateInput{Title: "调度任务失败: " + job.Name, Type: "incident", Priority: "high", Applicant: "scheduler", Description: description, RelatedAsset: target, RelatedAlert: "job-failed-" + job.ID, ProjectGroup: projectGroup, AutomationTemplate: automation})
}
