package aiops

import "testing"

func TestAnalyzeAndExecute(t *testing.T) {
	s := NewService()
	a, err := s.Analyze("alert-001")
	if err != nil || a.Confidence < 80 || len(a.Evidence) < 2 {
		t.Fatalf("analysis: %#v %v", a, err)
	}
	x, err := s.Execute(a.ID, "rb-memory-relief", "admin")
	if err != nil || x.Status != "pending-approval" {
		t.Fatalf("execute: %#v %v", x, err)
	}
}
func TestKnowledgeSearch(t *testing.T) {
	if len(NewService().SearchKnowledge("OOM")) == 0 {
		t.Fatal("expected knowledge result")
	}
}
