package jobs

import (
	"errors"
	"testing"
)

func TestCreateRunAndRetry(t *testing.T) {
	s := NewService()
	j, err := s.Create(CreateInput{TemplateID: "tpl-health", Targets: []string{"prod-api-01"}, Operator: "admin"})
	if err != nil || j.Status != "pending" {
		t.Fatalf("create: %#v %v", j, err)
	}
	j, _ = s.Run(j.ID)
	if j.Status != "running" || j.Attempt != 1 || len(j.Logs) == 0 {
		t.Fatalf("run: %#v", j)
	}
	j, _ = s.Cancel(j.ID)
	if j.Status != "cancelled" {
		t.Fatalf("cancel: %#v", j)
	}
	j, _ = s.Retry(j.ID)
	if j.Status != "running" || j.Attempt != 2 {
		t.Fatalf("retry: %#v", j)
	}
}
func TestInvalidTemplate(t *testing.T) {
	if _, err := NewService().Create(CreateInput{TemplateID: "missing"}); err == nil {
		t.Fatal("expected validation error")
	}
}
func TestNodeExporterTemplateIsBuiltIn(t *testing.T) {
	s := NewService()
	found := false
	for _, template := range s.Templates() {
		if template.ID == "tpl-node-exporter" && template.Command == "install-node-exporter" && template.Risk == "medium" {
			found = true
		}
	}
	if !found {
		t.Fatal("node exporter template is missing")
	}
}
func TestUnsafeTemplateAndHighRiskApproval(t *testing.T) {
	s := NewService()
	if _, err := s.CreateTemplate(TemplateInput{Name: "危险", Category: "维护", Command: "rm -rf /", Risk: "high"}); !errors.Is(err, ErrUnsafeCommand) {
		t.Fatalf("expected unsafe command, got %v", err)
	}
	tpl, err := s.CreateTemplate(TemplateInput{Name: "补丁", Category: "安全", Command: "patch --approved", Risk: "high"})
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Create(CreateInput{TemplateID: tpl.ID, Targets: []string{"srv-1"}, Operator: "creator"})
	if err != nil || j.Status != "awaiting_approval" {
		t.Fatalf("job %#v %v", j, err)
	}
	if _, err = s.Approve(j.ID, "creator"); !errors.Is(err, ErrInvalidState) {
		t.Fatal("self approval must fail")
	}
	j, err = s.Approve(j.ID, "reviewer")
	if err != nil || j.Status != "pending" || j.ApprovedBy != "reviewer" {
		t.Fatalf("approval %#v %v", j, err)
	}
}
