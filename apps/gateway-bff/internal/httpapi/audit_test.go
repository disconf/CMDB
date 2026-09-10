package httpapi

import (
	"net/http"
	"testing"
)

func TestAuditActionAndTarget(t *testing.T) {
	tests := []struct {
		method string
		path   string
		action string
		target string
	}{
		{http.MethodPatch, "/api/v1/cmdb/assets/srv-001", "cmdb.asset.update", "srv-001"},
		{http.MethodPost, "/api/v1/discovery/agent-install", "discovery.agent.install", "agent-install"},
		{http.MethodPost, "/api/v1/discovery/agent-uninstall", "discovery.agent.uninstall", "agent-uninstall"},
		{http.MethodPost, "/api/v1/idc/rooms/room-1/modules", "idc.module.create", "modules"},
		{http.MethodPost, "/api/v1/jobs/executions/job-1/retry", "job.execution.retry", "job-1"},
		{http.MethodPost, "/api/v1/tickets/ticket-1/approve", "ticket.approve", "ticket-1"},
		{http.MethodPost, "/api/v1/system/users/u-1/toggle", "system.user.toggle", "u-1"},
	}
	for _, test := range tests {
		if got := auditAction(test.method, test.path); got != test.action {
			t.Errorf("auditAction(%s, %s)=%q want %q", test.method, test.path, got, test.action)
		}
		if got := auditTarget(test.path); got != test.target {
			t.Errorf("auditTarget(%s)=%q want %q", test.path, got, test.target)
		}
	}
}

func TestShouldAuditMutation(t *testing.T) {
	if shouldAuditMutation(http.MethodPost, "/api/v1/discovery/ingest") {
		t.Fatal("agent ingest must not be recorded as a human mutation")
	}
	if shouldAuditMutation(http.MethodGet, "/api/v1/cmdb/assets") {
		t.Fatal("read operations must not be recorded")
	}
	if !shouldAuditMutation(http.MethodPatch, "/api/v1/cmdb/assets/srv-001") {
		t.Fatal("human asset update should be recorded")
	}
}

func TestResponseID(t *testing.T) {
	if got := responseID([]byte(`{"id":"asset-001","name":"host"}`)); got != "asset-001" {
		t.Fatalf("responseID=%q", got)
	}
	if got := responseID([]byte(`{"message":"ok"}`)); got != "" {
		t.Fatalf("unexpected responseID=%q", got)
	}
}
