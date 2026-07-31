package topology

import "testing"

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
}

func TestMissingImpactNode(t *testing.T) {
	_, err := NewService().Impact("missing")
	if err == nil {
		t.Fatal("expected missing node error")
	}
}
