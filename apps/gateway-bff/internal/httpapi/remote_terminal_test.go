package httpapi

import "testing"

func TestTerminalInputGuardRequestsApprovalForDangerousCommand(t *testing.T) {
	guard := &terminalInputGuard{}
	forward, command, approvalCommand, blocked := guard.process("rm -rf /")
	if blocked != "" || command != "" || approvalCommand != "" {
		t.Fatalf("command should not complete before enter")
	}
	if forward == "" {
		t.Fatalf("typed characters should still be forwarded")
	}
	forward, command, approvalCommand, blocked = guard.process("\r")
	if blocked == "" || command != "" || approvalCommand != "rm -rf /" {
		t.Fatalf("dangerous command should request approval, forward=%q command=%q approval=%q blocked=%q", forward, command, approvalCommand, blocked)
	}
	if forward != string([]byte{3}) {
		t.Fatalf("expected Ctrl+C to cancel remote command, got %q", forward)
	}
}

func TestTerminalInputGuardAllowsNormalCommand(t *testing.T) {
	guard := &terminalInputGuard{}
	_, _, _, _ = guard.process("uptime")
	forward, command, approvalCommand, blocked := guard.process("\r")
	if blocked != "" || approvalCommand != "" || command != "uptime" || forward != "\r" {
		t.Fatalf("normal command failed: forward=%q command=%q approval=%q blocked=%q", forward, command, approvalCommand, blocked)
	}
}
