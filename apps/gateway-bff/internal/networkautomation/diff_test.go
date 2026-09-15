package networkautomation

import (
	"testing"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/tickets"
)

func TestNetworkSnapshotDiffDetectsAddedDevice(t *testing.T) {
	cmdbService := cmdb.NewService()
	service := NewService(cmdbService, tickets.NewService(), monitor.NewService(), jobs.NewService())
	first, err := service.CaptureNetworkSnapshot("tester")
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	if _, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "diff-switch", Name: "diff-switch", Type: "network-device", Status: "online", IP: "10.0.1.1", Environment: "test", ProjectGroup: "test", Owner: "test"}, "test"); err != nil {
		t.Fatalf("create switch: %v", err)
	}
	second, err := service.CaptureNetworkSnapshot("tester")
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	diff, err := service.NetworkDiff(first.ID, second.ID)
	if err != nil {
		t.Fatalf("network diff: %v", err)
	}
	if diff.Summary.Added == 0 {
		t.Fatalf("expected added device diff: %#v", diff)
	}
}
