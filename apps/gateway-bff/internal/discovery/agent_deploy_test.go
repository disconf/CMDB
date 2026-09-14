package discovery

import "testing"

func TestParseAgentPrecheckOutput(t *testing.T) {
	output := "OS=Linux\nKERNEL=5.15.0\nARCH=x86_64\nROOT=true\nSYSTEMD=true\nCURL=true\nDISK_FREE_KB=1048576\nGATEWAY_CODE=200\n"
	result := parseAgentPrecheckOutput("10.0.0.8", output)
	if !result.OK || !result.Root || !result.Systemd || !result.Curl || !result.GatewayOK {
		t.Fatalf("expected precheck to pass: %+v", result)
	}
	if result.DiskFreeKB != 1048576 || result.OS != "Linux" || result.Architecture != "x86_64" {
		t.Fatalf("unexpected parsed values: %+v", result)
	}
}

func TestParseAgentPrecheckOutputReportsBlockingIssues(t *testing.T) {
	output := "OS=Linux\nKERNEL=5.15.0\nARCH=x86_64\nROOT=false\nSYSTEMD=false\nCURL=true\nDISK_FREE_KB=4096\nGATEWAY_CODE=503\n"
	result := parseAgentPrecheckOutput("10.0.0.9", output)
	if result.OK {
		t.Fatalf("expected precheck to fail: %+v", result)
	}
	if result.Message == "" {
		t.Fatal("expected a blocking message")
	}
}

func TestAgentDeploymentHistoryIsCloned(t *testing.T) {
	service := &Service{agentDeployments: []AgentDeployment{}}
	item := AgentDeployment{ID: "agent-deploy-test", Action: "install", Status: "partial", Hosts: []string{"10.0.0.8"}, Results: []AgentInstallResult{{Host: "10.0.0.8", OK: true}}, Total: 1, Succeeded: 1, CreatedAt: "2026-09-15T00:00:00Z"}
	if err := service.saveAgentDeployment(item); err != nil {
		t.Fatalf("save deployment: %v", err)
	}
	items, err := service.AgentDeployments(10)
	if err != nil {
		t.Fatalf("list deployments: %v", err)
	}
	if len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("unexpected deployment list: %+v", items)
	}
	items[0].Hosts[0] = "mutated"
	again, err := service.AgentDeployment(item.ID)
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if again.Hosts[0] != "10.0.0.8" {
		t.Fatalf("deployment history was not cloned: %+v", again)
	}
}
