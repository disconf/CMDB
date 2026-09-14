package discovery

import (
	"strings"
	"testing"
)

func testPolicyInput(subjectType, subject string) RemoteSecurityPolicyInput {
	enabled := true
	return RemoteSecurityPolicyInput{
		Name: "测试策略", SubjectType: subjectType, Subject: subject, Enabled: &enabled,
		MaxSessionMinutes: 30, MaxConcurrentSessions: 2, RecordingRetentionDays: 30,
		FileTransferEnabled: true, UploadMaxMB: 50, DownloadMaxMB: 25,
		AllowedUploadPaths: []string{"/**"}, AllowedDownloadPaths: []string{"/**"},
		AllowControlCollaborators: true, ApprovalMode: "risk", ApprovalTTLMinutes: 10,
	}
}

func TestRemoteSecurityPolicyPrecedence(t *testing.T) {
	service := NewService()
	roleInput := testPolicyInput("role", "operator")
	roleInput.MaxSessionMinutes = 45
	if _, err := service.SaveRemoteSecurityPolicy(roleInput, "admin"); err != nil {
		t.Fatal(err)
	}
	userInput := testPolicyInput("user", "alice")
	userInput.MaxSessionMinutes = 25
	if _, err := service.SaveRemoteSecurityPolicy(userInput, "admin"); err != nil {
		t.Fatal(err)
	}
	if got := service.EffectiveRemoteSecurityPolicy("alice", []string{"operator"}).MaxSessionMinutes; got != 25 {
		t.Fatalf("user policy should win, got %d", got)
	}
	if got := service.EffectiveRemoteSecurityPolicy("bob", []string{"operator"}).MaxSessionMinutes; got != 45 {
		t.Fatalf("role policy should apply, got %d", got)
	}
	if got := service.EffectiveRemoteSecurityPolicy("bob", []string{"viewer"}).MaxSessionMinutes; got != 30 {
		t.Fatalf("global policy should apply, got %d", got)
	}
}

func TestRemoteSecurityAllCommandApproval(t *testing.T) {
	service := NewService()
	input := testPolicyInput("user", "alice")
	input.ApprovalMode = "all"
	input.ApprovalTTLMinutes = 3
	if _, err := service.SaveRemoteSecurityPolicy(input, "admin"); err != nil {
		t.Fatal(err)
	}
	service.remoteSessions = []RemoteSession{{ID: "session-1", AssetID: "asset-1", AssetName: "host-1", IP: "10.0.0.1", Operator: "alice", Roles: []string{"operator"}, Status: "active", ExpiresAt: "2099-01-01 00:00:00"}}
	approval, err := service.RequestTerminalApproval("session-1", "uptime", "alice", []string{"operator"})
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "pending" || !strings.Contains(approval.Reason, "所有交互命令") {
		t.Fatalf("unexpected approval: %+v", approval)
	}
}

func TestRemoteSecurityFileAndCollaboratorLimits(t *testing.T) {
	service := NewService()
	input := testPolicyInput("user", "alice")
	input.UploadMaxMB = 1
	input.DownloadMaxMB = 1
	input.AllowedUploadPaths = []string{"/tmp/**"}
	input.AllowedDownloadPaths = []string{"/var/log/**"}
	input.AllowControlCollaborators = false
	if _, err := service.SaveRemoteSecurityPolicy(input, "admin"); err != nil {
		t.Fatal(err)
	}
	item := RemoteSession{ID: "session-1", AssetID: "asset-1", AssetName: "host-1", Operator: "alice", Roles: []string{"operator"}, Status: "active", ExpiresAt: "2099-01-01 00:00:00"}
	if err := service.checkRemoteFilePolicy(item, "alice", []string{"operator"}, "/etc/passwd", true, 10); err == nil {
		t.Fatal("expected upload path rejection")
	}
	if err := service.checkRemoteFilePolicy(item, "alice", []string{"operator"}, "/tmp/file.bin", true, 2<<20); err == nil {
		t.Fatal("expected upload size rejection")
	}
	if err := service.checkRemoteFilePolicy(item, "alice", []string{"operator"}, "/var/log/system.log", false, 0); err != nil {
		t.Fatalf("expected allowed download path, got %v", err)
	}
	service.remoteSessions = []RemoteSession{item}
	if _, err := service.AddRemoteSessionCollaborator("session-1", RemoteSessionCollaboratorInput{Username: "viewer", Access: "control"}, "alice", []string{"operator"}); err == nil {
		t.Fatal("expected control collaborator policy rejection")
	}
	if _, err := service.AddRemoteSessionCollaborator("session-1", RemoteSessionCollaboratorInput{Username: "viewer", Access: "readonly"}, "alice", []string{"operator"}); err != nil {
		t.Fatalf("readonly collaborator should remain allowed: %v", err)
	}
}
func TestRemoteSecurityPolicyBatchApply(t *testing.T) {
	service := NewService()
	base := testPolicyInput("user", "ignored")
	base.ApprovalMode = "all"
	result, err := service.SaveRemoteSecurityPolicyBatch(RemoteSecurityPolicyBatchInput{
		Policy: base,
		Subjects: []RemoteSecurityPolicySubjectInput{
			{SubjectType: "user", Subject: "Alice"},
			{SubjectType: "role", Subject: "Operator"},
			{SubjectType: "user", Subject: "alice"},
		},
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Applied) != 2 || len(result.Errors) != 0 {
		t.Fatalf("unexpected batch result: %+v", result)
	}
	if got := service.EffectiveRemoteSecurityPolicy("alice", nil).ApprovalMode; got != "all" {
		t.Fatalf("user batch policy should apply, got %q", got)
	}
	if got := service.EffectiveRemoteSecurityPolicy("bob", []string{"operator"}).ApprovalMode; got != "all" {
		t.Fatalf("role batch policy should apply, got %q", got)
	}
}

func TestRemoteSecurityPolicyBatchRejectsInvalidSubjects(t *testing.T) {
	service := NewService()
	base := testPolicyInput("user", "ignored")
	result, err := service.SaveRemoteSecurityPolicyBatch(RemoteSecurityPolicyBatchInput{
		Policy:   base,
		Subjects: []RemoteSecurityPolicySubjectInput{{SubjectType: "global", Subject: "global"}},
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Applied) != 0 || len(result.Errors) != 1 {
		t.Fatalf("unexpected invalid batch result: %+v", result)
	}
}

func TestRemoteSecurityPolicyTemplateLifecycle(t *testing.T) {
	service := NewService()
	templates := service.RemoteSecurityPolicyTemplates()
	if len(templates) < 3 {
		t.Fatalf("expected built-in templates, got %d", len(templates))
	}
	item := templates[0]
	values := item.Values
	values.MaxSessionMinutes = 20
	updated, err := service.SaveRemoteSecurityPolicyTemplate(RemoteSecurityPolicyTemplateInput{ID: item.ID, Name: item.Name, Description: item.Description, Values: values, ChangeNote: "test update"}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if updated.CurrentVersion != item.CurrentVersion+1 || updated.Values.MaxSessionMinutes != 20 {
		t.Fatalf("unexpected template update: %+v", updated)
	}
	created, err := service.SaveRemoteSecurityPolicyTemplate(RemoteSecurityPolicyTemplateInput{Name: "自定义模板", Description: "test", Values: values}, "tester")
	if err != nil || created.ID == "" || created.CurrentVersion != 1 {
		t.Fatalf("unexpected template create: %+v err=%v", created, err)
	}
}
