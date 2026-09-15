package cmdb

import "testing"

func TestTopologySnapshotLifecycle(t *testing.T) {
	service := NewService()
	snapshot, err := service.SaveTopologySnapshot(TopologySnapshotInput{Kind: "physical", Environment: "test", NodeCount: 2, EdgeCount: 1, Payload: []byte(`{"nodes":[],"edges":[]}`), Operator: "tester"})
	if err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	if snapshot.ID == "" || snapshot.NodeCount != 2 || snapshot.EdgeCount != 1 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	items := service.TopologySnapshots("physical", "test", 10, false)
	if len(items) != 1 || len(items[0].Payload) != 0 {
		t.Fatalf("unexpected snapshot list: %#v", items)
	}
	detail, err := service.TopologySnapshot(snapshot.ID)
	if err != nil || len(detail.Payload) == 0 {
		t.Fatalf("snapshot detail missing: %#v %v", detail, err)
	}
}
