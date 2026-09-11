package jobs

import (
	"testing"
	"time"
)

func TestNextCronTimeSupportsCommonExpressions(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	from := time.Date(2026, 9, 11, 10, 2, 30, 0, location)
	tests := []struct {
		expression string
		want       string
	}{
		{"*/15 * * * *", "2026-09-11 10:15:00"},
		{"0 2 * * *", "2026-09-12 02:00:00"},
		{"0 0 * * 0", "2026-09-13 00:00:00"},
		{"@daily", "2026-09-12 00:00:00"},
	}
	for _, test := range tests {
		got, err := nextCronTime(test.expression, from)
		if err != nil {
			t.Fatalf("nextCronTime(%q): %v", test.expression, err)
		}
		if got.Format(scheduleTimeLayout) != test.want {
			t.Fatalf("nextCronTime(%q) = %s, want %s", test.expression, got.Format(scheduleTimeLayout), test.want)
		}
	}
}

func TestNextCronTimeRejectsInvalidExpression(t *testing.T) {
	if _, err := nextCronTime("61 * * * *", time.Now()); err == nil {
		t.Fatal("expected invalid minute to be rejected")
	}
	if _, err := nextCronTime("* * *", time.Now()); err == nil {
		t.Fatal("expected missing cron field to be rejected")
	}
}

func TestScheduleLifecycle(t *testing.T) {
	service := NewService()
	enabled := true
	item, err := service.CreateSchedule(ScheduleInput{Name: "daily health", TemplateID: "tpl-health", Targets: []string{"host-1", "host-1", "host-2"}, Cron: "0 2 * * *", TimeoutSeconds: 300, Enabled: &enabled}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Targets) != 2 || item.NextRun == "" || item.TemplateName == "" {
		t.Fatalf("unexpected schedule: %+v", item)
	}
	item, err = service.UpdateSchedule(item.ID, ScheduleInput{Name: "weekly health", TemplateID: "tpl-health", Targets: []string{"host-1"}, Cron: "0 3 * * 0", TimeoutSeconds: 600, Enabled: &enabled})
	if err != nil || item.Name != "weekly health" || item.TimeoutSeconds != 600 {
		t.Fatalf("update failed: %+v %v", item, err)
	}
	item, err = service.ToggleSchedule(item.ID)
	if err != nil || item.Enabled || item.NextRun != "" {
		t.Fatalf("toggle failed: %+v %v", item, err)
	}
	if err := service.DeleteSchedule(item.ID); err != nil {
		t.Fatal(err)
	}
	if len(service.Schedules()) != 0 {
		t.Fatal("schedule was not deleted")
	}
}

func TestRunScheduleCreatesExecution(t *testing.T) {
	service := NewService()
	enabled := true
	item, err := service.CreateSchedule(ScheduleInput{Name: "manual schedule", TemplateID: "tpl-health", Targets: []string{"host-1"}, Cron: "0 2 * * *", TimeoutSeconds: 300, Enabled: &enabled}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	job, err := service.RunSchedule(item.ID, "tester", false)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID == "" || job.TemplateID != "tpl-health" {
		t.Fatalf("unexpected scheduled execution: %+v", job)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		current, err := service.Get(job.ID)
		if err == nil && current.Status != "running" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	schedules := service.Schedules()
	if len(schedules) != 1 || schedules[0].LastJobID != job.ID || schedules[0].LastStatus == "" {
		t.Fatalf("schedule execution was not recorded: %+v", schedules)
	}
}
