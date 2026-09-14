package discovery

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestClaimDueCollectionScheduleIntegration(t *testing.T) {
	databaseURL := os.Getenv("CMDB_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CMDB_INTEGRATION_DATABASE_URL is not configured")
	}
	t.Setenv("DATABASE_URL", databaseURL)
	s := NewServiceWithCMDB(nil)
	if s.db == nil {
		t.Fatal("database not initialized")
	}
	t.Cleanup(func() { _ = s.db.Close() })
	id := "integration-schedule-" + time.Now().UTC().Format("20060102150405.000000000")
	t.Cleanup(func() { _, _ = s.db.Exec("DELETE FROM discovery_schedules WHERE id=$1", id) })
	_, err := s.db.Exec(`INSERT INTO discovery_schedules(id,name,kind,cidrs,port,ports,default_type,credential_id,require_approval,interval_minutes,enabled,running,next_run_at,created_by,created_at,updated_at)
VALUES($1,'integration schedule','node-exporter','["127.0.0.1/32"]'::jsonb,9100,'[9100]'::jsonb,'','',false,1440,true,false,now()-interval '1 hour','integration-test',now(),now())`, id)
	if err != nil {
		t.Fatalf("insert due schedule: %v", err)
	}
	item, found, err := s.claimDueCollectionSchedule(context.Background())
	if err != nil {
		t.Fatalf("claim due schedule: %v", err)
	}
	if !found || item.ID != id || !item.Running {
		t.Fatalf("unexpected claim result found=%v item=%+v", found, item)
	}
}
