package aiops

import (
	"context"
	"testing"

	"cmdb/gateway-bff/internal/cmdb"
	"cmdb/gateway-bff/internal/monitor"
	"cmdb/gateway-bff/internal/releases"
	"cmdb/gateway-bff/internal/tickets"
	"cmdb/gateway-bff/internal/topology"
)

type fakeAlerts struct{}

func (fakeAlerts) Alerts() []monitor.Alert {
	return []monitor.Alert{{ID: "alert-1", Title: "主机磁盘空间不足", Severity: "critical", Status: "firing", TargetID: "node-exporter-172-28-68-77", Target: "rds03.qa.bcavt.com", Rule: "DiskUsage > 90%", StartedAt: "2026-09-21 09:00:00", Duration: "30 分钟", Owner: "值班组", Message: "根分区使用率达到 91%"}}
}
func (fakeAlerts) Events(string) ([]monitor.AlertEvent, error) {
	return []monitor.AlertEvent{{ID: 1, AlertID: "alert-1", Type: "notification", Operator: "system", Message: "已通知负责人", CreatedAt: "2026-09-21 09:02:00"}}, nil
}
func (fakeAlerts) Host(string) monitor.HostMonitoring {
	return monitor.HostMonitoring{AssetID: "asset-1", Monitored: true, Up: true, Metrics: []monitor.Metric{{ID: "disk", Name: "根分区使用率", Target: "asset-1", Value: 91, Unit: "%", Warning: 80, Critical: 90, Trend: []float64{70, 75, 82, 88, 91}}}}
}
func (fakeAlerts) ActiveCount() int { return 1 }

type fakeAssets struct{}

func (fakeAssets) GetAsset(id string) (cmdb.Asset, error) {
	if id != "asset-1" {
		return cmdb.Asset{}, cmdb.ErrNotFound
	}
	return cmdb.Asset{ID: "asset-1", Name: "rds03", Type: "virtual-machine", TypeName: "虚拟服务器", Status: "warning", IP: "172.28.68.77", Environment: "qa", ProjectGroup: "数据库平台", Owner: "吴涛", Location: "A区/3模块/12机柜", LastSeenAt: "2026-09-21 09:05:00", Relations: []cmdb.Relation{{Type: "connects-to", TargetID: "switch-1", TargetName: "核心交换机"}}}, nil
}
func (fakeAssets) FindHostByIP(ip, _ string) string {
	if ip == "172.28.68.77" {
		return "asset-1"
	}
	return ""
}
func (fakeAssets) ListAssets(cmdb.AssetQuery) cmdb.AssetPage { return cmdb.AssetPage{} }

type fakeImpact struct{}

func (fakeImpact) Impact(string) (topology.Impact, error) {
	return topology.Impact{Root: topology.Node{ID: "asset-1", Name: "rds03"}, Affected: []topology.ImpactNode{{Node: topology.Node{ID: "switch-1", Name: "核心交换机", Status: "warning"}, Distance: 1}}, Summary: topology.ImpactSummary{Total: 1, Warning: 1}}, nil
}

type fakeTickets struct{ created []tickets.CreateInput }

func (f *fakeTickets) List() ([]tickets.Ticket, error) {
	return []tickets.Ticket{{ID: "TKT-OLD", Title: "磁盘空间巡检", Status: "pending", RelatedAlert: "alert-1", RelatedAsset: "asset-1"}}, nil
}
func (f *fakeTickets) Create(in tickets.CreateInput) (tickets.Ticket, error) {
	f.created = append(f.created, in)
	return tickets.Ticket{ID: "TKT-AI-1", Title: in.Title, Status: "pending"}, nil
}

type fakeReleases struct{}

func (fakeReleases) List() ([]releases.Release, error) { return []releases.Release{}, nil }

func TestAnalyzeAndCreateApprovalTicket(t *testing.T) {
	s := NewService()
	ticketsProvider := &fakeTickets{}
	s.SetAlertProvider(fakeAlerts{})
	s.SetAssetProvider(fakeAssets{})
	s.SetImpactProvider(fakeImpact{})
	s.SetTicketProvider(ticketsProvider)
	s.SetReleaseProvider(fakeReleases{})
	item, err := s.Analyze(context.Background(), "alert-1")
	if err != nil {
		t.Fatal(err)
	}
	if item.RootCause != "磁盘空间耗尽风险" || item.Confidence < 80 || len(item.Evidence) < 4 || len(item.RelatedAssets) == 0 {
		t.Fatalf("unexpected analysis: %#v", item)
	}
	if len(item.Tickets) != 1 || item.Impact.Total != 1 {
		t.Fatalf("related context missing: %#v", item)
	}
	execution, err := s.Execute(context.Background(), item.ID, item.Recommendations[0].RunbookID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if execution.Status != "pending-approval" || execution.TicketID != "TKT-AI-1" || len(ticketsProvider.created) != 1 {
		t.Fatalf("execution: %#v created=%#v", execution, ticketsProvider.created)
	}
}

func TestKnowledgeSearch(t *testing.T) {
	if len(NewService().SearchKnowledge("OOM")) == 0 {
		t.Fatal("expected knowledge result")
	}
}
