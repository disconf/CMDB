package tickets

import "testing"

func TestCreateAndApprove(t *testing.T) {
	s := NewService()
	x, err := s.Create(CreateInput{Title: "扩容订单服务", Type: "change", Priority: "high", Applicant: "admin", Description: "新增两个实例"})
	if err != nil || x.Status != "pending" {
		t.Fatalf("create: %#v %v", x, err)
	}
	x, err = s.Approve(x.ID, "admin", "审批通过")
	if err != nil || x.Status != "approved" || len(x.Timeline) < 2 {
		t.Fatalf("approve: %#v %v", x, err)
	}
}
func TestRejectMissing(t *testing.T) {
	if _, err := NewService().Reject("missing", "admin", "no"); err == nil {
		t.Fatal("expected error")
	}
}
