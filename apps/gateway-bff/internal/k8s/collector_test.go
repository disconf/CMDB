package k8s

import "testing"

func TestBuildResources(t *testing.T) {
	var node Node
	node.Metadata.Name = "node01"
	node.Metadata.UID = "n1"
	node.Spec.ProviderID = "vmware://node01"
	node.Status.Addresses = []NodeAddress{{Type: "InternalIP", Address: "10.0.0.1"}}
	node.Status.Conditions = []NodeCondition{{Type: "Ready", Status: "True"}}
	node.Status.Capacity = map[string]string{"cpu": "8", "memory": "16Gi"}
	node.Status.NodeInfo.KubeletVersion = "v1.28.15"
	node.Status.NodeInfo.OSImage = "Ubuntu"
	node.Status.NodeInfo.Architecture = "amd64"
	snapshot := Snapshot{Version: Version{GitVersion: "v1.28.15"}, Nodes: []Node{node}}
	resources := BuildResources("test-cluster", "https://k8s", snapshot)
	if len(resources) != 2 || resources[0].Asset.Type != "k8s-cluster" || resources[1].Asset.Type != "k8s-node" || resources[1].Asset.IP != "10.0.0.1" {
		t.Fatalf("unexpected resources: %+v", resources)
	}
}
