package auth

import (
	"errors"
	"testing"
)

func TestLoginReturnsAdminSession(t *testing.T) {
	service := NewService()
	session, err := service.Login("admin", "admin123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if session.Token == "" {
		t.Fatal("expected session token")
	}
	if session.User.DisplayName != "运维管理员" {
		t.Fatalf("unexpected display name %q", session.User.DisplayName)
	}
	if len(session.User.Permissions) < 3 {
		t.Fatal("expected administrator permissions")
	}
}

func TestLoginRejectsInvalidCredentials(t *testing.T) {
	service := NewService()
	_, err := service.Login("admin", "wrong")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
}

func TestCurrentUserAndModulesRespectRole(t *testing.T) {
	service := NewService()
	session, err := service.Login("viewer", "viewer123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	user, err := service.CurrentUser(session.Token)
	if err != nil {
		t.Fatalf("current user: %v", err)
	}
	if user.Username != "viewer" {
		t.Fatalf("unexpected user %q", user.Username)
	}
	modules, err := service.Modules(session.Token)
	if err != nil {
		t.Fatalf("modules: %v", err)
	}
	for _, module := range modules {
		if module.Code == "system" {
			t.Fatal("viewer must not see system management")
		}
	}
}

func TestCurrentUserRejectsUnknownToken(t *testing.T) {
	service := NewService()
	_, err := service.CurrentUser("unknown")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}

func TestSameAccountCanCreateMultipleSessions(t *testing.T) {
	service := NewService()
	first, err := service.Login("admin", "admin123")
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	second, err := service.Login("admin", "admin123")
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if first.Token == second.Token {
		t.Fatal("concurrent sessions must use independent tokens")
	}
	for _, session := range []Session{first, second} {
		user, err := service.CurrentUser(session.Token)
		if err != nil || user.Username != "admin" {
			t.Fatalf("session %s invalid: %+v %v", session.Token, user, err)
		}
	}
}

func TestDeniedPermissionOverridesWildcard(t *testing.T) {
	service := NewService()
	service.SetAccessProfileResolver(func(username string) (AccessProfile, error) {
		return AccessProfile{Permissions: []string{"job:view"}, DeniedPermissions: []string{"system:manage"}}, nil
	})
	user := service.applyAccessProfile(User{Username: "admin", Permissions: []string{"*"}, DataScope: "all"})
	if Allows(user, "system:manage") {
		t.Fatal("denied permission must override wildcard")
	}
	if !Allows(user, "cmdb:view") || !Allows(user, "job:view") {
		t.Fatalf("other permissions should remain: %#v", user)
	}
}

func TestAccessProfileResolverMergesDataScope(t *testing.T) {
	service := NewService()
	service.SetAccessProfileResolver(func(username string) (AccessProfile, error) {
		if username != "viewer" {
			return AccessProfile{}, ErrUnauthorized
		}
		return AccessProfile{DataScope: "groups", ProjectGroups: []string{"核心系统组"}, ScopeTags: []string{"生产"}, Permissions: []string{"cmdb:manage"}}, nil
	})
	session, err := service.Login("viewer", "viewer123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	user, err := service.CurrentUser(session.Token)
	if err != nil {
		t.Fatalf("current user: %v", err)
	}
	if user.DataScope != "groups" || !containsString(user.ProjectGroups, "核心系统组") || !containsString(user.ScopeTags, "生产") || !containsString(user.Permissions, "cmdb:manage") {
		t.Fatalf("access profile was not merged: %#v", user)
	}
}
