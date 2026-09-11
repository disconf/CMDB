package jobs

import (
	"strings"
	"testing"
	"time"
)

func TestPlaybookRendering(t *testing.T) {
	playbook := Playbook{Content: "echo {{name}} {{region}}", Constants: map[string]string{"region": "shanghai"}, Variables: map[string]string{"name": "default"}}
	rendered, err := compilePlaybook(playbook, map[string]string{"name": "order-api"})
	if err != nil {
		t.Fatal(err)
	}
	if rendered != "echo order-api shanghai" {
		t.Fatalf("unexpected rendered playbook: %q", rendered)
	}
	if _, err = compilePlaybook(playbook, map[string]string{"region": "other"}); err == nil {
		t.Fatal("expected constant override to be rejected")
	}
	if _, err = compilePlaybook(playbook, nil); err != nil {
		t.Fatalf("defaults should render: %v", err)
	}
}

func TestPlaybookLifecycleAndExecution(t *testing.T) {
	service := NewService()
	enabled := true
	item, err := service.CreatePlaybook(PlaybookInput{Name: "health playbook", Description: "test", Content: "echo {{message}}", Variables: map[string]string{"message": "ok"}, Enabled: &enabled}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if item.ID == "" || len(item.Variables) != 1 {
		t.Fatalf("unexpected playbook: %+v", item)
	}
	job, err := service.Create(CreateInput{PlaybookID: item.ID, Targets: []string{"host-1"}, Operator: "tester", TimeoutSeconds: 300})
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "awaiting_approval" || job.PlaybookID != item.ID {
		t.Fatalf("unexpected playbook job: %+v", job)
	}
	if _, err = service.Approve(job.ID, "approver"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Run(job.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		current, err := service.Get(job.ID)
		if err == nil && current.Status == "success" {
			if len(current.Logs) == 0 || !strings.Contains(strings.Join(logMessages(current.Logs), " "), "任务执行完成") {
				t.Fatalf("missing execution logs: %+v", current.Logs)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("playbook execution did not finish")
}

func logMessages(logs []Log) []string {
	out := make([]string, 0, len(logs))
	for _, item := range logs {
		out = append(out, item.Message)
	}
	return out
}
