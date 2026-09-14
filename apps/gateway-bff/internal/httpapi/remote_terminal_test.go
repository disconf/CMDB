package httpapi

import "testing"

func TestTerminalInputGuardBlocksDangerousCommand(t *testing.T) {
	guard := &terminalInputGuard{}
	forward, command, blocked := guard.process("rm -rf /")
	if blocked != "" || command != "" {
		t.Fatalf("command should not complete before enter")
	}
	if forward == "" {
		t.Fatalf("typed characters should still be forwarded")
	}
	forward, command, blocked = guard.process("\r")
	if blocked == "" || command != "" {
		t.Fatalf("dangerous command should be blocked, forward=%q command=%q blocked=%q", forward, command, blocked)
	}
	if forward != string([]byte{3}) {
		t.Fatalf("expected Ctrl+C to cancel remote command, got %q", forward)
	}
}

func TestTerminalInputGuardAllowsNormalCommand(t *testing.T) {
	guard := &terminalInputGuard{}
	_, _, _ = guard.process("uptime")
	forward, command, blocked := guard.process("\r")
	if blocked != "" || command != "uptime" || forward != "\r" {
		t.Fatalf("normal command failed: forward=%q command=%q blocked=%q", forward, command, blocked)
	}
}
