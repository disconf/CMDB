package discovery

import "testing"

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
