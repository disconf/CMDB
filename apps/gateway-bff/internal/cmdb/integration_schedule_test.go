package cmdb

import (
	"testing"
	"time"
)

func TestIntegrationSchedulePersistsAndSurvivesConfigUpdate(t *testing.T) {
	service := NewService()
	if _, err := service.SaveIntegrationConfig(IntegrationConfigInput{Provider: "aliyun", URL: "https://ecs.aliyuncs.com#region=cn-hangzhou", Username: "ak", AuthType: "access-key", Enabled: true}, "admin"); err != nil {
		t.Fatal(err)
	}
	scheduled, err := service.SaveIntegrationSchedule("aliyun", IntegrationScheduleInput{Enabled: true, IntervalMinutes: 15}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !scheduled.AutoSyncEnabled || scheduled.SyncIntervalMinutes != 15 || scheduled.NextSyncAt == "" {
		t.Fatalf("unexpected schedule: %#v", scheduled)
	}
	updated, err := service.SaveIntegrationConfig(IntegrationConfigInput{Provider: "aliyun", URL: "https://ecs.aliyuncs.com#region=cn-shanghai", Username: "ak2", AuthType: "access-key", Enabled: true}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !updated.AutoSyncEnabled || updated.SyncIntervalMinutes != 15 || updated.NextSyncAt == "" {
		t.Fatalf("config update lost schedule: %#v", updated)
	}
	result, err := service.SaveIntegrationSyncResult("aliyun", "success", "同步完成", 8, time.Now().Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if result.LastSyncStatus != "success" || result.LastSyncCount != 8 || result.LastSyncMessage != "同步完成" {
		t.Fatalf("unexpected sync result: %#v", result)
	}
	if _, err = service.SaveIntegrationSchedule("unknown", IntegrationScheduleInput{Enabled: true, IntervalMinutes: 15}, "admin"); err == nil {
		t.Fatal("expected unconfigured provider schedule to fail")
	}
}
