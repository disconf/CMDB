package aiops

import (
	"strings"
	"time"
)

func (s *Service) buildChangePlan(analysis Analysis, recommendation Recommendation, policy RunbookPolicy, user, sourceExecutionID string, rollback bool) ChangePlan {
	targetID, targetName, environment := "", "", ""
	if len(analysis.RelatedAssets) > 0 {
		target := analysis.RelatedAssets[0]
		targetID, targetName, environment = target.ID, target.Name, target.Environment
	}
	rollbackName := ""
	if rollbackPolicy, ok := s.policyFor(policy.RollbackRunbookID); ok {
		rollbackName = rollbackPolicy.Title
	}
	plan := ChangePlan{
		TargetAssetID:       targetID,
		TargetAssetName:     targetName,
		Environment:         environment,
		RunbookID:           policy.RunbookID,
		RunbookTitle:        policy.Title,
		ExecutionMode:       policy.ExecutionMode,
		ChangeWindow:        policy.ChangeWindow,
		RollbackRunbookID:   policy.RollbackRunbookID,
		RollbackRunbookName: rollbackName,
		SourceExecutionID:   sourceExecutionID,
		Preconditions:       []string{"完成执行预检", "工单审批通过", "一次性执行授权有效", "目标环境与变更窗口匹配"},
		Verification:        []string{"任务执行状态为 success", "目标主机监控状态为 Up", "关联告警在验证窗口内恢复"},
		PlannedBy:           strings.TrimSpace(user),
		PlannedAt:           time.Now().Format("2006-01-02 15:04:05"),
	}
	if rollback {
		plan.Steps = []ChangePlanStep{
			{Order: 1, Title: "冻结变更", Detail: "停止后续变更并确认故障影响范围"},
			{Order: 2, Title: "执行回滚", Detail: "按回滚 Runbook 恢复目标到变更前状态"},
			{Order: 3, Title: "恢复验证", Detail: "验证服务、主机和关联告警恢复正常"},
		}
		plan.RollbackPlan = []string{"回滚操作由独立工单审批和一次性授权控制", "回滚失败时升级人工应急流程"}
	} else {
		for index, step := range recommendation.Steps {
			plan.Steps = append(plan.Steps, ChangePlanStep{Order: index + 1, Title: step, Detail: "按 Runbook 白名单步骤执行"})
		}
		if len(plan.Steps) == 0 {
			plan.Steps = []ChangePlanStep{{Order: 1, Title: "执行 Runbook", Detail: "按注册中心绑定的任务模板执行"}}
		}
		if policy.RequireRollback && strings.TrimSpace(policy.RollbackRunbookID) != "" {
			plan.RollbackPlan = []string{"触发回滚 Runbook：" + rollbackName, "回滚仍需独立审批", "验证回滚后的任务和监控状态"}
		} else {
			plan.RollbackPlan = []string{"未要求自动回滚，执行前由审批人确认人工恢复方案"}
		}
	}
	return plan
}
