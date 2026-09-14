package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"cmdb/gateway-bff/internal/cmdb"
)

func TestCollectionScheduleCRUD(t *testing.T) {
	s := NewService()
	s.schedules = []CollectionSchedule{}
	item, err := s.CreateCollectionSchedule(CollectionScheduleInput{Name: "核心网段扫描", Kind: "node-exporter", CIDRs: []string{"172.28.68.0/24", "172.28.69.0/24"}, Ports: []int{9100, 19100}, IntervalMinutes: 30}, "tester")
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	if item.ID == "" || !item.Enabled || item.NextRunAt == "" || len(item.Ports) != 2 {
		t.Fatalf("unexpected schedule %+v", item)
	}
	list, err := s.CollectionSchedules()
	if err != nil || len(list) != 1 {
		t.Fatalf("list schedules: %+v err=%v", list, err)
	}
	enabled := false
	updated, err := s.UpdateCollectionSchedule(item.ID, CollectionScheduleInput{Name: "核心网段扫描（暂停）", Kind: "node-exporter", CIDRs: item.CIDRs, Ports: item.Ports, IntervalMinutes: 60, Enabled: &enabled})
	if err != nil || updated.Enabled || updated.NextRunAt != "" {
		t.Fatalf("update schedule: %+v err=%v", updated, err)
	}
	toggled, err := s.ToggleCollectionSchedule(item.ID)
	if err != nil || !toggled.Enabled || toggled.NextRunAt == "" {
		t.Fatalf("toggle schedule: %+v err=%v", toggled, err)
	}
	if err := s.DeleteCollectionSchedule(item.ID); err != nil {
		t.Fatalf("delete schedule: %v", err)
	}
	if _, err := s.collectionScheduleByID(item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestCollectionScheduleValidation(t *testing.T) {
	if _, err := normalizeCollectionScheduleInput(CollectionScheduleInput{Kind: "unknown", CIDRs: []string{"10.0.0.0/24"}}); err == nil {
		t.Fatal("expected invalid kind")
	}
	if _, err := normalizeCollectionScheduleInput(CollectionScheduleInput{Kind: "node-exporter", CIDRs: []string{"not-a-cidr"}}); err == nil {
		t.Fatal("expected invalid CIDR")
	}
	if _, err := normalizeCollectionScheduleInput(CollectionScheduleInput{Kind: "ssh", CIDRs: []string{"10.0.0.0/24"}, Port: 70000}); err == nil {
		t.Fatal("expected invalid port")
	}
}

func TestRunCollectionScheduleCreatesTaskAndUpdatesState(t *testing.T) {
	s := NewServiceWithCMDB(cmdb.NewService())
	s.tasks = []Task{}
	s.schedules = []CollectionSchedule{}
	s.scheduleRunner = func(_ context.Context, _ CollectionSchedule, taskID string) (CollectionScheduleRunResult, error) {
		return CollectionScheduleRunResult{TaskID: taskID, Status: "success", Scanned: 254, Found: 12, Adopted: 8, Merged: 4}, nil
	}
	item, err := s.CreateCollectionSchedule(CollectionScheduleInput{Name: "定时主机扫描", Kind: "node-exporter", CIDRs: []string{"172.28.68.0/24"}, IntervalMinutes: 15}, "tester")
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	result, err := s.RunCollectionSchedule(context.Background(), item.ID, "tester")
	if err != nil {
		t.Fatalf("run schedule: %v", err)
	}
	if result.TaskID == "" || result.Scanned != 254 || result.Found != 12 {
		t.Fatalf("unexpected result %+v", result)
	}
	detail, err := s.TaskDetail(result.TaskID)
	if err != nil || detail.Task.Status != "completed" || detail.Summary.Discovered != 0 {
		t.Fatalf("unexpected task detail %+v err=%v", detail, err)
	}
	updated, err := s.collectionScheduleByID(item.ID)
	if err != nil || updated.Running || updated.LastTaskID != result.TaskID || updated.LastStatus != "completed" || updated.NextRunAt == "" {
		t.Fatalf("unexpected schedule state %+v err=%v", updated, err)
	}
}

func TestRunCollectionScheduleRejectsConcurrentRun(t *testing.T) {
	s := NewService()
	s.tasks = []Task{}
	s.schedules = []CollectionSchedule{}
	started := make(chan struct{})
	release := make(chan struct{})
	s.scheduleRunner = func(_ context.Context, _ CollectionSchedule, taskID string) (CollectionScheduleRunResult, error) {
		close(started)
		<-release
		return CollectionScheduleRunResult{TaskID: taskID, Status: "success"}, nil
	}
	item, err := s.CreateCollectionSchedule(CollectionScheduleInput{Name: "并发保护", Kind: "ssh", CIDRs: []string{"10.0.0.0/30"}, IntervalMinutes: 5}, "tester")
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.RunCollectionSchedule(context.Background(), item.ID, "tester")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("schedule did not start")
	}
	if _, err := s.RunCollectionSchedule(context.Background(), item.ID, "tester"); !errors.Is(err, ErrScheduleRunning) {
		t.Fatalf("expected running error, got %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first run failed: %v", err)
	}
}
