package networkautomation

import (
	"encoding/json"
	"testing"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

func TestOnboardingRequestCreatesAssetAndWhitelist(t *testing.T) {
	cmdbService := cmdb.NewService()
	ports, _ := json.Marshal([]cmdb.NetworkPort{{IfIndex: "1", Name: "GE0/0/1", OperStatus: "up", VLAN: "100", ARPEntries: []cmdb.NetworkARPEntry{{IP: "10.30.0.50", MACAddress: "AA:BB:CC:DD:EE:88", VLAN: "100"}}}})
	if _, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "onboarding-switch", Name: "onboarding-switch", Type: "network-device", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test", Attributes: []cmdb.Attribute{{Name: "network_ports", Label: "网络端口清单", Value: string(ports)}}}, "test"); err != nil {
		t.Fatalf("create switch: %v", err)
	}
	automation := NewService(cmdbService, tickets.NewService(), monitor.NewService(), jobs.NewService())
	anomaly := automation.cmdb.NetworkAnomalies()[0]
	request, err := automation.CreateOnboardingRequest(OnboardingInput{AnomalyID: anomaly.ID, Name: "edge-device", AssetType: "physical-server", Environment: "生产", ProjectGroup: "网络组", Owner: "tester", Location: "一号机房"}, "tester")
	if err != nil {
		t.Fatalf("create onboarding: %v", err)
	}
	approved, err := automation.ApproveOnboardingRequest(request.ID, "admin")
	if err != nil || approved.Status != "onboarded" || approved.AssetID == "" || approved.RuleID == "" {
		t.Fatalf("approve onboarding: %#v %v", approved, err)
	}
	asset, err := cmdbService.GetAsset(approved.AssetID)
	if err != nil || asset.IP != "10.30.0.50" {
		t.Fatalf("onboarded asset missing: %#v %v", asset, err)
	}
	rule, err := automation.complianceRuleByID(approved.RuleID)
	if err != nil || rule.Status != "approved" || normalizeMACValue(rule.Value) != normalizeMACValue("AA:BB:CC:DD:EE:88") {
		t.Fatalf("onboarded rule missing: %#v %v", rule, err)
	}
}

func TestComplianceMaintenanceExpiresTemporaryRule(t *testing.T) {
	service := NewService(cmdb.NewService(), tickets.NewService(), monitor.NewService(), jobs.NewService())
	expired := time.Now().Add(-time.Hour).Format("2006-01-02 15:04:05")
	rule, err := service.CreateComplianceRule(ComplianceRuleInput{Name: "expired temp", Action: "temporary", Type: "mac", Value: "AA:BB:CC:DD:EE:77", ExpiresAt: expired}, "tester")
	if err != nil {
		t.Fatalf("create temp rule: %v", err)
	}
	if _, err = service.ApproveComplianceRule(rule.ID, "admin"); err != nil {
		t.Fatalf("approve temp rule: %v", err)
	}
	report := service.RunComplianceMaintenance()
	if report.Expired == 0 {
		t.Fatalf("expected expired rule: %#v", report)
	}
	updated, err := service.complianceRuleByID(rule.ID)
	if err != nil || updated.Status != "expired" {
		t.Fatalf("expected expired status: %#v %v", updated, err)
	}
}
