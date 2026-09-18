package cmdb

import (
	"encoding/json"
	"testing"
)

func TestNetworkLinkLifecycle(t *testing.T) {
	service := NewService()
	ports, _ := json.Marshal([]NetworkPort{{IfIndex: "1", Name: "GE0/0/1", AdminStatus: "up", OperStatus: "up", SpeedMbps: "1000", MACAddresses: []string{"00:11:22:33:44:55"}}})
	if _, err := service.CreateAsset(CreateAssetInput{ID: "net-link-switch", Name: "net-link-switch", Type: "network-device", Status: "online", IP: "10.9.0.1", Environment: "test", ProjectGroup: "test", Owner: "test", Attributes: []Attribute{{Name: "network_ports", Label: "网络端口清单", Value: string(ports)}}}, "test"); err != nil {
		t.Fatalf("create switch: %v", err)
	}
	interfaces, _ := json.Marshal([]NetworkInterface{{Name: "eth0", MACAddress: "00:11:22:33:44:55", AdminStatus: "up", OperStatus: "up"}})
	if _, err := service.CreateAsset(CreateAssetInput{ID: "net-link-host", Name: "net-link-host", Type: "physical-server", Status: "online", IP: "10.9.0.10", Environment: "test", ProjectGroup: "test", Owner: "test", Attributes: []Attribute{{Name: "network_interfaces", Label: "网卡清单", Value: string(interfaces)}}}, "test"); err != nil {
		t.Fatalf("create host: %v", err)
	}
	link, err := service.CreateNetworkLink(CreateNetworkLinkInput{SourceAssetID: "net-link-host", SourceInterface: "eth0", TargetAssetID: "net-link-switch", TargetPort: "GE0/0/1"}, "test")
	if err != nil {
		t.Fatalf("create link: %v", err)
	}
	if link.ID == "" || link.Source != "manual" {
		t.Fatalf("unexpected link: %#v", link)
	}
	links := service.NetworkLinks()
	if len(links) != 1 || links[0].TargetPort != "GE0/0/1" {
		t.Fatalf("unexpected links: %#v", links)
	}
	if links[0].Verification != "confirmed" || links[0].Confidence != 100 {
		t.Fatalf("expected MAC-verified link: %#v", links[0])
	}
	interfacesResult, err := service.NetworkInterfaces("net-link-host")
	if err != nil || len(interfacesResult) != 1 || interfacesResult[0].Link == nil {
		t.Fatalf("interface binding missing: %#v %v", interfacesResult, err)
	}
	if _, err := service.CreateNetworkLink(CreateNetworkLinkInput{SourceAssetID: "net-link-host", SourceInterface: "eth0", TargetAssetID: "net-link-switch", TargetPort: "GE0/0/1"}, "test"); err == nil {
		t.Fatal("expected duplicate link conflict")
	}
	if err := service.DeleteNetworkLink(link.ID); err != nil {
		t.Fatalf("delete link: %v", err)
	}
	if len(service.NetworkLinks()) != 0 {
		t.Fatalf("expected link removal: %#v", service.NetworkLinks())
	}
	if history := service.NetworkLinkHistory(10); len(history) < 2 {
		t.Fatalf("expected link create/delete history: %#v", history)
	}
}

func TestNetworkLinkVerificationUsesARPTable(t *testing.T) {
	service := NewService()
	ports, _ := json.Marshal([]NetworkPort{{IfIndex: "3", Name: "GE0/0/3", AdminStatus: "up", OperStatus: "up", ARPEntries: []NetworkARPEntry{{IP: "10.10.0.20", MACAddress: "AA:BB:CC:DD:EE:FF"}}}})
	if _, err := service.CreateAsset(CreateAssetInput{ID: "arp-switch", Name: "arp-switch", Type: "network-device", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test", Attributes: []Attribute{{Name: "network_ports", Label: "网络端口清单", Value: string(ports)}}}, "test"); err != nil {
		t.Fatalf("create switch: %v", err)
	}
	interfaces, _ := json.Marshal([]NetworkInterface{{Name: "eth0", MACAddress: "AA:BB:CC:DD:EE:FF", IPv4: "10.10.0.20/24", AdminStatus: "up", OperStatus: "up"}})
	if _, err := service.CreateAsset(CreateAssetInput{ID: "arp-host", Name: "arp-host", Type: "physical-server", Status: "online", IP: "10.10.0.20", Environment: "test", ProjectGroup: "test", Owner: "test", Attributes: []Attribute{{Name: "network_interfaces", Label: "网卡清单", Value: string(interfaces)}}}, "test"); err != nil {
		t.Fatalf("create host: %v", err)
	}
	if _, err := service.CreateNetworkLink(CreateNetworkLinkInput{SourceAssetID: "arp-host", SourceInterface: "eth0", TargetAssetID: "arp-switch", TargetPort: "GE0/0/3"}, "test"); err != nil {
		t.Fatalf("create link: %v", err)
	}
	links := service.NetworkLinks()
	if len(links) != 1 || links[0].Verification != "confirmed" || links[0].Confidence != 100 {
		t.Fatalf("expected ARP-verified link: %#v", links)
	}
}

func TestNetworkLinkAllowsSwitchInterconnect(t *testing.T) {
	service := NewService()
	accessPorts, _ := json.Marshal([]NetworkPort{{IfIndex: "24", Name: "GE1/0/24", AdminStatus: "up", OperStatus: "up", SpeedMbps: "10000"}})
	corePorts, _ := json.Marshal([]NetworkPort{{IfIndex: "1", Name: "GE1/0/1", AdminStatus: "up", OperStatus: "up", SpeedMbps: "10000"}})
	for _, input := range []CreateAssetInput{
		{ID: "interconnect-access", Name: "interconnect-access", Type: "network-device", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test", Attributes: []Attribute{{Name: "network_ports", Label: "网络端口清单", Value: string(accessPorts)}}},
		{ID: "interconnect-core", Name: "interconnect-core", Type: "network-device", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test", Attributes: []Attribute{{Name: "network_ports", Label: "网络端口清单", Value: string(corePorts)}}},
	} {
		if _, err := service.CreateAsset(input, "test"); err != nil {
			t.Fatalf("create switch %s: %v", input.ID, err)
		}
	}
	interfaces, err := service.NetworkInterfaces("interconnect-access")
	if err != nil || len(interfaces) != 1 || interfaces[0].Type != "switch-port" {
		t.Fatalf("expected switch ports as bindable interfaces: %#v %v", interfaces, err)
	}
	link, err := service.CreateNetworkLink(CreateNetworkLinkInput{SourceAssetID: "interconnect-access", SourceInterface: "GE1/0/24", TargetAssetID: "interconnect-core", TargetPort: "GE1/0/1"}, "test")
	if err != nil {
		t.Fatalf("create switch interconnect: %v", err)
	}
	if link.SourceAssetID != "interconnect-access" || link.TargetAssetID != "interconnect-core" || link.SourceInterface != "GE1/0/24" || link.TargetPort != "GE1/0/1" {
		t.Fatalf("unexpected switch interconnect: %#v", link)
	}
}
