package assetgovernance

import (
	"testing"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

func TestGovernanceRuleAssignsMatchingAssets(t *testing.T) {
	t.Setenv("CMDB_ENABLE_DEMO_DATA", "false")
	t.Setenv("DATABASE_URL", "")
	cmdbService := cmdb.NewService()
	asset, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "rule-asset-1", Name: "rule-node", Type: "physical-server", Status: "online", IP: "172.28.68.21", Environment: "测试", ProjectGroup: "自动发现", Owner: "待分配", Source: "node-exporter", Tags: []string{"自动发现"}}, "tester")
	if err != nil {
		t.Fatalf("create asset: %v", err)
	}
	service := NewService(cmdbService, tickets.NewService(), monitor.NewService())
	rule, err := service.CreateGovernanceRule(GovernanceRuleInput{Name: "68 网段主机分派", Enabled: true, Priority: 20, MatchAssetType: "physical-server", MatchIPCIDR: "172.28.68.0/24", Owner: "基础设施组", TargetProjectGroup: "基础设施组", Location: "68 网段 / 待补充"}, "tester")
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	report := service.EvaluateGovernanceRules("tester")
	if report.Matched != 1 || report.Assigned != 1 {
		t.Fatalf("unexpected rule report: %#v", report)
	}
	updated, err := cmdbService.GetAsset(asset.ID)
	if err != nil {
		t.Fatalf("read updated asset: %v", err)
	}
	if updated.Owner != "基础设施组" || updated.ProjectGroup != "基础设施组" || updated.Location != "68 网段 / 待补充" {
		t.Fatalf("asset was not assigned by rule: %#v", updated)
	}
	if rule.ID == "" || len(service.GovernanceRules()) != 1 {
		t.Fatalf("rule was not persisted in memory: %#v", rule)
	}
}

func TestBulkGovernanceActions(t *testing.T) {
	t.Setenv("CMDB_ENABLE_DEMO_DATA", "false")
	t.Setenv("DATABASE_URL", "")
	cmdbService := cmdb.NewService()
	for _, id := range []string{"bulk-asset-1", "bulk-asset-2"} {
		if _, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: id, Name: id, Type: "virtual-machine", Status: "online", IP: "10.0.0." + id[len(id)-1:], Environment: "测试", ProjectGroup: "自动发现", Owner: "待分配", Source: "node-exporter"}, "tester"); err != nil {
			t.Fatalf("create asset %s: %v", id, err)
		}
	}
	service := NewService(cmdbService, tickets.NewService(), monitor.NewService())
	result, err := service.BulkGovernance(AssetGovernanceBulkInput{AssetIDs: []string{"bulk-asset-1", "bulk-asset-2", "bulk-asset-1"}, Action: "assign", Owner: "研发效能组", ProjectGroup: "研发效能组", Location: "虚拟化平台 / cluster-a", Note: "批量分派"}, "tester")
	if err != nil {
		t.Fatalf("bulk assign: %v", err)
	}
	if result.Total != 2 || result.Succeeded != 2 || result.Failed != 0 {
		t.Fatalf("unexpected bulk result: %#v", result)
	}
	for _, id := range []string{"bulk-asset-1", "bulk-asset-2"} {
		asset, err := cmdbService.GetAsset(id)
		if err != nil || asset.Owner != "研发效能组" {
			t.Fatalf("asset %s not updated: %#v %v", id, asset, err)
		}
	}
}
