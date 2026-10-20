package system

import (
	"errors"
	"testing"
)

func TestCreateDeleteUserAndPermissionOverrides(t *testing.T) {
	service := NewService()
	user, password, err := service.CreateUser(UserCreateInput{
		Username: "test-operator", DisplayName: "测试操作员", Department: "运维部",
		Email: "test-operator@example.com", Roles: []string{"viewer"},
		DataScope: "groups", ProjectGroups: []string{"核心系统组"},
		PermissionOverrides: []PermissionOverride{
			{Code: "job:manage", Effect: "allow", Reason: "临时自动化"},
			{Code: "cmdb:view", Effect: "deny", Reason: "仅任务"},
		},
	}, "admin")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if password == "" || user.Status != "active" || user.KeycloakID != "" {
		t.Fatalf("unexpected created user: %#v password=%q", user, password)
	}
	profile, err := service.AccessProfile(user.Username)
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	if containsValue(profile.Permissions, "cmdb:view") {
		t.Fatalf("deny override did not win: %#v", profile.Permissions)
	}
	if !containsValue(profile.Permissions, "job:manage") {
		t.Fatalf("allow override missing: %#v", profile.Permissions)
	}
	if !containsValue(profile.DeniedPermissions, "cmdb:view") {
		t.Fatalf("denied permission not exposed: %#v", profile)
	}
	if _, err = service.DeleteUser(user.ID, "admin"); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	for _, item := range service.Users() {
		if item.ID == user.ID {
			t.Fatal("deleted user still listed")
		}
	}
	if _, err = service.AccessProfile(user.Username); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted user still has access profile: %v", err)
	}
}

func TestLastAdminAndRoleAssignmentProtection(t *testing.T) {
	service := NewService()
	if _, err := service.DeleteUser("u-admin", "other-admin"); !errors.Is(err, ErrConflict) {
		t.Fatalf("last admin must be protected: %v", err)
	}
	if _, err := service.UpdateUser("u-admin", UserUpdateInput{
		DisplayName: "运维管理员", Roles: []string{"viewer"}, Status: "active",
		DataScope: "all",
	}, "other-admin"); !errors.Is(err, ErrConflict) {
		t.Fatalf("last admin role removal must be protected: %v", err)
	}
	if _, err := service.UpdateUser("u-admin", UserUpdateInput{
		DisplayName: "运维管理员", Roles: []string{"platform-admin"}, Status: "disabled",
		DataScope: "all",
	}, "other-admin"); !errors.Is(err, ErrConflict) {
		t.Fatalf("last admin disable must be protected: %v", err)
	}
	if _, err := service.UpdateUser("u-viewer", UserUpdateInput{
		DisplayName: "只读用户", Roles: []string{}, Status: "active", DataScope: "none",
	}, "admin"); err != nil {
		t.Fatalf("roles should be clearable: %v", err)
	}
}

func TestRoleCRUDAndUsageProtection(t *testing.T) {
	service := NewService()
	role, err := service.CreateRole(RoleCreateInput{
		Name: "测试知识编辑", Description: "只允许知识编辑",
		Permissions: []string{"knowledge:view", "knowledge:edit"},
		DataScope:   "groups", ProjectGroups: []string{"核心系统组"},
	}, "admin")
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	if role.System {
		t.Fatal("custom role marked as system")
	}
	updated, err := service.UpdateRoleProfile(role.ID, RoleUpdateInput{
		Name: "测试知识发布", Description: "编辑并发布",
		Permissions: []string{"knowledge:view", "knowledge:edit", "knowledge:publish"},
		DataScope:   "groups",
	}, "admin")
	if err != nil || updated.Name != "测试知识发布" || len(updated.Permissions) != 3 {
		t.Fatalf("update role: %#v %v", updated, err)
	}
	user, _, err := service.CreateUser(UserCreateInput{
		Username: "role-user", DisplayName: "角色用户", Roles: []string{role.ID},
		DataScope: "groups",
	}, "admin")
	if err != nil {
		t.Fatalf("create user with role: %v", err)
	}
	if err = service.DeleteRole(role.ID, "admin"); !errors.Is(err, ErrConflict) {
		t.Fatalf("role in use must not be deleted: %v", err)
	}
	if err = service.DeleteRole("viewer", "admin"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("system role must not be deleted: %v", err)
	}
	if _, err = service.UpdateUser(user.ID, UserUpdateInput{
		DisplayName: user.DisplayName, Roles: []string{"viewer"}, Status: "active",
		DataScope: "groups",
	}, "admin"); err != nil {
		t.Fatalf("remove role from user: %v", err)
	}
	if err = service.DeleteRole(role.ID, "admin"); err != nil {
		t.Fatalf("delete unused role: %v", err)
	}
}

func TestUnknownPermissionRejected(t *testing.T) {
	service := NewService()
	if _, err := service.CreateRole(RoleCreateInput{
		Name: "非法权限", Permissions: []string{"cmdb:fly"}, DataScope: "none",
	}, "admin"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown permission should be rejected: %v", err)
	}
}
