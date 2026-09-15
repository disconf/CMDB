package monitor

import "testing"

func TestSyncCMDBAnomaliesCreatesAndResolvesAlert(t *testing.T) {
	service := NewService()
	count, err := service.SyncCMDBAnomalies([]CMDBAnomalyAlert{{ID: "network-anomaly-1", Title: "MAC 漂移", Severity: "critical", TargetID: "asset-1", Target: "10.0.0.1", Message: "同一 MAC 出现在多个端口", Owner: "admin"}})
	if err != nil || count != 1 {
		t.Fatalf("sync firing alert: count=%d err=%v", count, err)
	}
	found := false
	for _, alert := range service.Alerts() {
		if alert.ID == "network-anomaly-1" && alert.Rule == "cmdb_network_anomaly" && alert.Severity == "critical" {
			found = true
		}
	}
	if !found {
		t.Fatalf("network anomaly alert missing: %#v", service.Alerts())
	}
	if _, err := service.SyncCMDBAnomalies(nil); err != nil {
		t.Fatalf("resolve stale alert: %v", err)
	}
	for _, alert := range service.Alerts() {
		if alert.ID == "network-anomaly-1" && alert.Status != "resolved" {
			t.Fatalf("expected resolved alert: %#v", alert)
		}
	}
}
