package aiops

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/releases"
	"cmdb/gateway-bff/internal/tickets"
)

func (s *Service) HandleTicketDecision(ctx context.Context, ticket tickets.Ticket, authorizationToken, decision string) {
	execution, ok := s.findExecutionByTicket(ticket.ID)
	if !ok || execution.Status != "pending-approval" {
		return
	}
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "reject":
		execution.Status = "rejected"
		execution.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
		execution.ResultSummary = "审批已驳回，未创建任务"
		_ = s.saveExecution(ctx, execution)
		return
	case "approve":
	default:
		return
	}
	policy, ok := s.policyFor(execution.RunbookID)
	if !ok {
		s.failExecution(ctx, execution, "Runbook 不在白名单")
		return
	}
	if !policy.Enabled {
		s.failExecution(ctx, execution, "Runbook 已禁用")
		return
	}
	if policy.Mode == "change" && policy.ExecutionMode == "live" && !strings.EqualFold(strings.TrimSpace(os.Getenv("AIOPS_ALLOW_CHANGE_RUNBOOKS")), "true") {
		s.failExecution(ctx, execution, "变更类 Runbook 全局开关未启用")
		return
	}
	if s.tickets == nil || s.jobs == nil {
		s.failExecution(ctx, execution, "任务执行依赖未配置")
		return
	}
	if _, err := s.tickets.ConsumeTaskAuthorization(ticket.ID, authorizationToken, execution.RunbookID); err != nil {
		s.failExecution(ctx, execution, "一次性授权校验失败："+err.Error())
		return
	}
	analysis, err := s.Get(execution.AnalysisID)
	if err != nil {
		s.failExecution(ctx, execution, "分析记录不存在")
		return
	}
	targets := []string{}
	for _, asset := range analysis.RelatedAssets {
		if strings.TrimSpace(asset.ID) != "" {
			targets = append(targets, asset.ID)
			break
		}
	}
	if len(targets) == 0 || len(targets) > policy.MaxTargets {
		s.failExecution(ctx, execution, "执行目标数量不符合 Runbook 限制")
		return
	}
	targetEnvironment := ""
	if len(analysis.RelatedAssets) > 0 {
		targetEnvironment = analysis.RelatedAssets[0].Environment
	}
	if len(policy.AllowedEnvironments) > 0 && !containsString(policy.AllowedEnvironments, "*") && !containsString(policy.AllowedEnvironments, targetEnvironment) {
		s.failExecution(ctx, execution, "目标环境不在 Runbook 允许范围")
		return
	}
	if allowed, message := releases.ChangeWindowAllowed(targetEnvironment, policy.ChangeWindow, time.Now()); !allowed {
		s.failExecution(ctx, execution, "当前不在变更窗口："+message)
		return
	}
	if policy.Mode == "change" && policy.ExecutionMode == "live" {
		if policy.RequireRollback {
			rollback, ok := s.policyFor(policy.RollbackRunbookID)
			if !ok || !rollback.Enabled || rollback.Mode != "change" || rollback.ExecutionMode != "live" {
				s.failExecution(ctx, execution, "真实执行前回滚 Runbook 未就绪")
				return
			}
		}
		count, last := s.runbookLiveUsage(policy.RunbookID, time.Now())
		if policy.LiveDailyLimit > 0 && count > policy.LiveDailyLimit {
			s.failExecution(ctx, execution, "超过 Runbook 每日真实执行上限")
			return
		}
		if policy.LiveCooldownMinutes > 0 && !last.IsZero() {
			next := last.Add(time.Duration(policy.LiveCooldownMinutes) * time.Minute)
			if time.Now().Before(next) {
				s.failExecution(ctx, execution, "距离上次真实执行未满足冷却时间，可执行时间："+next.Format("2006-01-02 15:04:05"))
				return
			}
		}
	}
	if policy.Mode == "change" && policy.ExecutionMode == "simulate" {
		execution.Status = "success"
		execution.Simulated = true
		execution.ApprovedBy = ticket.ID
		execution.ApprovedAt = time.Now().Format("2006-01-02 15:04:05")
		execution.FinishedAt = execution.ApprovedAt
		execution.ResultSummary = "变更演练完成，未创建真实任务、未修改目标主机"
		_ = s.saveExecution(ctx, execution)
		return
	}
	job, err := s.jobs.Create(jobs.CreateInput{
		TemplateID:  policy.TemplateID,
		Targets:     targets,
		Operator:    "aiops:" + ticket.Applicant,
		TicketID:    ticket.ID,
		Parameters:  map[string]string{"aiops_analysis_id": analysis.ID, "runbook_id": execution.RunbookID},
		PreApproved: true,
	})
	if err != nil {
		s.failExecution(ctx, execution, "创建任务失败："+err.Error())
		return
	}
	job, err = s.jobs.Run(job.ID)
	if err != nil {
		s.failExecution(ctx, execution, "启动任务失败："+err.Error())
		return
	}
	execution.Status = "running"
	execution.JobID = job.ID
	execution.ApprovedBy = ticket.ID
	execution.ApprovedAt = time.Now().Format("2006-01-02 15:04:05")
	execution.ResultSummary = fmt.Sprintf("已创建白名单任务 %s（%s）", job.Name, job.ID)
	_ = s.saveExecution(ctx, execution)
}

func (s *Service) OnJobTerminal(job jobs.Job) {
	execution, ok := s.findExecutionByJob(job.ID)
	if !ok {
		return
	}
	execution.Status = job.Status
	execution.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
	execution.ResultSummary = fmt.Sprintf("任务 %s 状态 %s，进度 %d%%", job.Name, job.Status, job.Progress)
	for _, result := range job.TargetResults {
		if result.Error != "" {
			execution.Error = result.Error
			break
		}
		if result.Stderr != "" {
			execution.Error = result.Stderr
			break
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.saveExecution(ctx, execution)
}

func (s *Service) RequestRollback(ctx context.Context, executionID, user string) (Execution, error) {
	executionID = strings.TrimSpace(executionID)
	user = strings.TrimSpace(user)
	if executionID == "" || user == "" {
		return Execution{}, ErrValidation
	}
	original, ok := s.findExecutionByID(executionID)
	if !ok {
		return Execution{}, ErrNotFound
	}
	if original.Status != "failed" || original.Rollback {
		return Execution{}, ErrValidation
	}
	policy, ok := s.policyFor(original.RunbookID)
	if !ok || strings.TrimSpace(policy.RollbackRunbookID) == "" {
		return Execution{}, ErrValidation
	}
	rollback, ok := s.policyFor(policy.RollbackRunbookID)
	if !ok || rollback.Mode != "change" || !rollback.Enabled {
		return Execution{}, ErrValidation
	}
	if rollback.ExecutionMode == "live" && !strings.EqualFold(strings.TrimSpace(os.Getenv("AIOPS_ALLOW_CHANGE_RUNBOOKS")), "true") {
		return Execution{}, ErrChangeDisabled
	}
	if s.tickets == nil {
		return Execution{}, ErrNotReady
	}
	analysis, err := s.Get(original.AnalysisID)
	if err != nil {
		return Execution{}, err
	}
	s.mu.RLock()
	for _, item := range s.executions {
		if item.ParentExecutionID == original.ID && (item.Status == "pending-approval" || item.Status == "running" || item.Status == "success") {
			s.mu.RUnlock()
			return Execution{}, ErrValidation
		}
	}
	s.mu.RUnlock()
	assetID, projectGroup := "", ""
	if len(analysis.RelatedAssets) > 0 {
		assetID = analysis.RelatedAssets[0].ID
		projectGroup = analysis.RelatedAssets[0].ProjectGroup
	}
	ticket, err := s.tickets.Create(tickets.CreateInput{
		Title:              "AIOps 回滚执行审批：" + rollback.Title,
		Type:               "change",
		Priority:           "high",
		Applicant:          user,
		Description:        "AIOps 执行 " + original.ID + " 失败，申请执行回滚 Runbook " + rollback.RunbookID + "。回滚仍需人工审批，平台不会自动操作服务器。",
		RelatedAsset:       assetID,
		RelatedAlert:       analysis.AlertID,
		ProjectGroup:       projectGroup,
		AutomationTemplate: rollback.RunbookID,
	})
	if err != nil {
		return Execution{}, err
	}
	item := Execution{ID: newID("aiexec"), AnalysisID: analysis.ID, RunbookID: rollback.RunbookID, RunbookTitle: rollback.Title, Status: "pending-approval", RequestedBy: user, RequestedAt: time.Now().Format("2006-01-02 15:04:05"), ApprovalRequired: true, TicketID: ticket.ID, Rollback: true, ParentExecutionID: original.ID}
	if err = s.repo.SaveExecution(ctx, item); err != nil {
		return Execution{}, err
	}
	s.mu.Lock()
	s.executions = append([]Execution{item}, s.executions...)
	s.mu.Unlock()
	return item, nil
}

func (s *Service) failExecution(ctx context.Context, execution Execution, message string) {
	execution.Status = "failed"
	execution.Error = message
	execution.FinishedAt = time.Now().Format("2006-01-02 15:04:05")
	execution.ResultSummary = "审批通过，但受控任务创建或启动失败"
	_ = s.saveExecution(ctx, execution)
}

func (s *Service) saveExecution(ctx context.Context, execution Execution) error {
	if err := s.repo.SaveExecution(ctx, execution); err != nil {
		return err
	}
	s.mu.Lock()
	for i := range s.executions {
		if s.executions[i].ID == execution.ID {
			s.executions[i] = execution
			s.mu.Unlock()
			return nil
		}
	}
	s.executions = append([]Execution{execution}, s.executions...)
	s.mu.Unlock()
	return nil
}

func (s *Service) findExecutionByTicket(ticketID string) (Execution, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.executions {
		if item.TicketID == ticketID {
			return item, true
		}
	}
	return Execution{}, false
}

func (s *Service) findExecutionByID(executionID string) (Execution, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.executions {
		if item.ID == executionID {
			return item, true
		}
	}
	return Execution{}, false
}

func (s *Service) findExecutionByJob(jobID string) (Execution, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.executions {
		if item.JobID == jobID {
			return item, true
		}
	}
	return Execution{}, false
}
