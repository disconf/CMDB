package aiops

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/releases"
	"cmdb/gateway-bff/internal/tickets"
	"cmdb/gateway-bff/internal/toolbox"
	"cmdb/gateway-bff/internal/topology"
)

type fakeAlerts struct{}

func (fakeAlerts) Alerts() []monitor.Alert {
	return []monitor.Alert{{ID: "alert-1", Title: "主机磁盘空间不足", Severity: "critical", Status: "firing", TargetID: "node-exporter-172-28-68-77", Target: "rds03.qa.bcavt.com", Rule: "DiskUsage > 90%", StartedAt: "2026-09-21 09:00:00", Duration: "30 分钟", Owner: "值班组", Message: "根分区使用率达到 91%"}}
}
func (fakeAlerts) Events(string) ([]monitor.AlertEvent, error) {
	return []monitor.AlertEvent{{ID: 1, AlertID: "alert-1", Type: "notification", Operator: "system", Message: "已通知负责人", CreatedAt: "2026-09-21 09:02:00"}}, nil
}
func (fakeAlerts) Host(string) monitor.HostMonitoring {
	return monitor.HostMonitoring{AssetID: "asset-1", Monitored: true, Up: true, Metrics: []monitor.Metric{{ID: "disk", Name: "根分区使用率", Target: "asset-1", Value: 91, Unit: "%", Warning: 80, Critical: 90, Trend: []float64{70, 75, 82, 88, 91}}}}
}
func (fakeAlerts) ActiveCount() int { return 1 }

type fakeAssets struct{}

func (fakeAssets) GetAsset(id string) (cmdb.Asset, error) {
	if id != "asset-1" {
		return cmdb.Asset{}, cmdb.ErrNotFound
	}
	return cmdb.Asset{ID: "asset-1", Name: "rds03", Type: "virtual-machine", TypeName: "虚拟服务器", Status: "warning", IP: "172.28.68.77", Environment: "qa", ProjectGroup: "数据库平台", Owner: "吴涛", Location: "A区/3模块/12机柜", LastSeenAt: "2026-09-21 09:05:00", Relations: []cmdb.Relation{{Type: "connects-to", TargetID: "switch-1", TargetName: "核心交换机"}}}, nil
}
func (fakeAssets) FindHostByIP(ip, _ string) string {
	if ip == "172.28.68.77" {
		return "asset-1"
	}
	return ""
}
func (fakeAssets) ListAssets(cmdb.AssetQuery) cmdb.AssetPage { return cmdb.AssetPage{} }

type fakeImpact struct{}

func (fakeImpact) Impact(string) (topology.Impact, error) {
	return topology.Impact{Root: topology.Node{ID: "asset-1", Name: "rds03"}, Affected: []topology.ImpactNode{{Node: topology.Node{ID: "switch-1", Name: "核心交换机", Status: "warning"}, Distance: 1}}, Summary: topology.ImpactSummary{Total: 1, Warning: 1}}, nil
}

type fakeTickets struct{ created []tickets.CreateInput }

func (f *fakeTickets) List() ([]tickets.Ticket, error) {
	return []tickets.Ticket{{ID: "TKT-OLD", Title: "磁盘空间巡检", Status: "pending", RelatedAlert: "alert-1", RelatedAsset: "asset-1"}}, nil
}
func (f *fakeTickets) Create(in tickets.CreateInput) (tickets.Ticket, error) {
	f.created = append(f.created, in)
	return tickets.Ticket{ID: "TKT-AI-1", Title: in.Title, Status: "pending"}, nil
}

func (f *fakeTickets) ConsumeTaskAuthorization(ticketID, token, resourceID string) (tickets.Ticket, error) {
	return tickets.Ticket{ID: ticketID, Status: tickets.StatusApproved, AutomationTemplate: resourceID}, nil
}

type fakeJobs struct {
	created []jobs.CreateInput
	runID   string
}

func (f *fakeJobs) Templates() []jobs.Template {
	return []jobs.Template{{ID: "tpl-health", Name: "主机健康巡检", Risk: "low", Enabled: true}, {ID: "tpl-clean", Name: "日志空间清理", Risk: "low", Enabled: true}, {ID: "tpl-restart", Name: "应用滚动重启", Risk: "medium", Enabled: true}, {ID: "tpl-patch", Name: "安全补丁安装", Risk: "high", Enabled: true}}
}
func (f *fakeJobs) Create(in jobs.CreateInput) (jobs.Job, error) {
	f.created = append(f.created, in)
	return jobs.Job{ID: "job-ai-1", Name: "主机健康巡检", Status: "pending", Targets: in.Targets}, nil
}

func (f *fakeJobs) Run(id string) (jobs.Job, error) {
	f.runID = id
	return jobs.Job{ID: id, Name: "主机健康巡检", Status: "running", Progress: 10}, nil
}

type fakeTools struct{}

func (fakeTools) Tools() []toolbox.Tool {
	return []toolbox.Tool{{ID: "dns", Name: "DNS 解析", ReadOnly: true, Configured: true}, {ID: "prometheus", Name: "Prometheus 查询", ReadOnly: true, Configured: true}}
}

func (fakeTools) Run(_ context.Context, in toolbox.RunInput) (toolbox.Result, error) {
	return toolbox.Result{ID: "diag-1", ToolID: in.ToolID, ToolName: in.ToolID + " 诊断", Status: "success", Summary: "只读证据采集成功", FinishedAt: "2026-10-10 10:00:00"}, nil
}

type fakeReleases struct{}

func (fakeReleases) List() ([]releases.Release, error) { return []releases.Release{}, nil }

func TestAnalyzeAndCreateApprovalTicket(t *testing.T) {
	s := NewService()
	ticketsProvider := &fakeTickets{}
	s.SetAlertProvider(fakeAlerts{})
	s.SetAssetProvider(fakeAssets{})
	s.SetImpactProvider(fakeImpact{})
	s.SetTicketProvider(ticketsProvider)
	s.SetReleaseProvider(fakeReleases{})
	jobsProvider := &fakeJobs{}
	s.SetJobProvider(jobsProvider)
	s.SetToolProvider(fakeTools{})
	item, err := s.Analyze(context.Background(), "alert-1")
	if err != nil {
		t.Fatal(err)
	}
	if item.RootCause != "磁盘空间耗尽风险" || item.Confidence < 80 || len(item.Evidence) < 4 || len(item.RelatedAssets) == 0 {
		t.Fatalf("unexpected analysis: %#v", item)
	}
	if len(item.Tickets) != 1 || item.Impact.Total != 1 {
		t.Fatalf("related context missing: %#v", item)
	}
	enriched, err := s.CollectEvidence(context.Background(), item.ID, nil)
	if err != nil || len(enriched.Evidence) <= len(item.Evidence) {
		t.Fatalf("collect evidence: %#v %v", enriched, err)
	}
	report, err := s.Report(item.ID)
	if err != nil || !strings.Contains(report.Markdown, "# 故障分析报告") {
		t.Fatalf("report: %#v %v", report, err)
	}
	execution, err := s.Execute(context.Background(), item.ID, item.Recommendations[0].RunbookID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != "pending-approval" || execution.TicketID != "TKT-AI-1" || len(ticketsProvider.created) != 1 {
		t.Fatalf("execution: %#v created=%#v", execution, ticketsProvider.created)
	}
	if execution.ChangePlan.RunbookID == "" || len(execution.ChangePlan.Steps) == 0 || len(execution.ChangePlan.RollbackPlan) == 0 {
		t.Fatalf("change plan missing: %#v", execution.ChangePlan)
	}
	s.HandleTicketDecision(context.Background(), tickets.Ticket{ID: execution.TicketID, Applicant: "admin", AutomationTemplate: execution.RunbookID}, "test-token", "approve")
	execution = s.Executions()[0]
	if execution.Status != "running" || execution.JobID != "job-ai-1" || jobsProvider.runID != "job-ai-1" {
		t.Fatalf("approved execution: %#v jobs=%#v", execution, jobsProvider.created)
	}
	if _, err := s.SetFeedback(context.Background(), item.ID, FeedbackInput{Status: "confirmed", Note: "验证通过"}, "admin"); err != nil {
		t.Fatal(err)
	}
	s.OnJobTerminal(jobs.Job{ID: "job-ai-1", Name: "主机健康巡检", Status: "success", Progress: 100})
	if updated := s.Executions()[0]; updated.Status != "success" || updated.FinishedAt == "" || updated.VerificationStatus != "passed" || updated.VerifiedAt == "" {
		t.Fatalf("terminal callback: %#v", updated)
	}
	if summary := s.Summary(); summary.Accuracy != 100 || summary.ConfirmedFeedback != 1 {
		t.Fatalf("feedback summary: %#v", summary)
	}
	stats := s.FeedbackStats()
	if stats.Total != 1 || stats.Confirmed != 1 || len(stats.ByRootCause) == 0 {
		t.Fatalf("feedback stats: %#v", stats)
	}
}

func TestRunbookChangeGate(t *testing.T) {
	s := NewService()
	s.SetJobProvider(&fakeJobs{})
	policy, ok := s.policyFor("rb-log-cleanup")
	if !ok {
		t.Fatal("missing change runbook")
	}
	policy.Enabled = true
	policy.ExecutionMode = "live"
	policy.RequireRollback = false
	t.Setenv("AIOPS_ALLOW_CHANGE_RUNBOOKS", "false")
	if _, err := s.UpdateRunbook(context.Background(), policy.RunbookID, policy, "admin"); !errors.Is(err, ErrChangeDisabled) {
		t.Fatalf("expected change gate, got %v", err)
	}
	t.Setenv("AIOPS_ALLOW_CHANGE_RUNBOOKS", "true")
	updated, err := s.UpdateRunbook(context.Background(), policy.RunbookID, policy, "admin")
	if err != nil || !updated.Enabled || updated.TemplateID != "tpl-clean" {
		t.Fatalf("update runbook: %#v %v", updated, err)
	}
}
func TestRunbookPreflightRequiresRollbackAndEnforcesThrottle(t *testing.T) {
	t.Setenv("AIOPS_ALLOW_CHANGE_RUNBOOKS", "true")
	s := NewService()
	s.SetAlertProvider(fakeAlerts{})
	s.SetAssetProvider(fakeAssets{})
	s.SetImpactProvider(fakeImpact{})
	s.SetTicketProvider(&fakeTickets{})
	s.SetReleaseProvider(fakeReleases{})
	s.SetJobProvider(&fakeJobs{})
	s.SetToolProvider(fakeTools{})
	analysis, err := s.Analyze(context.Background(), "alert-1")
	if err != nil {
		t.Fatal(err)
	}
	rollbackPolicy, _ := s.policyFor("rb-service-restart")
	rollbackPolicy.Enabled = true
	rollbackPolicy.ExecutionMode = "live"
	rollbackPolicy.RequireRollback = false
	if _, err = s.UpdateRunbook(context.Background(), rollbackPolicy.RunbookID, rollbackPolicy, "admin"); err != nil {
		t.Fatal(err)
	}
	policy, _ := s.policyFor("rb-log-cleanup")
	policy.Enabled = true
	policy.ExecutionMode = "live"
	policy.RollbackRunbookID = rollbackPolicy.RunbookID
	policy.RequireRollback = true
	policy.LiveDailyLimit = 1
	policy.LiveCooldownMinutes = 60
	if _, err = s.UpdateRunbook(context.Background(), policy.RunbookID, policy, "admin"); err != nil {
		t.Fatal(err)
	}
	preflight, err := s.RunbookPreflight(policy.RunbookID, analysis.ID)
	if err != nil || !preflight.Ready {
		t.Fatalf("expected ready preflight: %#v %v", preflight, err)
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	if err = s.saveExecution(context.Background(), Execution{ID: "aiexec-live", AnalysisID: analysis.ID, RunbookID: policy.RunbookID, RunbookTitle: policy.Title, Status: "success", RequestedBy: "admin", RequestedAt: now, ApprovalRequired: true, FinishedAt: now}); err != nil {
		t.Fatal(err)
	}
	preflight, err = s.RunbookPreflight(policy.RunbookID, analysis.ID)
	if err != nil || preflight.Ready {
		t.Fatalf("expected throttled preflight: %#v %v", preflight, err)
	}
	if preflight.NextAllowedAt == "" {
		t.Fatalf("next allowed time missing: %#v", preflight)
	}
}

func TestRunbookSimulationCanEnableWithoutLiveGate(t *testing.T) {
	s := NewService()
	s.SetJobProvider(&fakeJobs{})
	policy, ok := s.policyFor("rb-log-cleanup")
	if !ok {
		t.Fatal("missing change runbook")
	}
	policy.Enabled = true
	policy.ExecutionMode = "simulate"
	t.Setenv("AIOPS_ALLOW_CHANGE_RUNBOOKS", "false")
	updated, err := s.UpdateRunbook(context.Background(), policy.RunbookID, policy, "admin")
	if err != nil || !updated.Enabled || updated.ExecutionMode != "simulate" {
		t.Fatalf("simulation enable: %#v %v", updated, err)
	}
	s.SetAlertProvider(fakeAlerts{})
	s.SetAssetProvider(fakeAssets{})
	s.SetImpactProvider(fakeImpact{})
	ticketsProvider := &fakeTickets{}
	s.SetTicketProvider(ticketsProvider)
	s.SetReleaseProvider(fakeReleases{})
	analysis, err := s.Analyze(context.Background(), "alert-1")
	if err != nil {
		t.Fatal(err)
	}
	execution := Execution{ID: "aiexec-sim", AnalysisID: analysis.ID, RunbookID: policy.RunbookID, RunbookTitle: policy.Title, Status: "pending-approval", TicketID: "TKT-SIM"}
	if err = s.saveExecution(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	s.HandleTicketDecision(context.Background(), tickets.Ticket{ID: "TKT-SIM", Applicant: "admin", AutomationTemplate: policy.RunbookID}, "test-token", "approve")
	simulated, _ := s.findExecutionByTicket("TKT-SIM")
	if simulated.Status != "success" || !simulated.Simulated || simulated.JobID != "" || simulated.VerificationStatus != "passed" {
		t.Fatalf("simulation result: %#v", simulated)
	}
}
func TestExecutionStatsAndControlledRollback(t *testing.T) {
	s := NewService()
	s.SetAlertProvider(fakeAlerts{})
	s.SetAssetProvider(fakeAssets{})
	s.SetImpactProvider(fakeImpact{})
	ticketsProvider := &fakeTickets{}
	s.SetTicketProvider(ticketsProvider)
	s.SetReleaseProvider(fakeReleases{})
	s.SetJobProvider(&fakeJobs{})
	s.SetToolProvider(fakeTools{})
	analysis, err := s.Analyze(context.Background(), "alert-1")
	if err != nil {
		t.Fatal(err)
	}
	rollbackPolicy, ok := s.policyFor("rb-service-restart")
	if !ok {
		t.Fatal("missing rollback runbook")
	}
	rollbackPolicy.Enabled = true
	rollbackPolicy.ExecutionMode = "simulate"
	if _, err = s.UpdateRunbook(context.Background(), rollbackPolicy.RunbookID, rollbackPolicy, "admin"); err != nil {
		t.Fatal(err)
	}
	policy, ok := s.policyFor("rb-log-cleanup")
	if !ok {
		t.Fatal("missing primary runbook")
	}
	policy.Enabled = true
	policy.ExecutionMode = "simulate"
	policy.RollbackRunbookID = rollbackPolicy.RunbookID
	if _, err = s.UpdateRunbook(context.Background(), policy.RunbookID, policy, "admin"); err != nil {
		t.Fatal(err)
	}
	failed := Execution{ID: "aiexec-failed", AnalysisID: analysis.ID, RunbookID: policy.RunbookID, RunbookTitle: policy.Title, Status: "failed", RequestedBy: "admin", RequestedAt: "2026-10-10 12:00:00", ApprovalRequired: true, TicketID: "TKT-FAILED", Error: "execution failed"}
	if err = s.saveExecution(context.Background(), failed); err != nil {
		t.Fatal(err)
	}
	rollback, err := s.RequestRollback(context.Background(), failed.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !rollback.Rollback || rollback.ParentExecutionID != failed.ID || rollback.Status != "pending-approval" {
		t.Fatalf("unexpected rollback request: %#v", rollback)
	}
	s.HandleTicketDecision(context.Background(), tickets.Ticket{ID: rollback.TicketID, Applicant: "admin", AutomationTemplate: rollback.RunbookID}, "test-token", "approve")
	completed, _ := s.findExecutionByID(rollback.ID)
	if completed.Status != "success" || !completed.Simulated {
		t.Fatalf("unexpected rollback result: %#v", completed)
	}
	stats := s.ExecutionStats()
	if stats.Total != 2 || stats.Simulated != 1 || stats.Live != 1 || stats.Success != 1 || stats.Failed != 1 || stats.SuccessRate != 50 {
		t.Fatalf("unexpected execution stats: %#v", stats)
	}
}

func TestKnowledgeSearch(t *testing.T) {
	if len(NewService().SearchKnowledge("OOM")) == 0 {
		t.Fatal("expected knowledge result")
	}
}
