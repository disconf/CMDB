package discovery

import (
	"testing"
	"time"
)

func TestAccessGrantNormalization(t *testing.T) {
	enabled := true
	grant, err := normalizeAccessGrant(AccessGrantInput{SubjectType: "user", Subject: "alice", ProjectGroup: "ops", Permissions: []string{"file", "terminal", "invalid"}, Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	if grant.Subject != "alice" || len(grant.Permissions) != 2 || !grant.Enabled {
		t.Fatalf("unexpected grant: %+v", grant)
	}
}

func TestRemoteAccessAuthorization(t *testing.T) {
	service := NewService()
	service.accessGrants = []AccessGrant{{ID: "g1", SubjectType: "user", Subject: "alice", ProjectGroup: "ops", Permissions: []string{"terminal"}, Enabled: true}}
	if !service.authorizeRemoteAccess("alice", []string{"viewer"}, "host-1", "ops", "terminal") {
		t.Fatal("expected terminal grant to match")
	}
	if service.authorizeRemoteAccess("alice", []string{"viewer"}, "host-1", "ops", "file") {
		t.Fatal("file permission should not be granted")
	}
	if !service.authorizeRemoteAccess("anyone", []string{"admin"}, "host-1", "ops", "file") {
		t.Fatal("admin should bypass grants")
	}
}

func TestRemotePathValidation(t *testing.T) {
	for _, value := range []string{"/tmp/file", "/opt/app/config.yml"} {
		if err := validateRemotePath(value); err != nil {
			t.Fatalf("valid path rejected: %s", value)
		}
	}
	for _, value := range []string{"", "../etc/passwd", "/tmp/../etc/passwd"} {
		if err := validateRemotePath(value); err == nil {
			t.Fatalf("invalid path accepted: %s", value)
		}
	}
}

func TestRemoteSessionHistoryAndReplayAuthorization(t *testing.T) {
	service := NewService()
	service.remoteSessions = []RemoteSession{
		{ID: "new", Operator: "alice", AssetName: "new-host", Status: "closed", Logs: []RemoteSessionLog{{Time: "10:00:01", Level: "success", Kind: "session", Message: "closed"}}},
		{ID: "old", Operator: "alice", AssetName: "old-host", Status: "closed", Logs: []RemoteSessionLog{{Time: "09:00:01", Level: "success", Kind: "session", Message: "closed"}}},
		{ID: "bob", Operator: "bob", AssetName: "bob-host", Status: "closed"},
	}
	history := service.RemoteSessionHistory("alice", []string{"viewer"})
	if len(history) != 2 || history[0].ID != "new" || history[1].ID != "old" {
		t.Fatalf("unexpected user history order: %+v", history)
	}
	if _, err := service.RemoteSessionReplay("bob", "alice", []string{"viewer"}); err != ErrNotFound {
		t.Fatalf("expected replay authorization to hide another user's session, got %v", err)
	}
	replay, err := service.RemoteSessionReplay("bob", "alice", []string{"platform-admin"})
	if err != nil || replay.ID != "bob" {
		t.Fatalf("admin replay failed: %+v err=%v", replay, err)
	}
}

func TestAppendRemoteSessionLogPersistsStructuredEntry(t *testing.T) {
	service := NewService()
	service.remoteSessions = []RemoteSession{{ID: "session-1", Operator: "alice", Status: "active"}}
	updated, ok := service.appendSessionLog("session-1", "success", "command", "hello", 1250*time.Millisecond)
	if !ok {
		t.Fatal("session log was not appended")
	}
	if len(updated.Logs) != 1 || updated.Logs[0].Kind != "command" || updated.Logs[0].DurationMS != 1250 {
		t.Fatalf("unexpected structured log: %+v", updated.Logs)
	}
}
