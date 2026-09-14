package httpapi

import (
	"testing"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/discovery"
)

func TestBuildDiscoveryQualityReportsCoverageAndIssues(t *testing.T) {
	agents := []discovery.Agent{
		{ID: "agent-1", Name: "node-1", Hostname: "node-1", IP: "10.0.0.1", Status: "online", Version: "0.1.2"},
		{ID: "agent-2", Name: "node-2", Hostname: "node-2", IP: "10.0.0.2", Status: "offline", Version: "0.1.1"},
	}
	tasks := []discovery.Task{
		{ID: "task-1", Name: "任务一", Status: "completed"},
		{ID: "task-2", Name: "任务二", Status: "failed"},
		{ID: "task-3", Name: "任务三", Status: "running"},
	}
	assets := []cmdb.Asset{
		{ID: "asset-1", Name: "node-1", IP: "10.0.0.1", Type: "virtual-machine", Attributes: []cmdb.Attribute{{Name: "node_exporter_status", Value: "active"}, {Name: "node_exporter_port", Value: "9100"}}},
		{ID: "asset-2", Name: "node-2", IP: "10.0.0.2", Type: "physical-server", Attributes: []cmdb.Attribute{{Name: "node_exporter_status", Value: "failed"}}},
		{ID: "asset-3", Name: "switch-1", IP: "10.0.0.3", Type: "network-device"},
	}
	schedules := []discovery.CollectionSchedule{{ID: "schedule-1", Name: "节点扫描", Enabled: true, LastStatus: "completed"}, {ID: "schedule-2", Name: "网络扫描", Enabled: false, LastStatus: "failed", LastError: "timeout"}}

	report := buildDiscoveryQuality(agents, tasks, assets, schedules, time.Date(2026, 9, 15, 1, 2, 3, 0, time.UTC))

	if report.Agents.Total != 2 || report.Agents.Online != 1 || report.Agents.Offline != 1 {
		t.Fatalf("unexpected agent stats: %+v", report.Agents)
	}
	if report.Exporter.Total != 2 || report.Exporter.Active != 1 || report.Exporter.Inactive != 1 {
		t.Fatalf("unexpected exporter stats: %+v", report.Exporter)
	}
	if report.Tasks.Completed != 1 || report.Tasks.Failed != 1 || report.Tasks.Running != 1 {
		t.Fatalf("unexpected task stats: %+v", report.Tasks)
	}
	if report.Schedules.Enabled != 1 || report.Schedules.Failed != 1 {
		t.Fatalf("unexpected schedule stats: %+v", report.Schedules)
	}
	if len(report.Issues) != 4 {
		t.Fatalf("expected 4 issues, got %d: %+v", len(report.Issues), report.Issues)
	}
	if report.Issues[0].Severity != "critical" || report.Issues[0].Kind != "agent_offline" {
		t.Fatalf("critical agent issue should be first: %+v", report.Issues[0])
	}
	if report.GeneratedAt != "2026-09-15T01:02:03Z" {
		t.Fatalf("unexpected generated time: %s", report.GeneratedAt)
	}
}
