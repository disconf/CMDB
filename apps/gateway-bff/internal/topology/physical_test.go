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

func TestPhysicalGraphViewHidesUnlinkedAssets(t *testing.T) {
	cmdbService := cmdb.NewService()
	for _, input := range []cmdb.CreateAssetInput{
		{ID: "view-switch", Name: "view-switch", Type: "network-device", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test"},
		{ID: "view-host-linked", Name: "view-host-linked", Type: "physical-server", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test"},
		{ID: "view-host-unlinked", Name: "view-host-unlinked", Type: "physical-server", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test"},
	} {
		if _, err := cmdbService.CreateAsset(input, "test"); err != nil {
			t.Fatalf("create asset %s: %v", input.ID, err)
		}
	}
	if _, err := cmdbService.CreateNetworkLink(cmdb.CreateNetworkLinkInput{SourceAssetID: "view-host-linked", SourceInterface: "eth0", TargetAssetID: "view-switch", TargetPort: "GE0/0/1"}, "test"); err != nil {
		t.Fatalf("link: %v", err)
	}

	service := NewServiceWithCMDB(cmdbService)
	linkedGraph := service.PhysicalGraphView("test", true)
	if len(linkedGraph.Nodes) != 2 || len(linkedGraph.Edges) != 1 {
		t.Fatalf("unexpected linked-only graph: %#v", linkedGraph)
	}
	for _, node := range linkedGraph.Nodes {
		if node.ID == "view-host-unlinked" {
			t.Fatalf("unlinked host must be hidden: %#v", linkedGraph.Nodes)
		}
	}

	fullGraph := service.PhysicalGraphView("test", false)
	if len(fullGraph.Nodes) != 3 || len(fullGraph.Edges) != 1 {
		t.Fatalf("unexpected full graph: %#v", fullGraph)
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

func TestCapturePhysicalSnapshot(t *testing.T) {
	cmdbService := cmdb.NewService()
	if _, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "snapshot-switch", Name: "snapshot-switch", Type: "network-device", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test"}, "test"); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if _, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "snapshot-host", Name: "snapshot-host", Type: "physical-server", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test"}, "test"); err != nil {
		t.Fatalf("host: %v", err)
	}
	if _, err := cmdbService.CreateNetworkLink(cmdb.CreateNetworkLinkInput{SourceAssetID: "snapshot-host", SourceInterface: "eth0", TargetAssetID: "snapshot-switch", TargetPort: "GE0/0/1"}, "test"); err != nil {
		t.Fatalf("link: %v", err)
	}
	snapshot, err := NewServiceWithCMDB(cmdbService).CaptureSnapshot("physical", "test", "tester")
	if err != nil {
		t.Fatalf("capture snapshot: %v", err)
	}
	if snapshot.NodeCount != 2 || snapshot.EdgeCount != 1 || len(snapshot.Payload) == 0 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestPhysicalGraphClassifiesSwitchRolesAndInterconnect(t *testing.T) {
	cmdbService := cmdb.NewService()
	accessPorts := `[{"ifIndex":"1","name":"GE1/0/24","operStatus":"up","neighborName":"aggregation-switch-01","neighborPort":"GE1/0/1"}]`
	for _, input := range []cmdb.CreateAssetInput{
		{ID: "access-switch", Name: "access-switch-01", Type: "network-device", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test", Attributes: []cmdb.Attribute{{Name: "network_role", Label: "网络角色", Value: "接入交换机"}, {Name: "network_ports", Label: "网络端口清单", Value: accessPorts}}},
		{ID: "agg-switch", Name: "aggregation-switch-01", Type: "network-device", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test", Attributes: []cmdb.Attribute{{Name: "network_role", Label: "网络角色", Value: "汇聚交换机"}}},
	} {
		if _, err := cmdbService.CreateAsset(input, "test"); err != nil {
			t.Fatalf("create switch %s: %v", input.ID, err)
		}
	}

	graph := NewServiceWithCMDB(cmdbService).PhysicalGraph("test")
	roles := map[string]string{}
	for _, node := range graph.Nodes {
		roles[node.ID] = node.Role
	}
	if roles["access-switch"] != "接入交换机" || roles["agg-switch"] != "汇聚交换机" {
		t.Fatalf("unexpected switch roles: %#v", roles)
	}
	if len(graph.Edges) != 1 || graph.Edges[0].LinkKind != "switch-interconnect" || graph.Edges[0].SourceInterface != "GE1/0/24" || graph.Edges[0].TargetPort != "GE1/0/1" {
		t.Fatalf("unexpected interconnect edge: %#v", graph.Edges)
	}
}
