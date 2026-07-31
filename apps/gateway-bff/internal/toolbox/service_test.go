package toolbox

import "testing"

func TestRunAndHistory(t *testing.T) {
	s := NewService()
	r, err := s.Run(RunInput{ToolID: "ping", Target: "10.20.1.11", Operator: "admin"})
	if err != nil || r.Status != "success" || len(r.Output) == 0 {
		t.Fatalf("run: %#v %v", r, err)
	}
	if len(s.History()) != 3 {
		t.Fatal("history not archived")
	}
}
func TestRejectUnknownTool(t *testing.T) {
	if _, err := NewService().Run(RunInput{ToolID: "shell", Target: "host"}); err == nil {
		t.Fatal("expected validation")
	}
}
