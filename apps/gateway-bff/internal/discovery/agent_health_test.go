package discovery

import "testing"

func TestParseAgentHealthOutputHealthy(t *testing.T) {
	t.Setenv("CMDB_AGENT_VERSION", "0.1.2")
	output := "HOSTNAME=host-01\nAGENT_BINARY=true\nAGENT_ACTIVE=active\nAGENT_ENABLED=enabled\nAGENT_VERSION=cmdb-agent version 0.1.2\nRESTART_COUNT=0\nUPTIME_SECONDS=86400\nCPU_COUNT=4\nDISK_FREE_KB=10485760\nDISK_USAGE_PERCENT=42\nMEMORY_USAGE_PERCENT=38\nLOAD1=0.4\nGATEWAY_CODE=200\nNODE_EXPORTER_ACTIVE=active\nNODE_EXPORTER_PORTS=19100,9100\n"
	result := parseAgentHealthOutput("10.0.0.8", output)
	if !result.OK || result.Status != "healthy" {
		t.Fatalf("expected healthy result: %+v", result)
	}
	if result.NodeExporterPort != 19100 || len(result.NodeExporterPorts) != 2 {
		t.Fatalf("unexpected exporter ports: %+v", result.NodeExporterPorts)
	}
	if result.Hostname != "host-01" || !result.GatewayOK {
		t.Fatalf("unexpected parsed fields: %+v", result)
	}
}

func TestParseAgentHealthOutputCritical(t *testing.T) {
	t.Setenv("CMDB_AGENT_VERSION", "0.1.2")
	output := "HOSTNAME=host-02\nAGENT_BINARY=false\nAGENT_ACTIVE=inactive\nAGENT_ENABLED=disabled\nAGENT_VERSION=unknown\nRESTART_COUNT=30\nUPTIME_SECONDS=100\nCPU_COUNT=2\nDISK_FREE_KB=100\nDISK_USAGE_PERCENT=95\nMEMORY_USAGE_PERCENT=93\nLOAD1=12\nGATEWAY_CODE=503\nNODE_EXPORTER_ACTIVE=inactive\nNODE_EXPORTER_PORTS=19100\n"
	result := parseAgentHealthOutput("10.0.0.9", output)
	if result.OK || result.Status != "critical" || result.Score >= 60 {
		t.Fatalf("expected critical result: %+v", result)
	}
	if len(result.Issues) < 5 {
		t.Fatalf("expected multiple issues: %+v", result.Issues)
	}
}

func TestAgentHealthHistoryIsCloned(t *testing.T) {
	service := &Service{agentHealthChecks: []AgentHealthCheck{}}
	item := AgentHealthCheck{ID: "agent-health-test", Status: "critical", Hosts: []string{"10.0.0.8"}, Results: []AgentHealthResult{{Host: "10.0.0.8", Status: "critical", Issues: []AgentHealthIssue{{Code: "test"}}}}, Remediation: []AgentRemediationResult{}, Total: 1, Critical: 1, CreatedAt: "2026-09-15T00:00:00Z"}
	if err := service.saveAgentHealthCheck(item); err != nil {
		t.Fatalf("save health check: %v", err)
	}
	items, err := service.AgentHealthChecks(10)
	if err != nil || len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("list health checks: %+v err=%v", items, err)
	}
	items[0].Results[0].Issues[0].Code = "mutated"
	again, err := service.AgentHealthCheck(item.ID)
	if err != nil || again.Results[0].Issues[0].Code != "test" {
		t.Fatalf("health check was not cloned: %+v err=%v", again, err)
	}
}

func TestNormalizeAgentHealthInput(t *testing.T) {
	in := normalizeAgentHealthInput(AgentHealthInput{Hosts: []string{"10.0.0.8", "10.0.0.8", " 10.0.0.9 "}})
	if in.Port != 22 || len(in.Hosts) != 2 || in.Hosts[0] != "10.0.0.8" {
		t.Fatalf("unexpected normalized input: %+v", in)
	}
}

func TestExpandAgentHealthHosts(t *testing.T) {
	hosts, err := expandAgentHealthHosts([]string{"10.0.0.1", "10.0.0.0/30"})
	if err != nil {
		t.Fatalf("expand hosts: %v", err)
	}
	if len(hosts) != 4 {
		t.Fatalf("expected five hosts, got %+v", hosts)
	}
	if _, err := expandAgentHealthHosts([]string{"10.0.0.0/8"}); err == nil {
		t.Fatal("expected oversized CIDR to be rejected")
	}
}
