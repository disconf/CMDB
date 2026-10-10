package aiops

import "strings"

var runbookPolicyRegistry = map[string]RunbookPolicy{
	"rb-generic-triage": {RunbookID: "rb-generic-triage", Title: "通用故障只读排查", TemplateID: "tpl-health", Mode: "read-only", Risk: "low", ApprovalRequired: true},
	"rb-memory-relief":  {RunbookID: "rb-memory-relief", Title: "内存压力排查", TemplateID: "tpl-health", Mode: "read-only", Risk: "low", ApprovalRequired: true},
	"rb-disk-cleanup":   {RunbookID: "rb-disk-cleanup", Title: "磁盘空间治理检查", TemplateID: "tpl-health", Mode: "read-only", Risk: "low", ApprovalRequired: true},
	"rb-latency-check":  {RunbookID: "rb-latency-check", Title: "服务延迟链路检查", TemplateID: "tpl-health", Mode: "read-only", Risk: "low", ApprovalRequired: true},
	"rb-network-path":   {RunbookID: "rb-network-path", Title: "网络链路检查", TemplateID: "tpl-health", Mode: "read-only", Risk: "low", ApprovalRequired: true},
	"rb-pod-diagnose":   {RunbookID: "rb-pod-diagnose", Title: "Kubernetes Pod 检查", TemplateID: "tpl-health", Mode: "read-only", Risk: "low", ApprovalRequired: true},
	"rb-db-slow-query":  {RunbookID: "rb-db-slow-query", Title: "数据库连接与慢查询检查", TemplateID: "tpl-health", Mode: "read-only", Risk: "low", ApprovalRequired: true},
}

func policyFor(runbookID string) (RunbookPolicy, bool) {
	item, ok := runbookPolicyRegistry[strings.TrimSpace(runbookID)]
	return item, ok
}

func (s *Service) Runbooks() []RunbookPolicy {
	result := make([]RunbookPolicy, 0, len(runbookPolicyRegistry))
	for _, item := range runbookPolicyRegistry {
		result = append(result, item)
	}
	return result
}
