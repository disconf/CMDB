package releases

import "testing"

func TestStartAndRollback(t *testing.T) {
	service := NewService()
	item, err := service.Start(StartInput{Application: "订单服务", Version: "v2.8.0", Environment: "staging", Operator: "admin"})
	if err != nil || item.Status != StatusRunning || item.Source != "manual" {
		t.Fatalf("start: %#v %v", item, err)
	}
	rolledBack, err := service.Rollback(item.ID, "admin")
	if err != nil || rolledBack.Status != StatusRollingBack {
		t.Fatalf("rollback: %#v %v", rolledBack, err)
	}
	items, err := service.List()
	if err != nil || len(items) == 0 {
		t.Fatalf("list: %#v %v", items, err)
	}
}

func TestApplyEventPersistsPipelineAndArtifact(t *testing.T) {
	service := NewService()
	first, err := service.ApplyEvent(EventInput{
		Source: "gitlab", ExternalID: "pipeline-42", Type: "pipeline", Application: "payment-gateway",
		Version: "v4.2.0", Environment: "staging", Status: "running", Stage: "build", Progress: 35,
		Message: "building", TicketID: "TKT-42",
	})
	if err != nil || first.Status != StatusRunning || first.Source != "gitlab" {
		t.Fatalf("pipeline event: %#v %v", first, err)
	}
	second, err := service.ApplyEvent(EventInput{
		Source: "harbor", ExternalID: "pipeline-42", Type: "artifact", Application: "payment-gateway",
		Version: "v4.2.0", Environment: "staging", Status: "success", Stage: "artifact",
		Commit: "31bd808", Size: "194 MB", Security: "passed",
	})
	if err != nil || second.ID != first.ID || second.Status != StatusSuccess || second.Commit != "31bd808" {
		t.Fatalf("artifact event: %#v %v", second, err)
	}
	if len(second.Stages) == 0 {
		t.Fatal("expected release stages")
	}
	artifacts, err := service.Artifacts()
	if err != nil || len(artifacts) == 0 {
		t.Fatalf("artifacts: %#v %v", artifacts, err)
	}
}

func TestInvalidRelease(t *testing.T) {
	service := NewService()
	if _, err := service.Start(StartInput{}); err == nil {
		t.Fatal("expected validation")
	}
	if _, err := service.ApplyEvent(EventInput{}); err == nil {
		t.Fatal("expected event validation")
	}
}
