package discovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

const collectionScheduleColumns = `id,name,kind,cidrs,port,ports,default_type,credential_id,require_approval,interval_minutes,enabled,running,next_run_at,last_run_at,last_task_id,last_status,last_error,created_by,created_at,updated_at`
const collectionScheduleTargetColumns = `target.id,target.name,target.kind,target.cidrs,target.port,target.ports,target.default_type,target.credential_id,target.require_approval,target.interval_minutes,target.enabled,target.running,target.next_run_at,target.last_run_at,target.last_task_id,target.last_status,target.last_error,target.created_by,target.created_at,target.updated_at`

type collectionScheduleScanner interface {
	Scan(dest ...any) error
}

func scanCollectionSchedule(row collectionScheduleScanner) (CollectionSchedule, error) {
	var item CollectionSchedule
	var cidrsRaw, portsRaw []byte
	var nextRun, lastRun sql.NullTime
	var createdAt, updatedAt time.Time
	err := row.Scan(
		&item.ID, &item.Name, &item.Kind, &cidrsRaw, &item.Port, &portsRaw,
		&item.DefaultType, &item.CredentialID, &item.RequireApproval, &item.IntervalMinutes,
		&item.Enabled, &item.Running, &nextRun, &lastRun, &item.LastTaskID,
		&item.LastStatus, &item.LastError, &item.CreatedBy, &createdAt, &updatedAt,
	)
	if err != nil {
		return CollectionSchedule{}, err
	}
	if len(cidrsRaw) > 0 {
		_ = json.Unmarshal(cidrsRaw, &item.CIDRs)
	}
	if len(portsRaw) > 0 {
		_ = json.Unmarshal(portsRaw, &item.Ports)
	}
	if item.CIDRs == nil {
		item.CIDRs = []string{}
	}
	if item.Ports == nil {
		item.Ports = []int{}
	}
	if nextRun.Valid {
		item.NextRunAt = nextRun.Time.UTC().Format(time.RFC3339)
	}
	if lastRun.Valid {
		item.LastRunAt = lastRun.Time.UTC().Format(time.RFC3339)
	}
	item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return item, nil
}

func (s *Service) loadCollectionSchedules() ([]CollectionSchedule, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT `+collectionScheduleColumns+` FROM discovery_schedules ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CollectionSchedule{}
	for rows.Next() {
		item, err := scanCollectionSchedule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) loadCollectionSchedule(id string) (CollectionSchedule, error) {
	item, err := scanCollectionSchedule(s.db.QueryRowContext(context.Background(), `SELECT `+collectionScheduleColumns+` FROM discovery_schedules WHERE id=$1`, id))
	if err == sql.ErrNoRows {
		return CollectionSchedule{}, ErrNotFound
	}
	return item, err
}

func (s *Service) upsertCollectionSchedule(item CollectionSchedule) error {
	cidrs, err := json.Marshal(item.CIDRs)
	if err != nil {
		return err
	}
	ports, err := json.Marshal(item.Ports)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(context.Background(), `INSERT INTO discovery_schedules(
id,name,kind,cidrs,port,ports,default_type,credential_id,require_approval,interval_minutes,enabled,running,next_run_at,last_run_at,last_task_id,last_status,last_error,created_by,created_at,updated_at)
VALUES($1,$2,$3,$4::jsonb,$5,$6::jsonb,$7,$8,$9,$10,$11,$12,
CASE WHEN $13::text='' THEN NULL ELSE $13::timestamptz END,
CASE WHEN $14::text='' THEN NULL ELSE $14::timestamptz END,
$15,$16,$17,$18,CASE WHEN $19::text='' THEN now() ELSE $19::timestamptz END,now())
ON CONFLICT(id) DO UPDATE SET name=excluded.name,kind=excluded.kind,cidrs=excluded.cidrs,port=excluded.port,ports=excluded.ports,default_type=excluded.default_type,credential_id=excluded.credential_id,require_approval=excluded.require_approval,interval_minutes=excluded.interval_minutes,enabled=excluded.enabled,running=excluded.running,next_run_at=excluded.next_run_at,last_run_at=excluded.last_run_at,last_task_id=excluded.last_task_id,last_status=excluded.last_status,last_error=excluded.last_error,created_by=excluded.created_by,updated_at=now()`,
		item.ID, item.Name, item.Kind, string(cidrs), item.Port, string(ports), item.DefaultType, item.CredentialID,
		item.RequireApproval, item.IntervalMinutes, item.Enabled, item.Running, item.NextRunAt, item.LastRunAt,
		item.LastTaskID, item.LastStatus, item.LastError, item.CreatedBy, item.CreatedAt,
	)
	return err
}

func (s *Service) deleteCollectionScheduleRow(id string) error {
	result, err := s.db.ExecContext(context.Background(), `DELETE FROM discovery_schedules WHERE id=$1`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) claimCollectionScheduleRow(ctx context.Context, id string) (CollectionSchedule, bool, error) {
	row := s.db.QueryRowContext(ctx, `UPDATE discovery_schedules SET running=true,updated_at=now() WHERE id=$1 AND running=false RETURNING `+collectionScheduleColumns, id)
	item, err := scanCollectionSchedule(row)
	if err == sql.ErrNoRows {
		return CollectionSchedule{}, false, nil
	}
	if err != nil {
		return CollectionSchedule{}, false, err
	}
	return item, true, nil
}

func (s *Service) claimDueCollectionSchedule(ctx context.Context) (CollectionSchedule, bool, error) {
	row := s.db.QueryRowContext(ctx, `WITH due AS (
SELECT id FROM discovery_schedules WHERE enabled=true AND running=false AND next_run_at IS NOT NULL AND next_run_at<=now() ORDER BY next_run_at FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE discovery_schedules target SET running=true,updated_at=now() FROM due WHERE target.id=due.id RETURNING `+collectionScheduleTargetColumns)
	item, err := scanCollectionSchedule(row)
	if err == sql.ErrNoRows {
		return CollectionSchedule{}, false, nil
	}
	if err != nil {
		return CollectionSchedule{}, false, fmt.Errorf("claim due discovery schedule: %w", err)
	}
	return item, true, nil
}
