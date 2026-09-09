package discovery

import (
	"testing"

	"cmdb/gateway-bff/internal/cmdb"
)

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

func TestIngestAutoImportsToCMDB(t *testing.T) {
	cmdbService := cmdb.NewService()
	s := NewServiceWithCMDB(cmdbService)
	in := IngestInput{Source: "ssh", Scope: "172.28.69.0/24", Items: []DiscoveredItem{{ID: "ingest-test-001", Name: "ingest-test-001", IP: "10.99.2.1", Type: "virtual-machine", Confidence: 95}}}
	res, err := s.Ingest(in)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if res.Imported != 1 || res.Accepted != 1 {
		t.Fatalf("unexpected result %+v", res)
	}
	if _, err := cmdbService.GetAsset("ingest-test-001"); err != nil {
		t.Fatalf("asset not created: %v", err)
	}
	res2, err := s.Ingest(in)
	if err != nil {
		t.Fatalf("ingest2: %v", err)
	}
	if res2.Conflicts != 1 || res2.Imported != 0 {
		t.Fatalf("expected conflict on re-ingest, got %+v", res2)
	}
}
