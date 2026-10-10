package aiops

import (
	"sort"
	"strings"
)

func defaultRunbookPolicies() []RunbookPolicy {
	return []RunbookPolicy{
		{RunbookID: "rb-generic-triage", Title: "通用故障只读排查", TemplateID: "tpl-health", Mode: "read-only", Risk: "low", ApprovalRequired: true, Enabled: true, MaxTargets: 1, AllowedEnvironments: []string{"*"}, ChangeWindow: "*"},
		{RunbookID: "rb-memory-relief", Title: "内存压力排查", TemplateID: "tpl-health", Mode: "read-only", Risk: "low", ApprovalRequired: true, Enabled: true, MaxTargets: 1, AllowedEnvironments: []string{"*"}, ChangeWindow: "*"},
		{RunbookID: "rb-disk-cleanup", Title: "磁盘空间治理检查", TemplateID: "tpl-health", Mode: "read-only", Risk: "low", ApprovalRequired: true, Enabled: true, MaxTargets: 1, AllowedEnvironments: []string{"*"}, ChangeWindow: "*"},
		{RunbookID: "rb-latency-check", Title: "服务延迟链路检查", TemplateID: "tpl-health", Mode: "read-only", Risk: "low", ApprovalRequired: true, Enabled: true, MaxTargets: 1, AllowedEnvironments: []string{"*"}, ChangeWindow: "*"},
		{RunbookID: "rb-network-path", Title: "网络链路检查", TemplateID: "tpl-health", Mode: "read-only", Risk: "low", ApprovalRequired: true, Enabled: true, MaxTargets: 1, AllowedEnvironments: []string{"*"}, ChangeWindow: "*"},
		{RunbookID: "rb-pod-diagnose", Title: "Kubernetes Pod 检查", TemplateID: "tpl-health", Mode: "read-only", Risk: "low", ApprovalRequired: true, Enabled: true, MaxTargets: 1, AllowedEnvironments: []string{"*"}, ChangeWindow: "*"},
		{RunbookID: "rb-db-slow-query", Title: "数据库连接与慢查询检查", TemplateID: "tpl-health", Mode: "read-only", Risk: "low", ApprovalRequired: true, Enabled: true, MaxTargets: 1, AllowedEnvironments: []string{"*"}, ChangeWindow: "*"},
		{RunbookID: "rb-log-cleanup", Title: "日志空间清理（变更类）", TemplateID: "tpl-clean", Mode: "change", Risk: "medium", ApprovalRequired: true, Enabled: false, MaxTargets: 1, AllowedEnvironments: []string{"qa", "staging"}, ChangeWindow: "qa=*;staging=Mon-Fri,18:00-23:59"},
		{RunbookID: "rb-service-restart", Title: "应用滚动重启（变更类）", TemplateID: "tpl-restart", Mode: "change", Risk: "medium", ApprovalRequired: true, Enabled: false, MaxTargets: 1, AllowedEnvironments: []string{"qa", "staging"}, ChangeWindow: "qa=*;staging=Mon-Fri,18:00-23:59"},
		{RunbookID: "rb-security-patch", Title: "安全补丁安装（高风险）", TemplateID: "tpl-patch", Mode: "change", Risk: "high", ApprovalRequired: true, Enabled: false, MaxTargets: 1, AllowedEnvironments: []string{"production"}, ChangeWindow: "production=Sat-Sun"},
	}
}

func (s *Service) policyFor(runbookID string) (RunbookPolicy, bool) {
	id := strings.TrimSpace(runbookID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.runbooks {
		if item.RunbookID == id {
			return item, true
		}
	}
	return RunbookPolicy{}, false
}

func (s *Service) Runbooks() []RunbookPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]RunbookPolicy(nil), s.runbooks...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Mode != result[j].Mode {
			return result[i].Mode < result[j].Mode
		}
		return result[i].RunbookID < result[j].RunbookID
	})
	return result
}
