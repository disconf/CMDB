package networkautomation

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPostgresPolicyAndLifecyclePersistence(t *testing.T) {
	databaseURL := os.Getenv("CMDB_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CMDB_INTEGRATION_DATABASE_URL is not configured")
	}
	db, err := openPostgres(databaseURL)
	if err != nil {
		t.Fatalf("open automation postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	policy := Policy{ID: "integration-network-policy", Name: "integration", Enabled: true, Priority: 1, Severity: "critical", AnomalyType: "*", AutoTicket: true, EscalationMinutes: 5}
	if err := upsertPolicy(context.Background(), db, policy); err != nil {
		t.Fatalf("persist policy: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM cmdb_network_automation_policies WHERE id=$1", policy.ID)
	})
	policies, err := loadPolicies(context.Background(), db)
	if err != nil || len(policies) == 0 {
		t.Fatalf("load policies: %#v %v", policies, err)
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	life := Lifecycle{AnomalyID: "integration-network-anomaly", PolicyID: policy.ID, Status: "open", Title: "test", Severity: "critical", Target: "10.0.0.1", FirstSeenAt: now, LastSeenAt: now}
	if err := upsertLifecycle(context.Background(), db, life); err != nil {
		t.Fatalf("persist lifecycle: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM cmdb_network_anomaly_lifecycle WHERE anomaly_id=$1", life.AnomalyID)
	})
	lifecycle, err := loadLifecycle(context.Background(), db)
	if err != nil || lifecycle[life.AnomalyID].Status != "open" {
		t.Fatalf("load lifecycle: %#v %v", lifecycle, err)
	}
}
