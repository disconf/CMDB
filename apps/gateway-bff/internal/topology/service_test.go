package topology

import (
	"testing"

	"cmdb/gateway-bff/internal/cmdb"
)

func TestGraphAndImpact(t *testing.T) {
	s := NewService()
	g := s.Graph("production")
	if len(g.Nodes) < 8 || len(g.Edges) < 7 {
		t.Fatalf("unexpected graph size: %d/%d", len(g.Nodes), len(g.Edges))
	}
	impact, err := s.Impact("service-order")
	if err != nil || impact.Root.ID != "service-order" || len(impact.Affected) < 3 {
		t.Fatalf("unexpected impact: %#v %v", impact, err)
	}
	if impact.Summary.Total != len(impact.Affected) || len(impact.Paths) != len(impact.Affected) {
		t.Fatalf("impact summary/path mismatch: %#v", impact)
	}
	if len(impact.Affected[0].Path) < 2 || impact.Affected[0].Direction == "" || impact.Affected[0].Distance < 1 {
		t.Fatalf("expected enriched impact node, got %#v", impact.Affected[0])
	}
}

func TestCMDBGraphCacheInvalidation(t *testing.T) {
	cmdbService := cmdb.NewService()
	service := NewServiceWithCMDB(cmdbService)
	if got := len(service.Graph("test").Nodes); got != 0 {
		t.Fatalf("expected empty graph, got %d nodes", got)
	}
	if _, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "cache-host", Name: "cache-host", Type: "network-device", Status: "online", Environment: "test", ProjectGroup: "test", Owner: "test"}, "test"); err != nil {
		t.Fatalf("create asset: %v", err)
	}
	if got := len(service.Graph("test").Nodes); got != 0 {
		t.Fatalf("expected cached graph before invalidation, got %d nodes", got)
	}
	service.Invalidate()
	if got := len(service.Graph("test").Nodes); got != 1 {
		t.Fatalf("expected refreshed graph after invalidation, got %d nodes", got)
	}
}

func TestCMDBGraphUsesAssetsAndRelations(t *testing.T) {
	cmdbService := cmdb.NewService()
	if _, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "topology-host", Name: "topology-host", Type: "virtual-machine", Status: "online", IP: "10.0.0.10", Environment: "test", ProjectGroup: "test", Owner: "test", Relations: []cmdb.Relation{{Type: "connects-to", TargetID: "topology-switch", TargetName: "topology-switch"}}}, "test"); err != nil {
		t.Fatalf("create host: %v", err)
	}
	if _, err := cmdbService.CreateAsset(cmdb.CreateAssetInput{ID: "topology-switch", Name: "topology-switch", Type: "network-device", Status: "online", IP: "10.0.0.1", Environment: "test", ProjectGroup: "test", Owner: "test"}, "test"); err != nil {
		t.Fatalf("create switch: %v", err)
	}
	graph := NewServiceWithCMDB(cmdbService).Graph("test")
	foundEdge := false
	for _, edge := range graph.Edges {
		if edge.Source == "topology-host" && edge.Target == "topology-switch" && edge.Relation == "connects-to" {
			foundEdge = true
		}
	}
	if !foundEdge {
		t.Fatalf("expected CMDB relation edge, got %#v", graph.Edges)
	}
}
func TestMissingImpactNode(t *testing.T) {
	_, err := NewService().Impact("missing")
	if err == nil {
		t.Fatal("expected missing node error")
	}
}
