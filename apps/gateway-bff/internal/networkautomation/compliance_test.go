package networkautomation

import (
	"encoding/json"
	"testing"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

func TestComplianceRuleSuppressesUnmanagedAccess(t *testing.T) {
	cmdbService := cmdb.NewService()
	ports, _ := json.Marshal([]cmdb.NetworkPort{{IfIndex: "1", Name: "GE0/0/1", OperStatus: "up", VLAN: "100", ARPEntries: []cmdb.NetworkARPEntry{{IP: "10.20.0.50", MACAddress: "AA:BB:CC:DD:EE:99", VLAN: "100"}}}})
	if _, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "compliance-switch", Name: "compliance-switch", Type: "network-device", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test", Attributes: []cmdb.Attribute{{Name: "network_ports", Label: "网络端口清单", Value: string(ports)}}}, "test"); err != nil {
		t.Fatalf("create switch: %v", err)
	}
	automation := NewService(cmdbService, tickets.NewService(), monitor.NewService(), jobs.NewService())
	rule, err := automation.CreateComplianceRule(ComplianceRuleInput{Name: "允许测试终端", Action: "allow", Type: "mac", Value: "AA:BB:CC:DD:EE:99", Description: "测试授权"}, "tester")
	if err != nil {
		t.Fatalf("create compliance rule: %v", err)
	}
	if _, err = automation.ApproveComplianceRule(rule.ID, "admin"); err != nil {
		t.Fatalf("approve compliance rule: %v", err)
	}
	report, err := automation.Evaluate("tester")
	if err != nil {
		t.Fatalf("evaluate automation: %v", err)
	}
	if report.ComplianceAllowed == 0 || report.AutoTickets != 0 {
		t.Fatalf("expected allowed unmanaged access without ticket: %#v", report)
	}
}
