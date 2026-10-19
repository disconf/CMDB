package aiops

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

type AgentToolCall struct {
	Tool    string `json:"tool"`
	Summary string `json:"summary"`
}
type AgentEvidence struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
}
type AgentAction struct {
	Type      string `json:"type"`
	Label     string `json:"label"`
	AssetID   string `json:"assetId,omitempty"`
	RunbookID string `json:"runbookId,omitempty"`
}
type AgentAnswer struct {
	Question    string              `json:"question"`
	Answer      string              `json:"answer"`
	Tools       []AgentToolCall     `json:"tools"`
	Evidence    []AgentEvidence     `json:"evidence"`
	Actions     []AgentAction       `json:"actions"`
	Citations   []KnowledgeCitation `json:"citations"`
	GeneratedAt string              `json:"generatedAt"`
}

func (s *Service) AgentQuery(ctx context.Context, question, user string) (AgentAnswer, error) {
	return s.AgentQueryWithActor(ctx, question, KnowledgeActor{Username: user})
}

func (s *Service) AgentQueryWithActor(ctx context.Context, question string, actor KnowledgeActor) (AgentAnswer, error) {
	question = strings.TrimSpace(maskSensitiveText(question))
	if question == "" {
		return AgentAnswer{}, ErrValidation
	}
	answer := AgentAnswer{Question: question, Tools: []AgentToolCall{}, Evidence: []AgentEvidence{}, Actions: []AgentAction{}, Citations: []KnowledgeCitation{}, GeneratedAt: time.Now().Format("2006-01-02 15:04:05")}
	lower := strings.ToLower(question)
	switch {
	case containsAny(lower, "多少台", "数量", "统计", "几台") && containsAny(lower, "物理机", "物理服务器", "服务器", "虚拟机", "云主机"):
		s.agentAssetStats(&answer, lower)
	case containsAny(lower, "机房", "位置", "区域") && containsAny(lower, "分布", "在哪", "多少", "哪些"):
		s.agentLocationStats(&answer, lower)
	case containsAny(lower, "告警", "故障", "异常"):
		s.agentAlertSummary(ctx, &answer, actor)
	default:
		s.agentAssetLookup(&answer, question, actor)
	}
	return answer, nil
}

func (s *Service) allAgentAssets() []cmdb.Asset {
	if s.assets == nil {
		return []cmdb.Asset{}
	}
	items := []cmdb.Asset{}
	for page := 1; ; page++ {
		result := s.assets.ListAssets(cmdb.AssetQuery{Page: page, PageSize: 500, ScopeAll: true})
		items = append(items, result.Data...)
		if result.Meta.TotalPages == 0 || page >= result.Meta.TotalPages {
			break
		}
	}
	return items
}

func (s *Service) agentAssetStats(answer *AgentAnswer, lower string) {
	all := s.allAgentAssets()
	types := agentAssetTypes(lower)
	items := []cmdb.Asset{}
	for _, item := range all {
		if len(types) == 0 || containsString(types, item.Type) {
			items = append(items, item)
		}
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.TypeName]++
	}
	answer.Tools = append(answer.Tools, AgentToolCall{Tool: "cmdb.assets.list", Summary: fmt.Sprintf("匹配 %d 台资产", len(items))})
	parts := []string{}
	for key, value := range counts {
		parts = append(parts, fmt.Sprintf("%s %d 台", key, value))
	}
	sort.Strings(parts)
	if len(parts) > 0 {
		answer.Answer = "当前匹配资产统计：" + strings.Join(parts, "，") + "。"
	} else {
		answer.Answer = "未找到匹配该类型的资产。"
	}
	for _, item := range items {
		if len(answer.Evidence) >= 20 {
			break
		}
		answer.Evidence = append(answer.Evidence, AgentEvidence{Type: item.TypeName, Name: item.Name, Detail: item.IP + " · " + item.Location})
	}
	answer.Actions = append(answer.Actions, AgentAction{Type: "navigate", Label: "打开 CMDB 资产清单"})
}

func (s *Service) agentLocationStats(answer *AgentAnswer, lower string) {
	all := s.allAgentAssets()
	types := agentAssetTypes(lower)
	items := []cmdb.Asset{}
	for _, item := range all {
		if len(types) == 0 || containsString(types, item.Type) {
			items = append(items, item)
		}
	}
	counts := map[string]int{}
	for _, item := range items {
		location := item.Location
		if strings.TrimSpace(location) == "" {
			location = "未设置位置"
		}
		counts[location]++
	}
	answer.Tools = append(answer.Tools, AgentToolCall{Tool: "cmdb.assets.list", Summary: fmt.Sprintf("按位置聚合 %d 台资产", len(items))})
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s %d 台", key, counts[key]))
	}
	if len(parts) > 0 {
		answer.Answer = "当前资产位置分布：" + strings.Join(parts, "，") + "。"
	} else {
		answer.Answer = "未找到匹配资产的机房位置。"
	}
}

func agentAssetTypes(question string) []string {
	switch {
	case strings.Contains(question, "物理机") || strings.Contains(question, "物理服务器"):
		return []string{"physical-server"}
	case strings.Contains(question, "虚拟机"):
		return []string{"virtual-machine"}
	case strings.Contains(question, "云主机"):
		return []string{"cloud-host"}
	case strings.Contains(question, "k8s") || strings.Contains(question, "kubernetes"):
		return []string{"k8s-cluster", "k8s-node"}
	case strings.Contains(question, "服务器"):
		return []string{"physical-server", "virtual-machine", "cloud-host"}
	default:
		return nil
	}
}

func (s *Service) agentAssetLookup(answer *AgentAnswer, question string, actor KnowledgeActor) {
	page := cmdb.AssetPage{}
	if s.assets != nil {
		page = s.assets.ListAssets(cmdb.AssetQuery{Search: question, Page: 1, PageSize: 5, ScopeAll: true})
	}
	if len(page.Data) == 0 {
		s.agentKnowledgeFallback(answer, question, actor)
		return
	}
	asset := page.Data[0]
	answer.Tools = append(answer.Tools, AgentToolCall{Tool: "cmdb.assets.search", Summary: "匹配资产 " + asset.Name})
	answer.Evidence = append(answer.Evidence, AgentEvidence{Type: asset.TypeName, Name: asset.Name, Detail: asset.IP + " · " + asset.Environment + " · " + asset.Location + " · 负责人 " + asset.Owner})
	attributes := []string{}
	for _, attr := range asset.Attributes {
		if attr.Value != "" {
			attributes = append(attributes, attr.Label+"="+attr.Value)
		}
	}
	answer.Answer = fmt.Sprintf("找到资产《%s》：类型 %s，IP %s，环境 %s，项目组 %s，负责人 %s，位置 %s。", asset.Name, asset.TypeName, asset.IP, asset.Environment, asset.ProjectGroup, asset.Owner, asset.Location)
	if len(attributes) > 0 {
		answer.Answer += " 关键配置：" + strings.Join(attributes, "；") + "。"
	}
	answer.Actions = append(answer.Actions, AgentAction{Type: "asset", Label: "查看资产详情", AssetID: asset.ID})
}

func (s *Service) agentAlertSummary(ctx context.Context, answer *AgentAnswer, actor KnowledgeActor) {
	alerts := s.Alerts()
	counts := map[string]int{}
	for _, alert := range alerts {
		counts[alert.Severity]++
	}
	answer.Tools = append(answer.Tools, AgentToolCall{Tool: "monitor.alerts.list", Summary: fmt.Sprintf("读取 %d 条未恢复告警", len(alerts))})
	if len(alerts) == 0 {
		answer.Answer = "当前没有未恢复告警。"
		return
	}
	answer.Answer = fmt.Sprintf("当前共有 %d 条未恢复告警，其中 critical %d 条、warning %d 条。", len(alerts), counts["critical"], counts["warning"])
	for index, alert := range alerts {
		if index >= 5 {
			break
		}
		detail := alert.Title + "：" + alert.Message + "（目标 " + alert.Target + "）"
		asset, ok := s.resolveAsset(alert)
		if ok {
			detail += "；影响资产 " + asset.Name + "（" + asset.Location + "）"
		}
		answer.Evidence = append(answer.Evidence, AgentEvidence{Type: "告警", Name: alert.Title, Detail: detail})
	}
	knowledge, err := s.AskKnowledgeWithActor("告警 "+alerts[0].Title+" 影响 解决方案", 3, actor)
	if err == nil {
		answer.Citations = knowledge.Citations
		if len(answer.Citations) > 0 {
			answer.Answer += " 建议参考知识：" + answer.Citations[0].Title + " [" + answer.Citations[0].KnowledgeID + "]。"
		}
	}
	answer.Actions = append(answer.Actions, AgentAction{Type: "navigate", Label: "打开监控告警"})
}

func (s *Service) agentKnowledgeFallback(answer *AgentAnswer, question string, actor KnowledgeActor) {
	knowledge, err := s.AskKnowledgeWithActor(question, 5, actor)
	if err != nil {
		answer.Answer = "暂未找到匹配资产或知识，请提供更具体的资产名称、IP、告警名称或故障现象。"
		return
	}
	answer.Answer = knowledge.Answer
	answer.Citations = knowledge.Citations
	answer.Tools = append(answer.Tools, AgentToolCall{Tool: "aiops.knowledge.search", Summary: fmt.Sprintf("匹配 %d 条知识", len(knowledge.Citations))})
}
