package discovery

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCustomRemoteCommandLifecycle(t *testing.T) {
	service := NewService()
	service.credResolver = func(string) (string, string, error) { return "tester", "secret", nil }
	service.remoteRunner = func(_ context.Context, target string, _ int, command, username, secret string, _ time.Duration) (string, error) {
		if target != "172.28.69.161" || command != "uptime" || username != "tester" || secret != "secret" {
			t.Fatalf("unexpected remote request: %s %s %s %s", target, command, username, secret)
		}
		return "load average: 0.01", nil
	}
	execution, err := service.CreateRemoteExecution(RemoteExecutionInput{Command: "uptime", Targets: []string{"172.28.69.161"}, CredentialID: "cred-test", RequestedBy: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if execution.Risk != "high" || execution.OperationName != "自定义命令" || execution.Command != "uptime" {
		t.Fatalf("unexpected custom execution: %+v", execution)
	}
	if _, err = service.ApproveRemoteExecution(execution.ID, "approver"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, err := service.RemoteExecution(execution.ID)
		if err == nil && current.Status != "running" {
			if current.Status != "success" || len(current.Results) != 1 || !strings.Contains(current.Results[0].Output, "load average") {
				t.Fatalf("unexpected result: %+v", current)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("remote execution did not finish")
}

func TestDangerousRemoteCommandRejected(t *testing.T) {
	service := NewService()
	_, err := service.CreateRemoteExecution(RemoteExecutionInput{Command: "rm -rf /", Targets: []string{"172.28.69.161"}, CredentialID: "cred-test", RequestedBy: "admin"})
	if err != ErrRemoteValidation {
		t.Fatalf("expected dangerous command rejection, got %v", err)
	}
}
