package releases

import (
	"errors"
	"testing"
)

func TestApprovalAndExecutionAuthorization(t *testing.T) {
	service := NewService()
	item, err := service.Start(StartInput{Application: "订单服务", Version: "v2.8.0", Environment: "staging", Operator: "creator"})
	if err != nil || item.Status != StatusAwaitingApproval || item.Approval != "pending" {
		t.Fatalf("start: %#v %v", item, err)
	}
	if _, _, err = service.Approve(item.ID, "creator", "self approval"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected self approval rejection, got %v", err)
	}
	approved, token, err := service.Approve(item.ID, "approver", "approved")
	if err != nil || approved.Status != StatusApproved || token == "" || approved.AuthorizationExpiresAt == "" {
		t.Fatalf("approve: %#v token=%q err=%v", approved, token, err)
	}
	running, err := service.AuthorizeExecution(item.ID, token, "operator")
	if err != nil || running.Status != StatusRunning || running.AuthorizationUsedAt == "" {
		t.Fatalf("execute authorization: %#v %v", running, err)
	}
	if _, err = service.AuthorizeExecution(item.ID, token, "operator"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected one-time authorization consumption, got %v", err)
	}
	rolledBack, err := service.Rollback(item.ID, "operator")
	if err != nil || rolledBack.Status != StatusRollingBack {
		t.Fatalf("rollback: %#v %v", rolledBack, err)
	}
}

func TestManualReleaseCannotBypassApproval(t *testing.T) {
	service := NewService()
	item, err := service.Start(StartInput{Application: "payment", Version: "v1.0.0", Environment: "qa", Operator: "creator"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ApplyEvent(EventInput{Source: "gitlab", ExternalID: item.ID, Type: "pipeline", Application: item.Application, Version: item.Version, Environment: item.Environment, Status: "running"})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected approval gate, got %v", err)
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
