package monitor

import (
	"os"
	"testing"
	"time"
)

func TestAlertPersistsAcrossServiceRestart(t *testing.T) {
	url := os.Getenv("CMDB_INTEGRATION_DATABASE_URL")
	if url == "" {
		t.Skip("CMDB_INTEGRATION_DATABASE_URL is not configured")
	}
	t.Setenv("DATABASE_URL", url)
	id := "monitor-integration-" + time.Now().UTC().Format("20060102150405.000000000")
	first := NewService()
	t.Cleanup(func() { _, _ = first.db.Exec("DELETE FROM monitor_alerts WHERE id=$1", id); _ = first.db.Close() })
	payload := WebhookPayload{Status: "firing"}
	payload.Alerts = append(payload.Alerts, struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
		EndsAt      time.Time         `json:"endsAt"`
		Fingerprint string            `json:"fingerprint"`
	}{Status: "firing", Labels: map[string]string{"alertname": "PersistenceCheck", "severity": "critical", "instance": "integration"}, Annotations: map[string]string{"summary": "Persistence check"}, StartsAt: time.Now(), Fingerprint: id})
	if _, err := first.ReceiveWebhook(payload); err != nil {
		t.Fatalf("persist webhook: %v", err)
	}
	second := NewService()
	defer second.db.Close()
	found := false
	for _, a := range second.Alerts() {
		if a.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatal("alert was not restored from PostgreSQL")
	}
	if _, err := second.Acknowledge(id, "integration-test"); err != nil {
		t.Fatalf("persist acknowledgement: %v", err)
	}
	var status, owner string
	if err := second.db.QueryRow("SELECT status,owner FROM monitor_alerts WHERE id=$1", id).Scan(&status, &owner); err != nil || status != "acknowledged" || owner != "integration-test" {
		t.Fatalf("unexpected persisted state status=%s owner=%s err=%v", status, owner, err)
	}
}
