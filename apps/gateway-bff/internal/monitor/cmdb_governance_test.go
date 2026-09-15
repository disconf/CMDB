package monitor

import "testing"

func TestSyncCMDBGovernanceAlertsCreatesAndResolvesAlert(t *testing.T) {
	service := NewService()
	count, err := service.SyncCMDBGovernanceAlerts([]CMDBGovernanceAlert{{ID: "asset-governance-1", Title: "资产责任待确认", Severity: "critical", TargetID: "asset-1", Target: "10.0.0.1", Message: "资产未分配负责人", Owner: "admin"}})
	if err != nil || count != 1 {
		t.Fatalf("sync firing alert: count=%d err=%v", count, err)
	}
	found := false
	for _, alert := range service.Alerts() {
		if alert.ID == "asset-governance-1" && alert.Rule == "cmdb_asset_governance" && alert.TargetID == "asset-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("governance alert missing: %#v", service.Alerts())
	}
	if _, err := service.SyncCMDBGovernanceAlerts(nil); err != nil {
		t.Fatalf("resolve stale alert: %v", err)
	}
	for _, alert := range service.Alerts() {
		if alert.ID == "asset-governance-1" && alert.Status != "resolved" {
			t.Fatalf("expected resolved alert: %#v", alert)
		}
	}
}
