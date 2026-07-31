package dashboard

import "testing"

func TestServiceOverviewReturnsOperationalSummary(t *testing.T) {
	service := NewService()
	overview := service.Overview()

	if overview.Metrics.AssetTotal != 4286 {
		t.Fatalf("expected 4286 assets, got %d", overview.Metrics.AssetTotal)
	}
	if len(overview.Topology.Layers) != 5 {
		t.Fatalf("expected five topology layers, got %d", len(overview.Topology.Layers))
	}
	if len(overview.AlertTrend) != 12 {
		t.Fatalf("expected 12 alert trend samples, got %d", len(overview.AlertTrend))
	}
	if overview.UpdatedAt.IsZero() {
		t.Fatal("expected overview update timestamp")
	}
}

func TestServiceOverviewReturnsIndependentSnapshots(t *testing.T) {
	service := NewService()
	first := service.Overview()
	first.Alerts[0].Title = "changed"

	second := service.Overview()
	if second.Alerts[0].Title == "changed" {
		t.Fatal("expected independent overview snapshots")
	}
}
