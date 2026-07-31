package discovery

import "testing"

func TestRegisterHeartbeatAndSummary(t *testing.T) {
	s := NewService()
	a, err := s.Register(RegisterInput{Name: "edge-agent-03", Hostname: "edge-03", IP: "10.8.1.3", OS: "linux", Region: "华东"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Heartbeat(a.ID, "1.2.0"); err != nil {
		t.Fatal(err)
	}
	summary := s.Summary()
	if summary.Total != 4 || summary.Online != 3 {
		t.Fatalf("unexpected summary %+v", summary)
	}
}
func TestCreateDiscoveryTaskAndReconcile(t *testing.T) {
	s := NewService()
	task, err := s.CreateTask(CreateTaskInput{Name: "Linux 主机发现", Source: "agent", Scope: "华东生产区"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.RunTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Discovered == 0 || len(result.Items) == 0 {
		t.Fatal("expected discovery results")
	}
	done, err := s.Reconcile(task.ID)
	if err != nil || done.Imported != result.Discovered {
		t.Fatalf("reconcile failed %+v %v", done, err)
	}
}
