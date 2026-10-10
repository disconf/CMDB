package aiops

import (
	"context"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/tickets"
)

func (s *Service) StartLiveRehearsal(ctx context.Context, runbookID, assetID, user string) (Execution, error) {
	runbookID, assetID, user = strings.TrimSpace(runbookID), strings.TrimSpace(assetID), strings.TrimSpace(user)
	if runbookID == "" || assetID == "" || user == "" {
		return Execution{}, ErrValidation
	}
	policy, ok := s.policyFor(runbookID)
	if !ok || policy.Mode != "change" || policy.TemplateID != "tpl-health" || !policy.Enabled {
		return Execution{}, ErrValidation
	}
	if s.tickets == nil || s.assets == nil {
		return Execution{}, ErrNotReady
	}
	asset, err := s.assets.GetAsset(assetID)
	if err != nil {
		return Execution{}, ErrValidation
	}
	ticket, err := s.tickets.Create(tickets.CreateInput{Title: "AIOps 真实通道演练审批：" + policy.Title, Type: "change", Priority: "medium", Applicant: user, Description: "仅执行只读健康检查，验证审批、授权、Runner 与状态回写通道。不会清理、重启或安装补丁。", RelatedAsset: assetID, ProjectGroup: asset.ProjectGroup, AutomationTemplate: policy.RunbookID})
	if err != nil {
		return Execution{}, err
	}
	analysis := Analysis{RelatedAssets: []RelatedAsset{{ID: asset.ID, Name: asset.Name, Environment: asset.Environment, Status: asset.Status, IP: asset.IP}}}
	recommendation := Recommendation{RunbookID: policy.RunbookID, Title: policy.Title, Steps: []string{"执行目标主机只读健康检查", "确认任务未产生任何变更", "回写演练结果和审计记录"}}
	plan := s.buildChangePlan(analysis, recommendation, policy, user, "", false)
	plan.Rehearsal = true
	item := Execution{ID: newID("aiexec"), RunbookID: policy.RunbookID, RunbookTitle: policy.Title, Status: "pending-approval", RequestedBy: user, RequestedAt: time.Now().Format("2006-01-02 15:04:05"), ApprovalRequired: true, TicketID: ticket.ID, ChangePlan: plan, Rehearsal: true, VerificationStatus: "pending", VerificationSummary: "等待只读演练任务完成"}
	if err = s.repo.SaveExecution(ctx, item); err != nil {
		return Execution{}, err
	}
	s.mu.Lock()
	s.executions = append([]Execution{item}, s.executions...)
	s.mu.Unlock()
	return item, nil
}
