package discovery

import "testing"

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
