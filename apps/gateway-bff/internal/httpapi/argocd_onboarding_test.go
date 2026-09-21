package httpapi

import (
	"testing"

	"cmdb/gateway-bff/internal/cicd"
)

func TestArgoApplicationAssetID(t *testing.T) {
	id := argoApplicationAssetID("Ops-QA", "Codex.App")
	if id != "argocd-application-ops-qa-codex-app" {
		t.Fatalf("unexpected asset id: %s", id)
	}
}

func TestArgoApplicationAssetStatus(t *testing.T) {
	if status := argoApplicationAssetStatus("Healthy", "Synced"); status != "online" {
		t.Fatalf("unexpected healthy status: %s", status)
	}
	if status := argoApplicationAssetStatus("Missing", "OutOfSync"); status != "warning" {
		t.Fatalf("unexpected warning status: %s", status)
	}
}

func TestArgoAutoSyncEnabled(t *testing.T) {
	t.Setenv("CICD_ARGOCD_AUTO_SYNC", "false")
	if argoAutoSyncEnabled(cicd.ArgoApplication{}) {
		t.Fatal("auto sync should be disabled")
	}
	if !argoAutoSyncEnabled(cicd.ArgoApplication{Annotations: map[string]string{"cmdb.jzq.com/auto-sync": "true"}}) {
		t.Fatal("annotation should enable auto sync")
	}
	t.Setenv("CICD_ARGOCD_AUTO_SYNC", "true")
	if !argoAutoSyncEnabled(cicd.ArgoApplication{}) {
		t.Fatal("global policy should enable auto sync")
	}
}
