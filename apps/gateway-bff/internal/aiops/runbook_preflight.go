package aiops

import (
	"fmt"
	"os"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/releases"
)

func (s *Service) RunbookPreflight(runbookID, analysisID string) (RunbookPreflight, error) {
	runbookID = strings.TrimSpace(runbookID)
	analysisID = strings.TrimSpace(analysisID)
	policy, ok := s.policyFor(runbookID)
	if !ok {
		return RunbookPreflight{}, ErrRunbookNotFound
	}
	result := RunbookPreflight{RunbookID: runbookID, AnalysisID: analysisID, Checks: []RunbookPreflightCheck{}, GeneratedAt: time.Now().Format("2006-01-02 15:04:05")}
	failed := false
	nextAllowed := time.Time{}
	add := func(key, label, status, detail string) {
		result.Checks = append(result.Checks, RunbookPreflightCheck{Key: key, Label: label, Status: status, Detail: detail})
		if status == "fail" {
			failed = true
		}
	}

	if !policy.Enabled {
		add("policy-enabled", "Runbook 状态", "fail", "Runbook 当前未启用")
	} else {
		add("policy-enabled", "Runbook 状态", "pass", "Runbook 已启用")
	}

	if s.jobs == nil {
		add("template", "任务模板", "fail", "任务服务未就绪，无法校验模板")
	} else {
		templateFound, templateEnabled := false, false
		for _, template := range s.jobs.Templates() {
			if template.ID == policy.TemplateID {
				templateFound = true
				templateEnabled = template.Enabled
				break
			}
		}
		switch {
		case !templateFound:
			add("template", "任务模板", "fail", fmt.Sprintf("模板 %s 不存在", policy.TemplateID))
		case !templateEnabled:
			add("template", "任务模板", "fail", fmt.Sprintf("模板 %s 已禁用", policy.TemplateID))
		default:
			add("template", "任务模板", "pass", fmt.Sprintf("模板 %s 可用", policy.TemplateID))
		}
	}

	if policy.ExecutionMode == "simulate" {
		add("execution-mode", "执行方式", "pass", "演练模式不创建真实任务，不占用真实执行配额")
	} else if policy.Mode == "change" && !strings.EqualFold(getenv("AIOPS_ALLOW_CHANGE_RUNBOOKS"), "true") {
		add("execution-mode", "执行方式", "fail", "真实变更全局开关未启用")
	} else {
		add("execution-mode", "执行方式", "pass", "真实执行门禁已满足")
	}
	if policy.Mode == "change" && policy.ExecutionMode == "live" {
		if blocked, message := s.changeControlBlocked(time.Now()); blocked {
			add("change-control", "变更冻结", "fail", message)
		} else {
			add("change-control", "变更冻结", "pass", "平台未冻结变更且真实变更总开关已开启")
		}
	}

	if analysisID == "" {
		add("analysis", "分析目标", "warn", "未选择分析记录，本次只检查策略级条件")
	} else if analysis, err := s.Get(analysisID); err != nil {
		add("analysis", "分析目标", "fail", "分析记录不存在")
	} else {
		targetCount := 0
		for _, asset := range analysis.RelatedAssets {
			if strings.TrimSpace(asset.ID) != "" {
				targetCount++
			}
		}
		if targetCount == 0 || targetCount > policy.MaxTargets {
			add("analysis", "分析目标", "fail", fmt.Sprintf("目标数 %d，策略允许 1-%d", targetCount, policy.MaxTargets))
		} else {
			add("analysis", "分析目标", "pass", fmt.Sprintf("目标数 %d，符合策略限制", targetCount))
		}
		environment := ""
		if len(analysis.RelatedAssets) > 0 {
			environment = analysis.RelatedAssets[0].Environment
		}
		if len(policy.AllowedEnvironments) > 0 && !containsString(policy.AllowedEnvironments, "*") && !containsString(policy.AllowedEnvironments, environment) {
			add("environment", "目标环境", "fail", fmt.Sprintf("环境 %q 不在允许范围", environment))
		} else {
			add("environment", "目标环境", "pass", fmt.Sprintf("环境 %q 允许执行", environment))
		}
		if allowed, message := releases.ChangeWindowAllowed(environment, policy.ChangeWindow, time.Now()); !allowed {
			add("change-window", "变更窗口", "fail", "当前不在变更窗口："+message)
		} else {
			add("change-window", "变更窗口", "pass", "当前处于允许的变更窗口")
		}
	}

	if policy.Mode != "change" || !policy.RequireRollback {
		add("rollback", "回滚就绪", "pass", "当前策略不要求回滚 Runbook")
	} else if policy.ExecutionMode != "live" {
		add("rollback", "回滚就绪", "warn", "演练模式可执行，但切换真实执行前必须配置并启用回滚 Runbook")
	} else if policy.RollbackRunbookID == "" {
		add("rollback", "回滚就绪", "fail", "未配置回滚 Runbook")
	} else if rollback, ok := s.policyFor(policy.RollbackRunbookID); !ok {
		add("rollback", "回滚就绪", "fail", "回滚 Runbook 不存在")
	} else if !rollback.Enabled {
		add("rollback", "回滚就绪", "fail", "回滚 Runbook 未启用")
	} else if policy.ExecutionMode == "live" && rollback.ExecutionMode != "live" {
		add("rollback", "回滚就绪", "fail", "真实执行要求回滚 Runbook 也使用真实执行模式")
	} else {
		add("rollback", "回滚就绪", "pass", "回滚 Runbook 已就绪："+rollback.Title)
	}

	if policy.Mode == "change" && policy.ExecutionMode == "live" {
		count, last := s.runbookLiveUsage(policy.RunbookID, time.Now())
		if policy.LiveDailyLimit > 0 {
			if count >= policy.LiveDailyLimit {
				next := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day()+1, 0, 0, 0, 0, time.Now().Location())
				if next.After(nextAllowed) {
					nextAllowed = next
				}
				add("daily-limit", "每日执行额度", "fail", fmt.Sprintf("今日已占用 %d/%d 次", count, policy.LiveDailyLimit))
			} else {
				add("daily-limit", "每日执行额度", "pass", fmt.Sprintf("今日已占用 %d/%d 次", count, policy.LiveDailyLimit))
			}
		} else {
			add("daily-limit", "每日执行额度", "pass", "未设置每日上限")
		}
		if policy.LiveCooldownMinutes > 0 && !last.IsZero() {
			next := last.Add(time.Duration(policy.LiveCooldownMinutes) * time.Minute)
			remaining := time.Until(next)
			if remaining > 0 {
				if next.After(nextAllowed) {
					nextAllowed = next
				}
				add("cooldown", "执行冷却", "fail", fmt.Sprintf("距离上次真实执行不足 %d 分钟，还需等待 %d 分钟", policy.LiveCooldownMinutes, int(remaining.Minutes())+1))
			} else {
				add("cooldown", "执行冷却", "pass", "冷却时间已满足")
			}
		} else {
			add("cooldown", "执行冷却", "pass", "未设置冷却限制或暂无历史执行")
		}
	} else {
		add("daily-limit", "每日执行额度", "pass", "非真实变更执行，不占用真实执行配额")
		add("cooldown", "执行冷却", "pass", "当前执行方式无需执行冷却")
	}

	result.Ready = !failed
	if !nextAllowed.IsZero() {
		result.NextAllowedAt = nextAllowed.Format("2006-01-02 15:04:05")
	}
	return result, nil
}

func (s *Service) runbookLiveUsage(runbookID string, now time.Time) (int, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	var last time.Time
	for _, item := range s.executions {
		if item.Simulated || item.RunbookID != runbookID || item.Status == "rejected" {
			continue
		}
		requestedAt, err := parseAITime(item.RequestedAt)
		if err == nil && requestedAt.Year() == now.Year() && requestedAt.YearDay() == now.YearDay() {
			count++
		}
		if item.Status != "running" && item.Status != "success" && item.Status != "failed" {
			continue
		}
		timestamp := item.RequestedAt
		if strings.TrimSpace(item.FinishedAt) != "" {
			timestamp = item.FinishedAt
		}
		if parsed, parseErr := parseAITime(timestamp); parseErr == nil && parsed.After(last) {
			last = parsed
		}
	}
	return count, last
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
