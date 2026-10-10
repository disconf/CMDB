package aiops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/jobs"
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
	policy, ok := policyFor(execution.RunbookID)
	if !ok {
		s.failExecution(ctx, execution, "Runbook 不在白名单")
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
	if len(analysis.RelatedAssets) == 0 || strings.TrimSpace(analysis.RelatedAssets[0].ID) == "" {
		s.failExecution(ctx, execution, "分析未关联可执行资产")
		return
	}
	job, err := s.jobs.Create(jobs.CreateInput{
		TemplateID:  policy.TemplateID,
		Targets:     []string{analysis.RelatedAssets[0].ID},
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
