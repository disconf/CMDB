package cmdb

import (
	"encoding/json"
	"testing"
)

func TestVLANInventoryAndAnomalyDetection(t *testing.T) {
	service := NewService()
	ports, _ := json.Marshal([]NetworkPort{
		{IfIndex: "1", Name: "GE0/0/1", OperStatus: "up", VLAN: "100", MACAddresses: []string{"AA:BB:CC:DD:EE:FF"}, ARPEntries: []NetworkARPEntry{{IP: "10.10.0.20", MACAddress: "AA:BB:CC:DD:EE:FF", VLAN: "100"}}},
		{IfIndex: "2", Name: "GE0/0/2", OperStatus: "up", VLAN: "100", MACAddresses: []string{"AA:BB:CC:DD:EE:FF"}, ARPEntries: []NetworkARPEntry{{IP: "10.10.0.20", MACAddress: "11:22:33:44:55:66", VLAN: "100"}}},
	})
	if _, err := service.CreateAsset(CreateAssetInput{ID: "analysis-switch", Name: "analysis-switch", Type: "network-device", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test", Attributes: []Attribute{{Name: "network_ports", Label: "网络端口清单", Value: string(ports)}}}, "test"); err != nil {
		t.Fatalf("create switch: %v", err)
	}
	vlans := service.VLANInventory()
	if len(vlans) != 1 || vlans[0].VLAN != "100" || vlans[0].MemberCount != 2 || vlans[0].ARPEntryCount != 2 {
		t.Fatalf("unexpected VLAN inventory: %#v", vlans)
	}
	anomalies := service.NetworkAnomalies()
	seen := map[string]bool{}
	for _, item := range anomalies {
		seen[item.Type] = true
	}
	if !seen["mac-port-flapping"] || !seen["arp-ip-conflict"] {
		t.Fatalf("expected MAC/ARP anomalies: %#v", anomalies)
	}
	acked, err := service.AcknowledgeNetworkAnomaly(anomalies[0].ID, "admin", "已确认")
	if err != nil || acked.Status != "acknowledged" {
		t.Fatalf("acknowledge anomaly: %#v %v", acked, err)
	}
	resolved, err := service.ResolveNetworkAnomaly(anomalies[0].ID, "admin", "已恢复")
	if err != nil || resolved.Status != "resolved" {
		t.Fatalf("resolve anomaly: %#v %v", resolved, err)
	}
}
