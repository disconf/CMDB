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
		t.Fatalf("expected same-day duplicate as conflict, got %+v", res2)
	}
}

func TestIngestDedupesByIP(t *testing.T) {
	cmdbService := cmdb.NewService()
	s := NewServiceWithCMDB(cmdbService)
	if _, err := s.Ingest(IngestInput{Source: "agent", Items: []DiscoveredItem{{ID: "host-dup-1", Name: "dup-1", IP: "10.99.9.9", Type: "virtual-machine"}}}); err != nil {
		t.Fatalf("ingest1: %v", err)
	}
	res, err := s.Ingest(IngestInput{Source: "ssh", Items: []DiscoveredItem{{ID: "ssh-dup-1", Name: "dup-1-ssh", IP: "10.99.9.9", Type: "virtual-machine"}}})
	if err != nil {
		t.Fatalf("ingest2: %v", err)
	}
	if res.Merged != 1 || res.Imported != 0 {
		t.Fatalf("expected merge by ip, got %+v", res)
	}
}

func TestIngestRequiresApprovalAndApproves(t *testing.T) {
	cmdbService := cmdb.NewService()
	s := NewServiceWithCMDB(cmdbService)
	s.tasks = nil
	in := IngestInput{
		Source:          "approval-test",
		Scope:           "10.99.0.0/24",
		RequireApproval: true,
		Items: []DiscoveredItem{{
			ID: "approval-test-ok", Name: "approval-test-ok", IP: "10.99.1.1", Type: "physical-server", Confidence: 97,
			Attributes: []cmdb.Attribute{{Name: "room", Label: "机房", Value: "A-01"}},
		}},
	}
	result, err := s.Ingest(in)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if result.Accepted != 1 || result.Imported != 0 {
		t.Fatalf("expected staged result, got %+v", result)
	}
	if _, err := cmdbService.GetAsset("approval-test-ok"); err == nil {
		t.Fatal("asset must not be created before approval")
	}
	pending, err := s.PendingList()
	if err != nil || len(pending) != 1 || pending[0].ID != "approval-test-ok" {
		t.Fatalf("unexpected pending list %+v err=%v", pending, err)
	}
	status, err := s.ApprovePending("approval-test-ok")
	if err != nil || status != "imported" {
		t.Fatalf("approve status=%q err=%v", status, err)
	}
	asset, err := cmdbService.GetAsset("approval-test-ok")
	if err != nil || asset.ID != "approval-test-ok" {
		t.Fatalf("approved asset missing: %+v err=%v", asset, err)
	}
	pending, err = s.PendingList()
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending should be empty: %+v err=%v", pending, err)
	}
	if _, err := s.ApprovePending("approval-test-ok"); err != ErrNotFound {
		t.Fatalf("second approval error=%v", err)
	}
}

func TestIngestRequiresApprovalAndRejects(t *testing.T) {
	cmdbService := cmdb.NewService()
	s := NewServiceWithCMDB(cmdbService)
	s.tasks = nil
	_, err := s.Ingest(IngestInput{
		Source:          "approval-test",
		RequireApproval: true,
		Items:           []DiscoveredItem{{ID: "approval-test-no", Name: "approval-test-no", IP: "10.99.1.2", Type: "virtual-machine"}},
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if err := s.RejectPending("approval-test-no"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if _, err := cmdbService.GetAsset("approval-test-no"); err == nil {
		t.Fatal("rejected asset must not be created")
	}
	pending, err := s.PendingList()
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending should be empty: %+v err=%v", pending, err)
	}
	if err := s.RejectPending("approval-test-no"); err != ErrNotFound {
		t.Fatalf("second rejection error=%v", err)
	}
}
