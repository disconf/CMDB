package releases

import "testing"

func TestStartAndRollback(t *testing.T) {
	s := NewService()
	r, err := s.Start(StartInput{Application: "订单服务", Version: "v2.8.0", Environment: "staging", Operator: "admin"})
	if err != nil || r.Status != "running" {
		t.Fatalf("start: %#v %v", r, err)
	}
	r, err = s.Rollback("rel-003", "admin")
	if err != nil || r.Status != "rolling-back" {
		t.Fatalf("rollback: %#v %v", r, err)
	}
}
func TestInvalidRelease(t *testing.T) {
	if _, err := NewService().Start(StartInput{}); err == nil {
		t.Fatal("expected validation")
	}
}
