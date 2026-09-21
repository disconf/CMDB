package releases

import "testing"

func TestEvaluateVerification(t *testing.T) {
	healthy := EvaluateVerification(VerificationInput{Release: Release{ID: "r1", Status: StatusSuccess}, Argo: &GateArgo{SyncStatus: "Synced", HealthStatus: "Healthy"}, PrometheusReady: true, TargetsUp: 2})
	if healthy.Decision != "success" || healthy.CanAutoRollback {
		t.Fatalf("unexpected healthy verification: %#v", healthy)
	}
	rollback := EvaluateVerification(VerificationInput{Release: Release{ID: "r2", Status: StatusRunning}, Argo: &GateArgo{SyncStatus: "OutOfSync", HealthStatus: "Degraded"}, PrometheusReady: true, TargetsDown: 1})
	if rollback.Decision != "rollback" || !rollback.CanAutoRollback {
		t.Fatalf("unexpected rollback verification: %#v", rollback)
	}
	observe := EvaluateVerification(VerificationInput{Release: Release{ID: "r3", Status: StatusSuccess}, PrometheusReady: true, TargetsUp: 1})
	if observe.Decision != "observe" {
		t.Fatalf("expected observe decision: %#v", observe)
	}
}
