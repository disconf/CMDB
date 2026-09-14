package discovery

import (
	"bytes"
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
