package networkautomation

import (
	"encoding/json"
	"testing"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

func TestEvaluateCreatesTicketAlertAndVerificationJob(t *testing.T) {
	cmdbService := cmdb.NewService()
	ports, _ := json.Marshal([]cmdb.NetworkPort{
		{IfIndex: "1", Name: "GE0/0/1", OperStatus: "up", VLAN: "100", MACAddresses: []string{"AA:BB:CC:DD:EE:FF"}},
		{IfIndex: "2", Name: "GE0/0/2", OperStatus: "up", VLAN: "100", MACAddresses: []string{"AA:BB:CC:DD:EE:FF"}},
	})
	if _, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "automation-switch", Name: "automation-switch", Type: "network-device", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test", Attributes: []cmdb.Attribute{{Name: "network_ports", Label: "网络端口清单", Value: string(ports)}}}, "test"); err != nil {
		t.Fatalf("create switch: %v", err)
	}
	automation := NewService(cmdbService, tickets.NewService(), monitor.NewService(), jobs.NewService())
	report, err := automation.Evaluate("tester")
	if err != nil {
		t.Fatalf("evaluate automation: %v", err)
	}
	if report.AutoTickets == 0 || report.AlertsSynced == 0 || len(report.Errors) > 0 {
		t.Fatalf("unexpected first report: %#v", report)
	}
	cleanPorts, _ := json.Marshal([]cmdb.NetworkPort{{IfIndex: "1", Name: "GE0/0/1", OperStatus: "up", VLAN: "100"}})
	if _, err := cmdbService.MergeHostInventory("automation-switch", "test", []cmdb.Attribute{{Name: "network_ports", Label: "网络端口清单", Value: string(cleanPorts)}}); err != nil {
		t.Fatalf("clear anomaly: %v", err)
	}
	report, err = automation.Evaluate("tester")
	if err != nil {
		t.Fatalf("evaluate recovery: %v", err)
	}
	if report.Resolved == 0 || report.VerificationJobs == 0 {
		t.Fatalf("unexpected recovery report: %#v", report)
	}
}
