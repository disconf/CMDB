package topology

import (
	"testing"

	"cmdb/gateway-bff/internal/cmdb"
)

func TestPhysicalGraphUsesNetworkLinks(t *testing.T) {
	cmdbService := cmdb.NewService()
	if _, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "physical-switch", Name: "physical-switch", Type: "network-device", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test"}, "test"); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if _, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "physical-host", Name: "physical-host", Type: "physical-server", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test"}, "test"); err != nil {
		t.Fatalf("host: %v", err)
	}
	if _, err := cmdbService.CreateNetworkLink(cmdb.CreateNetworkLinkInput{SourceAssetID: "physical-host", SourceInterface: "eth0", TargetAssetID: "physical-switch", TargetPort: "GE0/0/1"}, "test"); err != nil {
		t.Fatalf("link: %v", err)
	}
	graph := NewServiceWithCMDB(cmdbService).PhysicalGraph("test")
	if len(graph.Nodes) != 2 || len(graph.Edges) != 1 {
		t.Fatalf("unexpected graph: %#v", graph)
	}
	edge := graph.Edges[0]
	if edge.Source != "physical-host" || edge.Target != "physical-switch" || edge.SourceInterface != "eth0" || edge.TargetPort != "GE0/0/1" {
		t.Fatalf("unexpected physical edge: %#v", edge)
	}
}

func TestPhysicalGraphNormalizesLLDPToHostSide(t *testing.T) {
	cmdbService := cmdb.NewService()
	ports := `[{"ifIndex":"1","name":"GE0/0/1","operStatus":"up","neighborName":"lldp-host","neighborPort":"eth0"}]`
	if _, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "lldp-switch", Name: "lldp-switch", Type: "network-device", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test", Attributes: []cmdb.Attribute{{Name: "network_ports", Label: "网络端口清单", Value: ports}}}, "test"); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if _, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "lldp-host", Name: "lldp-host", Type: "physical-server", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test"}, "test"); err != nil {
		t.Fatalf("host: %v", err)
	}
	graph := NewServiceWithCMDB(cmdbService).PhysicalGraph("test")
	if len(graph.Edges) != 1 || graph.Edges[0].Source != "lldp-host" || graph.Edges[0].Target != "lldp-switch" || graph.Edges[0].SourceInterface != "eth0" || graph.Edges[0].TargetPort != "GE0/0/1" {
		t.Fatalf("unexpected normalized LLDP edge: %#v", graph.Edges)
	}
}
