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
