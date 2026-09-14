package discovery

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type bufferWriteCloser struct {
	bytes.Buffer
}

func (b *bufferWriteCloser) Close() error { return nil }

func TestBlockedTerminalCommand(t *testing.T) {
	for _, command := range []string{
		"rm -rf /",
		"sudo rm -rf /etc",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda",
		"reboot",
		"kill -9 1",
		":(){ :|:& };:",
	} {
		if reason := BlockedTerminalCommand(command); reason == "" {
			t.Fatalf("expected command to be blocked: %s", command)
		}
	}
	for _, command := range []string{"uptime", "rm -f /tmp/demo.log", "systemctl status sshd", "docker ps"} {
		if reason := BlockedTerminalCommand(command); reason != "" {
			t.Fatalf("expected command to be allowed: %s (%s)", command, reason)
		}
	}
}

func TestTerminalTicketIsOneTimeAndExpires(t *testing.T) {
	service := NewService()
	service.remoteSessions = []RemoteSession{{ID: "session-1", AssetID: "asset-1", AssetName: "host-1", Operator: "admin", Roles: []string{"platform-admin"}, Status: "active", ExpiresAt: "2099-01-01 00:00:00"}}
	ticket, err := service.CreateTerminalTicket("session-1", "admin", []string{"platform-admin"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := service.ConsumeTerminalTicket(ticket.Ticket)
	if err != nil || item.ID != "session-1" {
		t.Fatalf("consume ticket: item=%+v err=%v", item, err)
	}
	if _, err = service.ConsumeTerminalTicket(ticket.Ticket); err != ErrNotFound {
		t.Fatalf("expected one-time ticket rejection, got %v", err)
	}
}

func TestTerminalApprovalReject(t *testing.T) {
	service := NewService()
	service.remoteSessions = []RemoteSession{{ID: "session-1", AssetID: "asset-1", AssetName: "host-1", IP: "10.0.0.1", Operator: "alice", Roles: []string{"operator"}, Status: "active", ExpiresAt: "2099-01-01 00:00:00"}}
	approval, err := service.RequestTerminalApproval("session-1", "rm -rf /", "alice", []string{"operator"})
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "pending" || approval.Reason == "" || approval.Command != "rm -rf /" {
		t.Fatalf("unexpected approval: %+v", approval)
	}
	decided, err := service.DecideTerminalApproval(approval.ID, "reject", "不允许", "admin", []string{"platform-admin"})
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != "rejected" || decided.Approver != "admin" {
		t.Fatalf("unexpected decision: %+v", decided)
	}
}

func TestApprovedTerminalApprovalExecutesOnce(t *testing.T) {
	service := NewService()
	service.remoteSessions = []RemoteSession{{ID: "session-1", AssetID: "asset-1", AssetName: "host-1", IP: "10.0.0.1", Operator: "admin", Roles: []string{"platform-admin"}, Status: "active", ExpiresAt: "2099-01-01 00:00:00"}}
	stdin := &bufferWriteCloser{}
	service.terminals["session-1"] = &RemoteTerminalChannel{stdin: stdin, service: service, sessionID: "session-1"}
	approval, err := service.RequestTerminalApproval("session-1", "rm -rf /", "admin", []string{"platform-admin"})
	if err != nil {
		t.Fatal(err)
	}
	decided, err := service.DecideTerminalApproval(approval.ID, "approve", "已确认", "admin", []string{"platform-admin"})
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != "approved" {
		t.Fatalf("expected approved, got %+v", decided)
	}
	if got := stdin.String(); got != "rm -rf /\r" {
		t.Fatalf("expected one-time command write, got %q", got)
	}
	if _, err = service.DecideTerminalApproval(approval.ID, "approve", "", "admin", []string{"platform-admin"}); err == nil {
		t.Fatal("expected approved command replay to be rejected")
	}
	if !strings.Contains(service.terminalApprovals[0].DecisionComment, "已确认") {
		t.Fatalf("decision comment was not persisted")
	}
}

func TestRemoteSessionCollaboratorAccess(t *testing.T) {
	service := NewService()
	service.remoteSessions = []RemoteSession{{ID: "session-1", AssetID: "asset-1", AssetName: "host-1", IP: "10.0.0.1", Operator: "owner", Roles: []string{"operator"}, Collaborators: []RemoteSessionCollaborator{}, Status: "active", ExpiresAt: "2099-01-01 00:00:00"}}
	if _, err := service.AddRemoteSessionCollaborator("session-1", RemoteSessionCollaboratorInput{Username: "viewer1", Access: "readonly"}, "owner", []string{"operator"}); err != nil {
		t.Fatal(err)
	}
	item, _, err := service.sessionForOperator("session-1", "viewer1", []string{"operator"})
	if err != nil {
		t.Fatal(err)
	}
	if item.AccessMode != "readonly" {
		t.Fatalf("expected readonly access, got %q", item.AccessMode)
	}
	if _, err = service.AddRemoteSessionCollaborator("session-1", RemoteSessionCollaboratorInput{Username: "viewer2", Access: "control"}, "viewer1", []string{"operator"}); err != ErrRemoteForbidden {
		t.Fatalf("expected collaborator management to be forbidden, got %v", err)
	}
	if _, err = service.RemoveRemoteSessionCollaborator("session-1", "viewer1", "owner", []string{"operator"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.sessionForOperator("session-1", "viewer1", []string{"operator"}); err != ErrRemoteForbidden {
		t.Fatalf("expected removed collaborator to be forbidden, got %v", err)
	}
}

func TestRemoteTerminalBroadcastsToCollaborators(t *testing.T) {
	channel := &RemoteTerminalChannel{subscribers: map[uint64]chan []byte{}, subscriberAccess: map[uint64]string{}}
	controller, ok := channel.Subscribe("control")
	if !ok {
		t.Fatal("subscribe controller")
	}
	viewer, ok := channel.Subscribe("readonly")
	if !ok {
		t.Fatal("subscribe viewer")
	}
	controllerOutput, _ := channel.Output(controller)
	viewerOutput, _ := channel.Output(viewer)
	channel.broadcast([]byte("shared terminal output"))
	for name, output := range map[string]<-chan []byte{"controller": controllerOutput, "viewer": viewerOutput} {
		select {
		case chunk := <-output:
			if string(chunk) != "shared terminal output" {
				t.Fatalf("%s received %q", name, chunk)
			}
		default:
			t.Fatalf("%s did not receive broadcast", name)
		}
	}
	if remaining := channel.Unsubscribe(controller); remaining != 1 {
		t.Fatalf("expected one remaining subscriber, got %d", remaining)
	}
	if _, controllerOnline := channel.Presence(); controllerOnline {
		t.Fatal("expected controller offline after unsubscribe")
	}
}

func TestCloneRemoteSessionKeepsJSONArrays(t *testing.T) {
	payload, err := json.Marshal(cloneRemoteSession(RemoteSession{}))
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(payload)
	for _, field := range []string{`"roles":[]`, `"collaborators":[]`, `"logs":[]`} {
		if !strings.Contains(encoded, field) {
			t.Fatalf("expected %s in %s", field, encoded)
		}
	}
}
