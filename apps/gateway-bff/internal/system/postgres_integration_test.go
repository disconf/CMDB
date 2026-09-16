package system

import (
	"context"
	"os"
	"testing"
)

func TestPostgresSystemUserAndRolePersistence(t *testing.T) {
	databaseURL := os.Getenv("CMDB_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CMDB_INTEGRATION_DATABASE_URL is not configured")
	}
	db, err := openPostgres(databaseURL)
	if err != nil {
		t.Fatalf("open system postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	user := User{ID: "integration-platform-user", Username: "integration-platform-user", DisplayName: "Integration User", Department: "QA", Roles: []string{"viewer"}, Status: "active", DataScope: "groups", ProjectGroups: []string{"核心系统组"}, ScopeTags: []string{"生产"}}
	if err = upsertSystemUser(context.Background(), db, user); err != nil {
		t.Fatalf("persist user: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), "DELETE FROM platform_users WHERE id=$1", user.ID) })
	role := Role{ID: "integration-role", Name: "Integration Role", Description: "integration", Permissions: []string{"cmdb:view"}, DataScope: "groups"}
	if err = upsertSystemRole(context.Background(), db, role); err != nil {
		t.Fatalf("persist role: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), "DELETE FROM platform_roles WHERE id=$1", role.ID) })
	users, roles, _, _, err := loadSystemData(context.Background(), db)
	if err != nil {
		t.Fatalf("load system data: %v", err)
	}
	foundUser, foundRole := false, false
	for _, item := range users {
		if item.ID == user.ID && item.DataScope == "groups" && len(item.ProjectGroups) == 1 {
			foundUser = true
		}
	}
	for _, item := range roles {
		if item.ID == role.ID && len(item.Permissions) == 1 {
			foundRole = true
		}
	}
	if !foundUser || !foundRole {
		t.Fatalf("system persistence missing: users=%#v roles=%#v", users, roles)
	}
}
