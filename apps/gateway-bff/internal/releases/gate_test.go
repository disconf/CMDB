package releases

import (
	"testing"
	"time"
)

func TestEvaluateGateBlocksCriticalConditions(t *testing.T) {
	sunday := time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)
	for sunday.Weekday() != time.Sunday {
		sunday = sunday.AddDate(0, 0, 1)
	}
	result := EvaluateGate(GateInput{
		Release:       Release{Status: StatusApproved, Environment: "production", Application: "gateway", TicketID: "TKT-1"},
		Now:           sunday,
		ChangeWindows: "production=Mon-Fri,09:00-18:00",
		Alerts:        []GateAlert{{ID: "a1", Severity: "critical", Status: "firing", Target: "gateway"}},
		Argo:          &GateArgo{Name: "gateway", SyncStatus: "Synced", HealthStatus: "Healthy"},
		Assets:        []GateAsset{{ID: "gateway-1", Name: "gateway", Status: "online"}},
	})
	if result.Allowed || result.Decision != "block" || len(result.Blockers) < 2 {
		t.Fatalf("expected blocked gate: %#v", result)
	}
}

func TestEvaluateGateAllowsHealthyRelease(t *testing.T) {
	result := EvaluateGate(GateInput{
		Release: Release{Status: StatusApproved, Environment: "qa", Application: "gateway"},
		Argo:    &GateArgo{Name: "gateway", SyncStatus: "Synced", HealthStatus: "Healthy"},
		Assets:  []GateAsset{{ID: "gateway-1", Name: "gateway", Status: "online"}},
	})
	if !result.Allowed || result.Decision != "allow" {
		t.Fatalf("expected allowed gate: %#v", result)
	}
}

func TestAutoRequestRollbackForRunningRelease(t *testing.T) {
	service := NewService()
	item, err := service.ApplyEvent(EventInput{Source: "gitlab", ExternalID: "pipeline-auto-rollback", Type: "pipeline", Application: "auto-rollback-app", Version: "v1.0.0", Environment: "qa", Status: StatusRunning})
	if err != nil {
		t.Fatal(err)
	}
	rollback, err := service.AutoRequestRollback("auto-rollback-app", "qa", "v1.0.0", "health event")
	if err != nil || rollback.ID != item.ID || rollback.Status != StatusRollbackAwaitingApproval {
		t.Fatalf("auto rollback: %#v %v", rollback, err)
	}
}
