package system

import "testing"

func TestToggleUserAndAudit(t *testing.T) {
	s := NewService()
	u, err := s.ToggleUser("u-viewer", "admin")
	if err != nil || u.Status != "disabled" {
		t.Fatalf("toggle: %#v %v", u, err)
	}
	if len(s.Audits()) < 3 {
		t.Fatal("audit not recorded")
	}
}
func TestUpdateRole(t *testing.T) {
	s := NewService()
	r, err := s.UpdateRole("viewer", []string{"dashboard:view", "cmdb:view"}, "admin")
	if err != nil || len(r.Permissions) != 2 {
		t.Fatalf("role: %#v %v", r, err)
	}
}

func TestAccessProfileMergesRoleAndUserScope(t *testing.T) {
	service := NewService()
	profile, err := service.AccessProfile("viewer")
	if err != nil {
		t.Fatalf("access profile: %v", err)
	}
	if profile.DataScope != "groups" || len(profile.Permissions) == 0 || !containsValue(profile.ProjectGroups, "华东业务组") {
		t.Fatalf("unexpected access profile: %#v", profile)
	}
}

func TestUpdateUserDataScope(t *testing.T) {
	service := NewService()
	user, err := service.UpdateUser("u-viewer", UserUpdateInput{DisplayName: "只读用户", Department: "研发中心", Roles: []string{"viewer"}, Status: "active", DataScope: "groups", ProjectGroups: []string{"核心系统组"}, ScopeTags: []string{"生产"}}, "admin")
	if err != nil {
		t.Fatalf("update user: %v", err)
	}
	if user.DataScope != "groups" || !containsValue(user.ProjectGroups, "核心系统组") || !containsValue(user.ScopeTags, "生产") {
		t.Fatalf("user scope not updated: %#v", user)
	}
}

func containsValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
