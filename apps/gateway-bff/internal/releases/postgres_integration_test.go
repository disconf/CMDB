package releases

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPostgresReleaseEventLifecycle(t *testing.T) {
	url := os.Getenv("CMDB_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("CMDB_TEST_DATABASE_URL is not configured")
	}
	db, err := openPostgres(url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := &Service{db: db}
	externalID := "integration-release-" + time.Now().Format("150405.000000")
	application := "integration-gateway"
	version := "v0.0.0-test"
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM release_deployments WHERE external_id=$1`, externalID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM release_artifacts WHERE application=$1 AND version=$2`, application, version)
	})
	first, err := service.ApplyEvent(EventInput{
		Source: "gitlab", ExternalID: externalID, Type: "pipeline", Application: application,
		Version: version, Environment: "qa", Status: "running", Stage: "build", Progress: 40,
		Message: "pipeline running", TicketID: "TKT-INTEGRATION",
	})
	if err != nil || first.Status != StatusRunning || first.TicketID != "TKT-INTEGRATION" {
		t.Fatalf("pipeline event: %#v %v", first, err)
	}
	second, err := service.ApplyEvent(EventInput{
		Source: "argocd", ExternalID: externalID, Type: "deployment", Application: application,
		Version: version, Environment: "qa", Status: "success", Stage: "verify", Progress: 100,
		Message: "healthy", Commit: "integration-commit", Size: "12 MB", Security: "passed",
	})
	if err != nil || second.ID != first.ID || second.Status != StatusSuccess || second.Progress != 100 {
		t.Fatalf("deployment event: %#v %v", second, err)
	}
	items, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.ID == first.ID && item.Commit == "integration-commit" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("release %s not found after persistence", first.ID)
	}
	artifacts, err := service.Artifacts()
	if err != nil || len(artifacts) == 0 {
		t.Fatalf("artifacts: %#v %v", artifacts, err)
	}
}
