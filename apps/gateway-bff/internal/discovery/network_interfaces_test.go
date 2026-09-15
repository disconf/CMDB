package discovery

import "testing"

func TestParseSSHNetworkInterfaces(t *testing.T) {
	output := `test-host
"Rocky Linux 9.4"
5.14.0
x86_64
8
8388608
10737418240
1718726400
none
__CMDB_NETWORK_STATE__
2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc mq state UP mode DEFAULT group default qlen 1000    link/ether 00:50:56:aa:bb:cc brd ff:ff:ff:ff:ff:ff
2: eth0    inet 172.28.68.10/24 brd 172.28.68.255 scope global eth0
`
	host := parseSSHInventory("172.28.68.10", output)
	if host == nil {
		t.Fatal("nil host")
	}
	interfaces := parseSSHNetworkInterfaces("172.28.68.10", output)
	if len(interfaces) != 1 {
		t.Fatalf("interfaces=%#v", interfaces)
	}
	if interfaces[0].Name != "eth0" || interfaces[0].MACAddress != "00:50:56:AA:BB:CC" || interfaces[0].IPv4 != "172.28.68.10/24" || interfaces[0].MTU != "1500" {
		t.Fatalf("unexpected interface: %#v", interfaces[0])
	}
	found := false
	for _, attribute := range host.Attributes {
		if attribute.Name == "network_interfaces" {
			found = true
		}
	}
	if !found {
		t.Fatalf("network_interfaces attribute missing: %#v", host.Attributes)
	}
}

func TestParseSSHNetworkNeighbors(t *testing.T) {
	output := `test-host
"Rocky Linux 9.4"
5.14.0
x86_64
8
8388608
10737418240
1718726400
none
__CMDB_NETWORK_STATE__
2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc mq state UP mode DEFAULT group default qlen 1000    link/ether 00:50:56:aa:bb:cc brd ff:ff:ff:ff:ff:ff
2: eth0    inet 172.28.68.10/24 brd 172.28.68.255 scope global eth0
__CMDB_NEIGHBOR_STATE__
172.28.68.1 dev eth0 lladdr 00:11:22:33:44:55 REACHABLE
`
	interfaces := parseSSHNetworkInterfaces("172.28.68.10", output)
	if len(interfaces) != 1 || len(interfaces[0].Neighbors) != 1 {
		t.Fatalf("unexpected neighbors: %#v", interfaces)
	}
	if interfaces[0].Neighbors[0].IP != "172.28.68.1" || interfaces[0].Neighbors[0].MACAddress != "00:11:22:33:44:55" {
		t.Fatalf("unexpected neighbor: %#v", interfaces[0].Neighbors[0])
	}
}
