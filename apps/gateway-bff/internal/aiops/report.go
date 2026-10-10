package aiops

import (
	"fmt"
	"strings"
	"time"
)

func (s *Service) Report(analysisID string) (Report, error) {
	item, err := s.Get(analysisID)
	if err != nil {
		return Report{}, err
	}
	executions := []Execution{}
	for _, execution := range s.Executions() {
		if execution.AnalysisID == item.ID {
			executions = append(executions, execution)
		}
	}
	report := Report{Analysis: item, Executions: executions, GeneratedAt: time.Now().Format("2006-01-02 15:04:05")}
	report.Markdown = renderReport(report)
	return report, nil
}

func renderReport(report Report) string {
	item := report.Analysis
	var builder strings.Builder
	builder.WriteString("# 故障分析报告\n\n")
	builder.WriteString(fmt.Sprintf("- 分析编号：%s\n", item.ID))
	builder.WriteString(fmt.Sprintf("- 告警：%s\n", item.AlertTitle))
	builder.WriteString(fmt.Sprintf("- 根因假设：%s\n", item.RootCause))
	builder.WriteString(fmt.Sprintf("- 置信度：%d%%\n", item.Confidence))
	builder.WriteString(fmt.Sprintf("- 严重级别：%s\n", item.Severity))
	builder.WriteString(fmt.Sprintf("- 报告生成时间：%s\n\n", report.GeneratedAt))
	builder.WriteString("## 分析摘要\n\n")
	builder.WriteString(item.Summary + "\n\n")
	if item.Feedback != nil {
		builder.WriteString("## 根因反馈\n\n")
		builder.WriteString(fmt.Sprintf("- 状态：%s\n- 反馈人：%s\n- 反馈时间：%s\n- 修正根因：%s\n- 说明：%s\n\n", item.Feedback.Status, item.Feedback.Reviewer, item.Feedback.ReviewedAt, item.Feedback.CorrectedRootCause, item.Feedback.Note))
	}
	builder.WriteString("## 归因证据\n\n")
	builder.WriteString("| 类型 | 来源 | 证据 | 权重 |\n|---|---|---|---:|\n")
	for _, evidence := range item.Evidence {
		builder.WriteString(fmt.Sprintf("| %s | %s | %s | %d%% |\n", evidence.Type, evidence.Source, escapeMarkdownTable(evidence.Title+"："+evidence.Detail), evidence.Weight))
	}
	builder.WriteString("\n## 关联资产\n\n")
	if len(item.RelatedAssets) == 0 {
		builder.WriteString("未匹配到 CMDB 资产。\n\n")
	}
	for _, asset := range item.RelatedAssets {
		builder.WriteString(fmt.Sprintf("- %s（%s，IP %s，环境 %s，负责人 %s，位置 %s）\n", asset.Name, asset.Type, asset.IP, asset.Environment, asset.Owner, asset.Location))
	}
	builder.WriteString("\n## 拓扑影响\n\n")
	builder.WriteString(fmt.Sprintf("- 严重：%d\n- 关注：%d\n- 健康：%d\n- 合计：%d\n\n", item.Impact.Critical, item.Impact.Warning, item.Impact.Healthy, item.Impact.Total))
	for _, affected := range item.Impact.Items {
		builder.WriteString("- " + affected + "\n")
	}
	builder.WriteString("\n## 时间线\n\n")
	for _, timeline := range item.Timeline {
		builder.WriteString(fmt.Sprintf("- %s [%s] %s\n", timeline.Time, timeline.Source, timeline.Message))
	}
	builder.WriteString("\n## Runbook 建议\n\n")
	for _, recommendation := range item.Recommendations {
		builder.WriteString(fmt.Sprintf("### %s\n\n- 风险：%s\n- 影响：%s\n- 预计耗时：%s\n\n", recommendation.Title, recommendation.Risk, recommendation.Impact, recommendation.Duration))
		for _, step := range recommendation.Steps {
			builder.WriteString("- " + step + "\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("## 受控执行记录\n\n")
	if len(report.Executions) == 0 {
		builder.WriteString("暂无执行申请。\n")
	}
	for _, execution := range report.Executions {
		builder.WriteString(fmt.Sprintf("- %s：状态 %s，工单 %s，任务 %s，结果 %s，错误 %s\n", execution.RunbookTitle, execution.Status, execution.TicketID, execution.JobID, execution.ResultSummary, execution.Error))
	}
	return builder.String()
}

func escapeMarkdownTable(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}
