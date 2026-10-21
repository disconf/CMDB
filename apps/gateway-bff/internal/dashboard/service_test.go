package dashboard

import (
	"testing"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/discovery"
	"cmdb/gateway-bff/internal/jobs"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/releases"
	"cmdb/gateway-bff/internal/tickets"
)

func TestServiceOverviewWithoutRuntimeReturnsEmptyRealSnapshot(t *testing.T) {
	service := NewService()
	overview := service.Overview()

	if overview.Metrics.AssetTotal != 0 || overview.Assets.Total != 0 {
		t.Fatalf("expected no invented assets, got metrics=%+v snapshot=%+v", overview.Metrics, overview.Assets)
	}
	if len(overview.Capacity) != 0 || len(overview.Health) != 0 || len(overview.Alerts) != 0 {
		t.Fatalf("expected empty dashboard panels without runtime sources")
	}
	if overview.UpdatedAt.IsZero() {
		t.Fatal("expected overview update timestamp")
	}
}

func TestServiceOverviewReturnsIndependentSnapshots(t *testing.T) {
	now := time.Now()
	build := func() Overview {
		return BuildScopedOverview(
			now,
			[]cmdb.Asset{{ID: "h1", Type: "physical-server", Status: "online", ProjectGroup: "核心"}},
			nil,
			monitor.Coverage{},
			[]monitor.Alert{{ID: "a1", Title: "磁盘告警", Status: "firing", Severity: "warning"}},
			nil,
			nil,
			nil,
			nil,
		)
	}
	first := build()
	first.Alerts[0].Title = "changed"
	second := build()
	if second.Alerts[0].Title == "changed" {
		t.Fatal("expected independent overview snapshots")
	}
}

func TestBuildScopedOverviewUsesOnlyVisibleSources(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.Local)
	overview := BuildScopedOverview(
		now,
		[]cmdb.Asset{
			{ID: "h1", Type: "physical-server", Status: "online", ProjectGroup: "核心", Environment: "生产", Source: "agent"},
			{ID: "h2", Type: "virtual-machine", Status: "offline", ProjectGroup: "核心", Environment: "生产", Source: "agent"},
		},
		[]discovery.Agent{{ID: "h1", IP: "10.0.0.1", Status: "online"}, {ID: "h2", IP: "10.0.0.2", Status: "offline"}},
		monitor.Coverage{Total: 2, Monitored: 2, Healthy: 1, Percentage: 100},
		[]monitor.Alert{{ID: "a1", Title: "主机离线", Status: "firing", Severity: "critical", StartedAt: now.Add(-time.Minute).Format("2006-01-02 15:04")}},
		[]jobs.Job{
			{ID: "j1", Name: "巡检", Status: "success", StartedAt: now.Format("2006-01-02 15:04:05"), Operator: "ops"},
			{ID: "j2", Name: "变更", Status: "failed", StartedAt: now.Format("2006-01-02 15:04:05"), Operator: "ops"},
		},
		[]jobs.Schedule{{ID: "schedule-1"}},
		[]tickets.Ticket{{ID: "t1", Status: tickets.StatusPending, SLADueAt: now.Add(10 * time.Minute).Format(time.RFC3339)}},
		[]releases.Release{{Application: "api", Version: "v1", Status: releases.StatusSuccess, StartedAt: now.Format("2006-01-02 15:04:05")}},
	)

	if overview.Assets.Total != 2 || overview.Assets.Online != 1 || overview.Assets.Offline != 1 {
		t.Fatalf("unexpected asset snapshot %+v", overview.Assets)
	}
	if overview.Agents.Online != 1 || overview.Agents.Coverage != 50 {
		t.Fatalf("unexpected agent snapshot %+v", overview.Agents)
	}
	if overview.Monitoring.Coverage != 100 || overview.Monitoring.Availability != 50 {
		t.Fatalf("unexpected monitoring snapshot %+v", overview.Monitoring)
	}
	if overview.Tasks.Today != 2 || overview.Tasks.SuccessRate != 50 {
		t.Fatalf("unexpected task snapshot %+v", overview.Tasks)
	}
	if overview.Command.CriticalAlerts != 1 || overview.Command.PlatformStatus != "attention" {
		t.Fatalf("unexpected command status %+v", overview.Command)
	}
	if len(overview.Sources) != 6 || len(overview.Alerts) != 1 || len(overview.Timeline) != 0 {
		t.Fatalf("unexpected scoped panels %+v", overview)
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
