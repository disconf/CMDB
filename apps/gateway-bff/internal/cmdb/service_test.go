package cmdb

import (
	"errors"
	"testing"
)

func TestListAssetsSupportsFilteringAndPagination(t *testing.T) {
	service := NewService()
	result := service.ListAssets(AssetQuery{Search: "prod", Type: "physical-server", Status: "online", Page: 1, PageSize: 2})
	if result.Meta.Total != 2 {
		t.Fatalf("expected 2 matching assets, got %d", result.Meta.Total)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected first page with 2 assets, got %d", len(result.Data))
	}
	for _, asset := range result.Data {
		if asset.Type != "physical-server" || asset.Status != "online" {
			t.Fatalf("unexpected asset %+v", asset)
		}
	}
}

func TestListAssetsNormalizesInvalidPagination(t *testing.T) {
	result := NewService().ListAssets(AssetQuery{Page: -1, PageSize: 500})
	if result.Meta.Page != 1 {
		t.Fatalf("expected page 1, got %d", result.Meta.Page)
	}
	if result.Meta.PageSize != 20 {
		t.Fatalf("expected default page size 20, got %d", result.Meta.PageSize)
	}
}

func TestGetAssetReturnsDetailedAttributesAndRelations(t *testing.T) {
	asset, err := NewService().GetAsset("srv-prod-001")
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	if asset.Owner == "" || len(asset.Attributes) == 0 || len(asset.Relations) == 0 {
		t.Fatalf("expected enriched asset detail: %+v", asset)
	}
}

func TestGetAssetReturnsNotFound(t *testing.T) {
	_, err := NewService().GetAsset("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestPrometheusTargetGroupsOnlyIncludeOnlineLinuxAgents(t *testing.T) {
	service := NewService()
	_, err := service.UpsertAgentAsset(AgentAssetInput{ID: "agent-monitor-01", Hostname: "monitor-01", IP: "10.88.0.11"})
	if err != nil {
		t.Fatalf("upsert agent asset: %v", err)
	}
	targets := service.PrometheusTargetGroups("9200")
	if len(targets) != 1 || targets[0].Targets[0] != "10.88.0.11:9200" {
		t.Fatalf("unexpected service discovery targets: %#v", targets)
	}
	if targets[0].Labels["asset_id"] != "agent-monitor-01" || targets[0].Labels["service"] != "cmdb-host" {
		t.Fatalf("missing CMDB labels: %#v", targets[0].Labels)
	}
	if err := service.MarkAgentAssetOffline("agent-monitor-01"); err != nil {
		t.Fatalf("mark offline: %v", err)
	}
	if got := service.PrometheusTargetGroups(""); len(got) != 0 {
		t.Fatalf("offline asset must not be discovered: %#v", got)
	}
}

func TestMonitoringAssetsIncludeOfflineAgentAssets(t *testing.T) {
	service := NewService()
	_, _ = service.UpsertAgentAsset(AgentAssetInput{ID: "agent-offline", Hostname: "offline-host", IP: "10.88.0.12"})
	_ = service.MarkAgentAssetOffline("agent-offline")
	assets := service.MonitoringAssets()
	if len(assets) != 1 || assets[0].ID != "agent-offline" || assets[0].Status != "offline" {
		t.Fatalf("unexpected monitoring candidates: %+v", assets)
	}
}

func TestModelsAndSummaryExposeCatalog(t *testing.T) {
	service := NewService()
	models := service.Models()
	summary := service.Summary()
	if len(models) < 5 {
		t.Fatalf("expected at least five models, got %d", len(models))
	}
	if summary.Total != 12 {
		t.Fatalf("expected 12 assets, got %d", summary.Total)
	}
	if summary.Online+summary.Warning+summary.Offline != summary.Total {
		t.Fatal("status counts must equal total")
	}
}

func TestCreateAssetValidatesAndRecordsHistory(t *testing.T) {
	service := NewService()
	_, err := service.CreateAsset(CreateAssetInput{}, "admin")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	created, err := service.CreateAsset(CreateAssetInput{ID: "vm-new-001", Name: "new-vm", Type: "virtual-machine", Status: "online", IP: "10.40.1.8", Environment: "测试", ProjectGroup: "研发效能组", Owner: "李娜", Location: "VMware / test", Source: "manual", Tags: []string{"测试"}}, "admin")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.TypeName != "虚拟机" {
		t.Fatalf("unexpected type name %q", created.TypeName)
	}
	history, _ := service.History(created.ID)
	if len(history) != 1 || history[0].Action != "created" {
		t.Fatalf("expected creation history, got %+v", history)
	}
}

func TestUpdateAssetChangesFieldsAndRecordsDiff(t *testing.T) {
	service := NewService()
	updated, err := service.UpdateAsset("srv-prod-001", UpdateAssetInput{Name: "prod-api-01", Status: "warning", Owner: "新负责人", ProjectGroup: "核心系统组", Environment: "生产", Location: "上海一号机房 / A03-12", Tags: []string{"核心", "维护"}}, "admin")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Status != "warning" || updated.Owner != "新负责人" {
		t.Fatalf("update not applied: %+v", updated)
	}
	history, _ := service.History(updated.ID)
	if len(history) != 1 || len(history[0].Changes) < 2 {
		t.Fatalf("expected update diff, got %+v", history)
	}
}

func TestImportAssetsReturnsRowErrorsWithoutDiscardingValidRows(t *testing.T) {
	service := NewService()
	result := service.ImportAssets([]CreateAssetInput{{ID: "cloud-new-01", Name: "new-cloud", Type: "cloud-host", Status: "online", IP: "172.19.1.2", Environment: "测试", ProjectGroup: "数据平台组", Owner: "陈明", Location: "华东", Source: "csv"}, {ID: "bad"}}, "admin")
	if result.Created != 1 || len(result.Errors) != 1 {
		t.Fatalf("unexpected import result %+v", result)
	}
}

func TestCreateUpdateAssetWithCustomAttributes(t *testing.T) {
	s := NewService()
	created, err := s.CreateAsset(CreateAssetInput{
		ID: "p1-srv-001", Name: "p1-srv-001", Type: "physical-server", Status: "online", IP: "10.99.1.1",
		Environment: "??", ProjectGroup: "?????", Owner: "??", Location: "?????? / A03-12", Source: "manual",
		Attributes: []Attribute{{Name: "bmc_ip", Value: "10.99.1.9"}, {Name: "rack_no", Value: "A03"}},
	}, "tester")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(created.Attributes) != 2 {
		t.Fatalf("expected 2 attributes, got %d", len(created.Attributes))
	}
	labelBy := map[string]string{}
	for _, a := range created.Attributes {
		labelBy[a.Name] = a.Label
	}
	if labelBy["bmc_ip"] != "????IP(BMC)" {
		t.Fatalf("bmc_ip label not resolved from preset: %q", labelBy["bmc_ip"])
	}
	if labelBy["rack_no"] != "???" {
		t.Fatalf("rack_no label not resolved from preset: %q", labelBy["rack_no"])
	}
	updated, err := s.UpdateAsset("p1-srv-001", UpdateAssetInput{
		Name: "p1-srv-001", Status: "online", IP: "10.99.1.1", Environment: "??",
		ProjectGroup: "?????", Owner: "??", Location: "?????? / A03-12",
		Attributes: []Attribute{{Name: "bmc_ip", Value: "10.99.1.10"}, {Name: "rack_no", Value: "A03"}},
	}, "tester")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	found := ""
	for _, a := range updated.Attributes {
		if a.Name == "bmc_ip" {
			found = a.Value
		}
	}
	if found != "10.99.1.10" {
		t.Fatalf("bmc_ip not updated: %q", found)
	}
	history, err := s.History("p1-srv-001")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	changed := false
	for _, h := range history {
		for _, c := range h.Changes {
			if c.Field == "????IP(BMC)" && c.Before == "10.99.1.9" && c.After == "10.99.1.10" {
				changed = true
			}
		}
	}
	if !changed {
		t.Fatalf("expected attribute change in history, history=%+v", history)
	}
}

func TestAgentLifecycleRecordsEvents(t *testing.T) {
	s := NewService()
	in := AgentAssetInput{ID: "host-lifecycle-01", Type: "virtual-machine", Hostname: "lifecycle-01", IP: "10.77.1.1", OS: "linux"}
	if _, err := s.UpsertAgentAsset(in); err != nil {
		t.Fatalf("upsert1: %v", err)
	}
	if err := s.MarkAgentAssetOffline("host-lifecycle-01"); err != nil {
		t.Fatalf("offline: %v", err)
	}
	if _, err := s.UpsertAgentAsset(in); err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	history, err := s.History("host-lifecycle-01")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	actions := map[string]bool{}
	for _, h := range history {
		actions[h.Action] = true
	}
	for _, expected := range []string{"agent-registered", "offline", "online"} {
		if !actions[expected] {
			t.Fatalf("missing action %s in %+v", expected, actions)
		}
	}
	if asset, _ := s.GetAsset("host-lifecycle-01"); asset.Status != "online" {
		t.Fatalf("expected online after re-register, got %s", asset.Status)
	}
}
