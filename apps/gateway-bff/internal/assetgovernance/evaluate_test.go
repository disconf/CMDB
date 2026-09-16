package assetgovernance

import (
	"testing"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

func TestAssetGovernanceLifecycle(t *testing.T) {
	t.Setenv("CMDB_ENABLE_DEMO_DATA", "false")
	t.Setenv("DATABASE_URL", "")
	cmdbService := cmdb.NewService()
	asset, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "asset-governance-test", Name: "governance-test", Type: "physical-server", Status: "online", IP: "10.0.0.10", Environment: "测试", ProjectGroup: "订单平台", Owner: "待分配", Location: "A 机房 / A01-01", Source: "test"}, "tester")
	if err != nil {
		t.Fatalf("create asset: %v", err)
	}
	monitorService := monitor.NewService()
	service := NewService(cmdbService, tickets.NewService(), monitorService)
	issues := service.Issues()
	if len(issues) != 1 || issues[0].IssueType != "unowned" || issues[0].Severity != "critical" {
		t.Fatalf("unexpected governance issues: %#v", issues)
	}
	report, err := service.Evaluate("tester")
	if err != nil {
		t.Fatalf("evaluate governance: %v", err)
	}
	if report.Tickets != 1 || report.Alerts != 1 {
		t.Fatalf("expected ticket and alert: %#v", report)
	}
	report, err = service.Evaluate("tester")
	if err != nil {
		t.Fatalf("repeat evaluate governance: %v", err)
	}
	if report.Tickets != 0 {
		t.Fatalf("existing governance ticket should be reused: %#v", report)
	}
	updated, err := service.Assign(asset.ID, "负责人甲", "订单平台", "A 机房 / A01-01", "tester", "已完成责任确认")
	if err != nil {
		t.Fatalf("assign owner: %v", err)
	}
	if updated.Owner != "负责人甲" {
		t.Fatalf("owner not updated: %#v", updated)
	}
	if issues = service.Issues(); len(issues) != 0 {
		t.Fatalf("expected issues cleared after assignment: %#v", issues)
	}
	if _, err = service.Retire(asset.ID, "tester", "设备已下线"); err != nil {
		t.Fatalf("retire asset: %v", err)
	}
	if _, err = cmdbService.GetAsset(asset.ID); err != nil {
		t.Fatalf("read retired asset: %v", err)
	}
	report, err = service.Evaluate("tester")
	if err != nil {
		t.Fatalf("evaluate retired asset: %v", err)
	}
	if report.Issues != 0 || report.Tickets != 0 || report.Alerts != 1 {
		t.Fatalf("retired asset should be excluded: %#v", report)
	}
}

func TestAssetGovernanceConfirmSuppressesRemediation(t *testing.T) {
	t.Setenv("CMDB_ENABLE_DEMO_DATA", "false")
	t.Setenv("DATABASE_URL", "")
	cmdbService := cmdb.NewService()
	asset, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "asset-governance-confirm", Name: "confirm-test", Type: "virtual-machine", Status: "online", IP: "10.0.0.11", Environment: "测试", ProjectGroup: "待确认", Owner: "待分配", Source: "test"}, "tester")
	if err != nil {
		t.Fatalf("create asset: %v", err)
	}
	monitorService := monitor.NewService()
	service := NewService(cmdbService, tickets.NewService(), monitorService)
	if _, err = service.Confirm(asset.ID, "tester", "已线下确认"); err != nil {
		t.Fatalf("confirm responsibility: %v", err)
	}
	report, err := service.Evaluate("tester")
	if err != nil {
		t.Fatalf("evaluate governance: %v", err)
	}
	if report.Issues == 0 || report.Tickets != 0 || report.Alerts != 0 {
		t.Fatalf("confirmed issue should not create remediation: %#v", report)
	}
}

func TestIssuesForFiltersByAssetScope(t *testing.T) {
	t.Setenv("CMDB_ENABLE_DEMO_DATA", "false")
	t.Setenv("DATABASE_URL", "")
	cmdbService := cmdb.NewService()
	for _, item := range []cmdb.CreateAssetInput{
		{ID: "scope-core", Name: "scope-core", Type: "physical-server", Status: "online", IP: "10.1.0.1", Environment: "测试", ProjectGroup: "核心系统组", Owner: "待分配", Source: "test"},
		{ID: "scope-dev", Name: "scope-dev", Type: "physical-server", Status: "online", IP: "10.1.0.2", Environment: "测试", ProjectGroup: "研发效能组", Owner: "待分配", Source: "test"},
	} {
		if _, err := cmdbService.CreateAsset(item, "tester"); err != nil {
			t.Fatalf("create asset: %v", err)
		}
	}
	service := NewService(cmdbService, tickets.NewService(), monitor.NewService())
	items := service.IssuesFor(cmdb.AssetQuery{ScopeProjectGroups: []string{"核心系统组"}})
	if len(items) != 1 || items[0].AssetID != "scope-core" {
		t.Fatalf("scope filter failed: %#v", items)
	}
}
