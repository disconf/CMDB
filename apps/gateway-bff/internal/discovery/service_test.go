package discovery

import (
	"context"
	"testing"
	"time"

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
	if res2.Conflicts != 0 || res2.Merged != 1 {
		t.Fatalf("expected same-day duplicate refresh as merge, got %+v", res2)
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

func TestTaskDetailIncludesOutcomeAndEvents(t *testing.T) {
	s := NewServiceWithCMDB(cmdb.NewService())
	s.tasks = nil
	s.events = map[string][]TaskEvent{}
	result, err := s.Ingest(IngestInput{
		Source: "detail-test", Scope: "10.99.2.0/24", RequireApproval: true,
		Items: []DiscoveredItem{{ID: "detail-test-001", Name: "detail-test-001", IP: "10.99.2.10", Type: "physical-server"}},
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	detail, err := s.TaskDetail(result.TaskID)
	if err != nil {
		t.Fatalf("task detail: %v", err)
	}
	if detail.Summary.Pending != 1 || len(detail.Events) == 0 {
		t.Fatalf("unexpected pending detail %+v", detail)
	}
	if _, err := s.ApprovePending("detail-test-001"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	detail, err = s.TaskDetail(result.TaskID)
	if err != nil {
		t.Fatalf("task detail after approval: %v", err)
	}
	if detail.Summary.Imported != 1 || detail.Task.Items[0].Result != "imported" {
		t.Fatalf("unexpected approved detail %+v", detail)
	}
}

func TestRemoteExecutionRequiresApprovalAndUsesWhitelist(t *testing.T) {
	t.Setenv("CMDB_SSH_USERNAME", "tester")
	t.Setenv("CMDB_SSH_PASSWORD", "secret")
	s := NewServiceWithCMDB(nil)
	s.tasks = nil
	called := make(chan string, 2)
	s.remoteRunner = func(_ context.Context, target string, port int, command, username, secret string, _ time.Duration) (string, error) {
		called <- command
		return "up 1 day", nil
	}
	execution, err := s.CreateRemoteExecution(RemoteExecutionInput{
		OperationID: "uptime", Targets: []string{"10.0.0.1", "10.0.0.1", "10.0.0.2"}, RequestedBy: "admin",
	})
	if err != nil {
		t.Fatalf("create remote execution: %v", err)
	}
	if execution.Status != "awaiting_approval" || len(execution.Targets) != 2 {
		t.Fatalf("unexpected execution %+v", execution)
	}
	select {
	case command := <-called:
		t.Fatalf("command ran before approval: %s", command)
	default:
	}
	if _, err := s.ApproveRemoteExecution(execution.ID, "approver"); err != nil {
		t.Fatalf("approve remote execution: %v", err)
	}
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		current, err := s.RemoteExecution(execution.ID)
		if err != nil {
			t.Fatalf("get remote execution: %v", err)
		}
		if current.Status == "success" {
			if len(current.Results) != 2 || current.Results[0].Output != "up 1 day" {
				t.Fatalf("unexpected results %+v", current)
			}
			if command := <-called; command != "uptime" {
				t.Fatalf("unexpected command %q", command)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("remote execution did not finish")
}

func TestRemoteExecutionRejectsInjectedTarget(t *testing.T) {
	s := NewServiceWithCMDB(nil)
	if _, err := s.CreateRemoteExecution(RemoteExecutionInput{OperationID: "uptime", Targets: []string{"10.0.0.1;id"}, RequestedBy: "admin"}); err != ErrRemoteValidation {
		t.Fatalf("expected validation error, got %v", err)
	}
}
