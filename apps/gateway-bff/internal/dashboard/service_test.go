package dashboard

import (
	"testing"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/monitor"
)

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

func TestBuildCapacityAndHealthUseRealInventory(t *testing.T) {
	assets := []cmdb.Asset{
		{ID: "h1", Type: "physical-server", Status: "online"},
		{ID: "h2", Type: "virtual-machine", Status: "offline"},
		{ID: "p1", Type: "k8s-pod", Status: "online"},
		{ID: "n1", Type: "network-device", Status: "online"},
		{ID: "m1", Type: "middleware-kafka", Status: "online"},
	}
	capacity := buildCapacity(assets)
	if len(capacity) != len(capacityGroups) {
		t.Fatalf("expected one capacity row per group, got %d", len(capacity))
	}
	if capacity[0].Detail != "2 / 5 资产" {
		t.Fatalf("unexpected server share detail %q", capacity[0].Detail)
	}
	if capacity[0].Value != 40 {
		t.Fatalf("expected 40 percent for two of five assets, got %v", capacity[0].Value)
	}
	health := buildHealth(assets)
	if len(health) == 0 {
		t.Fatal("expected health rows for populated groups")
	}
	for _, item := range health {
		if item.Value < 0 || item.Value > 100 {
			t.Fatalf("health outside 0-100: %+v", item)
		}
	}
	if health[0].Name != "服务器节点" || health[0].Value != 50 {
		t.Fatalf("unexpected server health %+v", health[0])
	}
}

func TestBuildHealthSkipsEmptyGroups(t *testing.T) {
	items := buildHealth([]cmdb.Asset{{ID: "h1", Type: "physical-server", Status: "online"}})
	if len(items) != 1 {
		t.Fatalf("groups without assets must be skipped, got %d rows", len(items))
	}
}

func TestBuildTopologyUsesInventoryDimensions(t *testing.T) {
	assets := []cmdb.Asset{
		{ID: "h1", Type: "physical-server", Status: "online", ProjectGroup: "核心系统组", Environment: "生产", Source: "ssh", Location: "上海机房/A01"},
		{ID: "h2", Type: "physical-server", Status: "offline", ProjectGroup: "核心系统组", Environment: "生产", Source: "agent", Location: "上海机房/A02"},
	}
	topology := buildTopology(assets)
	if len(topology.Layers) != 5 {
		t.Fatalf("expected five layers, got %d", len(topology.Layers))
	}
	projectLayer := topology.Layers[0]
	if projectLayer.Name != "项目组" || len(projectLayer.Nodes) != 1 {
		t.Fatalf("unexpected project layer %+v", projectLayer)
	}
	if projectLayer.Nodes[0].Name != "核心系统组 2" {
		t.Fatalf("layer node must carry the asset count, got %q", projectLayer.Nodes[0].Name)
	}
	if projectLayer.Nodes[0].Status != "warning" {
		t.Fatalf("half offline assets should mark the bucket warning, got %q", projectLayer.Nodes[0].Status)
	}
	locationLayer := topology.Layers[4]
	if locationLayer.Name != "机房位置" || len(locationLayer.Nodes) != 1 || locationLayer.Nodes[0].Name != "上海机房 2" {
		t.Fatalf("location layer must group by room, got %+v", locationLayer)
	}
}

func TestBuildAlertTrendBucketsByTime(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.Local)
	alerts := []monitor.Alert{
		{Severity: "critical", StartedAt: now.Add(-30 * time.Minute).Format("2006-01-02 15:04")},
		{Severity: "warning", StartedAt: now.Add(-90 * time.Minute).Format("2006-01-02 15:04")},
		{Severity: "critical", StartedAt: now.Add(-48 * time.Hour).Format("2006-01-02 15:04")},
		{Severity: "critical", StartedAt: ""},
	}
	trend := buildAlertTrend(alerts, now)
	if len(trend) != 12 {
		t.Fatalf("expected twelve buckets, got %d", len(trend))
	}
	last := trend[len(trend)-1]
	if last.Total != 2 {
		t.Fatalf("expected two alerts in the newest bucket, got %d", last.Total)
	}
	if last.Critical != 1 {
		t.Fatalf("expected one critical alert in the newest bucket, got %d", last.Critical)
	}
	total := 0
	for _, point := range trend {
		total += point.Total
	}
	if total != 2 {
		t.Fatalf("alerts older than 24h or without a timestamp must be dropped, got %d", total)
	}
}
