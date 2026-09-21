package monitor

import "testing"

func TestBuildHealthEventPayload(t *testing.T) {
	fingerprint, payload := BuildHealthEventPayload(HealthEventInput{Source: "argocd", ResourceType: "argocd-application", ResourceID: "ops-qa/cmdb", ResourceName: "cmdb", Status: "degraded", Severity: "critical", AssetID: "k8s-jzq"})
	if fingerprint != "cicd-health-argocd-argocd-application-ops-qa-cmdb" {
		t.Fatalf("unexpected fingerprint: %s", fingerprint)
	}
	if payload.Status != "firing" || len(payload.Alerts) != 1 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	alert := payload.Alerts[0]
	if alert.Labels["source"] != "argocd" || alert.Labels["cmdb_asset_id"] != "k8s-jzq" || alert.Labels["severity"] != "critical" {
		t.Fatalf("unexpected labels: %#v", alert.Labels)
	}
	_, resolved := BuildHealthEventPayload(HealthEventInput{Source: "harbor", ResourceType: "harbor-repository", ResourceID: "cmdb/gateway-bff", Status: "healthy"})
	if resolved.Status != "resolved" {
		t.Fatalf("expected resolved payload: %#v", resolved)
	}
}
