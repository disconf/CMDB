package httpapi

import "testing"

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
