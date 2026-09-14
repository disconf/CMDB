package discovery

import (
	"testing"

	"cmdb/gateway-bff/internal/cmdb"
)

func TestAgentVersionReportMergesHeartbeatAndPersistedAssets(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("CMDB_ENABLE_DEMO_DATA", "false")
	t.Setenv("CMDB_AGENT_VERSION", "0.2.0")
	cmdbService := cmdb.NewService()
	if _, err := cmdbService.UpsertAgentAsset(cmdb.AgentAssetInput{
		ID: "host-persisted", Type: "virtual-machine", Hostname: "persisted", IP: "10.0.0.11", AgentVersion: "0.2.0",
	}); err != nil {
		t.Fatalf("seed persisted agent: %v", err)
	}
	service := NewServiceWithCMDB(cmdbService)
	if _, err := service.Register(RegisterInput{Name: "heartbeat", Hostname: "heartbeat", IP: "10.0.0.12"}); err != nil {
		t.Fatalf("register heartbeat agent: %v", err)
	}
	if err := service.Heartbeat("agt-01", "0.1.0"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	report := service.AgentVersionReport()
	if report.CurrentVersion != "0.2.0" {
		t.Fatalf("current version = %q", report.CurrentVersion)
	}
	if report.Total != 2 || report.Current != 1 || report.Outdated != 1 {
		t.Fatalf("unexpected report totals: total=%d current=%d outdated=%d", report.Total, report.Current, report.Outdated)
	}
	byIP := map[string]AgentVersionItem{}
	for _, item := range report.Agents {
		byIP[item.IP] = item
	}
	if !byIP["10.0.0.11"].Current || byIP["10.0.0.11"].Source != "cmdb" {
		t.Fatalf("persisted asset was not marked current: %+v", byIP["10.0.0.11"])
	}
	if !byIP["10.0.0.12"].Outdated || byIP["10.0.0.12"].Source != "heartbeat" {
		t.Fatalf("heartbeat agent was not marked outdated: %+v", byIP["10.0.0.12"])
	}
}
