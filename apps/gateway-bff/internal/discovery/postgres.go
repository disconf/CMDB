package discovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const discoverySchema = `
CREATE TABLE IF NOT EXISTS discovery_tasks (
 id text PRIMARY KEY, name text NOT NULL, source text NOT NULL, scope text NOT NULL DEFAULT '',
 status text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE discovery_tasks ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
CREATE TABLE IF NOT EXISTS discovery_items (
 id text PRIMARY KEY, task_id text NOT NULL REFERENCES discovery_tasks(id) ON DELETE CASCADE,
 name text NOT NULL, ip text NOT NULL DEFAULT '', type text NOT NULL,
 confidence integer NOT NULL DEFAULT 0, state text NOT NULL DEFAULT 'pending',
 result text NOT NULL DEFAULT '', message text NOT NULL DEFAULT '', raw_payload jsonb NOT NULL DEFAULT '{}',
 discovered_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE discovery_items ADD COLUMN IF NOT EXISTS result text NOT NULL DEFAULT '';
ALTER TABLE discovery_items ADD COLUMN IF NOT EXISTS message text NOT NULL DEFAULT '';
ALTER TABLE discovery_items ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX IF NOT EXISTS idx_discovery_items_task_state ON discovery_items(task_id,state);
CREATE TABLE IF NOT EXISTS discovery_task_events (
 id text PRIMARY KEY, task_id text NOT NULL REFERENCES discovery_tasks(id) ON DELETE CASCADE,
 level text NOT NULL, event text NOT NULL, message text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_discovery_task_events_task_time ON discovery_task_events(task_id,created_at);
CREATE TABLE IF NOT EXISTS remote_executions (
 id text PRIMARY KEY, operation_id text NOT NULL, operation_name text NOT NULL, command text NOT NULL DEFAULT '', risk text NOT NULL DEFAULT 'high',
 targets jsonb NOT NULL DEFAULT '[]', port integer NOT NULL DEFAULT 22, credential_id text NOT NULL DEFAULT '', status text NOT NULL,
 requested_by text NOT NULL, approved_by text NOT NULL DEFAULT '', created_at text NOT NULL,
 approved_at text NOT NULL DEFAULT '', started_at text NOT NULL DEFAULT '', finished_at text NOT NULL DEFAULT '',
 results jsonb NOT NULL DEFAULT '[]', updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE remote_executions ADD COLUMN IF NOT EXISTS port integer NOT NULL DEFAULT 22;
ALTER TABLE remote_executions ADD COLUMN IF NOT EXISTS command text NOT NULL DEFAULT '';
ALTER TABLE remote_executions ADD COLUMN IF NOT EXISTS risk text NOT NULL DEFAULT 'high';
CREATE INDEX IF NOT EXISTS idx_remote_executions_updated ON remote_executions(updated_at DESC);
`

const remoteAccessSchema = `
CREATE TABLE IF NOT EXISTS remote_access_grants(
 id text PRIMARY KEY,
 subject_type text NOT NULL,
 subject text NOT NULL,
 asset_id text NOT NULL DEFAULT '',
 project_group text NOT NULL DEFAULT '',
 permissions jsonb NOT NULL DEFAULT '[]',
 enabled boolean NOT NULL DEFAULT true,
 created_by text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_remote_access_grants_subject ON remote_access_grants(subject_type,subject);
CREATE TABLE IF NOT EXISTS remote_host_keys(id text PRIMARY KEY,asset_id text NOT NULL,host text NOT NULL,port integer NOT NULL DEFAULT 22,key_type text NOT NULL,fingerprint text NOT NULL,public_key text NOT NULL,added_by text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now());
CREATE UNIQUE INDEX IF NOT EXISTS idx_remote_host_keys_asset ON remote_host_keys(asset_id,host,port);
CREATE TABLE IF NOT EXISTS remote_access_sessions(
 id text PRIMARY KEY,
 asset_id text NOT NULL,
 asset_name text NOT NULL,
 host text NOT NULL,
 port integer NOT NULL DEFAULT 22,
 credential_id text NOT NULL DEFAULT '',
 operator_name text NOT NULL,
 roles jsonb NOT NULL DEFAULT '[]',
 status text NOT NULL,
 created_at text NOT NULL,
 expires_at text NOT NULL,
 closed_at text NOT NULL DEFAULT '',
 logs jsonb NOT NULL DEFAULT '[]',
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_remote_access_sessions_time ON remote_access_sessions(updated_at DESC);
CREATE TABLE IF NOT EXISTS remote_terminal_events(
 session_id text NOT NULL,
 sequence bigint NOT NULL,
 direction text NOT NULL,
 data text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(session_id,sequence)
);
CREATE INDEX IF NOT EXISTS idx_remote_terminal_events_time ON remote_terminal_events(session_id,created_at);
`

func openPostgres(databaseURL string) (*sql.DB, []Task, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, nil, err
	}
	if _, err = db.ExecContext(ctx, discoverySchema); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("migrate discovery schema: %w", err)
	}
	if _, err = db.ExecContext(ctx, remoteAccessSchema); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("migrate remote access schema: %w", err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE discovery_items SET state='pending' WHERE state='approving'`); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("recover interrupted approvals: %w", err)
	}
	tasks, err := loadTasks(ctx, db)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return db, tasks, nil
}

func loadTasks(ctx context.Context, db *sql.DB) ([]Task, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,name,source,scope,status,created_at,updated_at FROM discovery_tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []Task
	for rows.Next() {
		var task Task
		var created, updated time.Time
		if err = rows.Scan(&task.ID, &task.Name, &task.Source, &task.Scope, &task.Status, &created, &updated); err != nil {
			return nil, err
		}
		task.CreatedAt = created.Local().Format("2006-01-02 15:04")
		task.UpdatedAt = updated.Local().Format("2006-01-02 15:04:05")
		itemRows, queryErr := db.QueryContext(ctx, `SELECT id,name,ip,type,confidence,state,result,message,raw_payload,updated_at FROM discovery_items WHERE task_id=$1 ORDER BY discovered_at`, task.ID)
		if queryErr != nil {
			return nil, queryErr
		}
		for itemRows.Next() {
			var item DiscoveredItem
			var raw []byte
			var updated time.Time
			if queryErr = itemRows.Scan(&item.ID, &item.Name, &item.IP, &item.Type, &item.Confidence, &item.State, &item.Result, &item.Message, &raw, &updated); queryErr != nil {
				itemRows.Close()
				return nil, queryErr
			}
			item.UpdatedAt = updated.Local().Format("2006-01-02 15:04:05")
			decodeStoredItem(raw, &item)
			task.Items = append(task.Items, item)
			task.Discovered++
			if item.State == "imported" {
				task.Imported++
			}
		}
		itemRows.Close()
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Service) persistTask(task Task) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO discovery_tasks(id,name,source,scope,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,now(),now()) ON CONFLICT(id) DO UPDATE SET name=excluded.name,status=excluded.status,updated_at=now()`, task.ID, task.Name, task.Source, task.Scope, task.Status)
	return err
}

func (s *Service) persistItem(taskID string, item DiscoveredItem, raw []byte) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO discovery_items(id,task_id,name,ip,type,confidence,state,result,message,raw_payload,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now()) ON CONFLICT(id) DO UPDATE SET task_id=excluded.task_id,name=excluded.name,ip=excluded.ip,type=excluded.type,confidence=excluded.confidence,result=excluded.result,message=excluded.message,raw_payload=excluded.raw_payload,updated_at=now()`, item.ID, taskID, item.Name, item.IP, item.Type, item.Confidence, item.State, item.Result, item.Message, raw)
	return err
}

func decodeStoredItem(raw []byte, item *DiscoveredItem) {
	if len(raw) == 0 {
		return
	}
	var stored DiscoveredItem
	if json.Unmarshal(raw, &stored) == nil {
		item.Attributes = stored.Attributes
	}
}

func (s *Service) persistedPendingItems() ([]PendingItem, error) {
	rows, err := s.db.Query(`SELECT i.task_id,i.id,i.name,i.ip,i.type,t.source,i.state,i.confidence,i.discovered_at
FROM discovery_items i JOIN discovery_tasks t ON t.id=i.task_id
WHERE i.state='pending' ORDER BY i.discovered_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PendingItem{}
	for rows.Next() {
		var item PendingItem
		var discovered time.Time
		if err := rows.Scan(&item.TaskID, &item.ID, &item.Name, &item.IP, &item.Type, &item.Source, &item.State, &item.Confidence, &discovered); err != nil {
			return nil, err
		}
		item.DiscoveredAt = discovered.Local().Format("2006-01-02 15:04:05")
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) persistedPendingItem(itemID string) (DiscoveredItem, string, string, error) {
	var item DiscoveredItem
	var source, taskID string
	var raw []byte
	var updated time.Time
	err := s.db.QueryRow(`SELECT i.id,i.name,i.ip,i.type,i.confidence,i.state,i.result,i.message,i.raw_payload,i.updated_at,i.task_id,t.source
FROM discovery_items i JOIN discovery_tasks t ON t.id=i.task_id
WHERE i.id=$1 AND i.state='pending'`, itemID).Scan(&item.ID, &item.Name, &item.IP, &item.Type, &item.Confidence, &item.State, &item.Result, &item.Message, &raw, &updated, &taskID, &source)
	if err == sql.ErrNoRows {
		return DiscoveredItem{}, "", "", ErrNotFound
	}
	if err != nil {
		return DiscoveredItem{}, "", "", err
	}
	item.UpdatedAt = updated.Local().Format("2006-01-02 15:04:05")
	decodeStoredItem(raw, &item)
	item.State = "pending"
	return item, source, taskID, nil
}

func (s *Service) claimPersistedItem(itemID string) error {
	result, err := s.db.Exec(`UPDATE discovery_items SET state='approving' WHERE id=$1 AND state='pending'`, itemID)
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

func (s *Service) setPersistedItemState(itemID, from, to string) error {
	result, err := s.db.Exec(`UPDATE discovery_items SET state=$3 WHERE id=$1 AND state=$2`, itemID, from, to)
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

func (s *Service) updatePersistedItemOutcome(itemID, state, result, message string) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`UPDATE discovery_items SET state=$2,result=$3,message=$4,updated_at=now() WHERE id=$1`, itemID, state, result, message)
	return err
}

func (s *Service) persistedTaskDetail(id string) (Task, []TaskEvent, error) {
	var task Task
	var created, updated time.Time
	err := s.db.QueryRow(`SELECT id,name,source,scope,status,created_at,updated_at FROM discovery_tasks WHERE id=$1`, id).Scan(&task.ID, &task.Name, &task.Source, &task.Scope, &task.Status, &created, &updated)
	if err == sql.ErrNoRows {
		return Task{}, nil, ErrNotFound
	}
	if err != nil {
		return Task{}, nil, err
	}
	task.CreatedAt = created.Local().Format("2006-01-02 15:04")
	task.UpdatedAt = updated.Local().Format("2006-01-02 15:04:05")
	rows, err := s.db.Query(`SELECT id,name,ip,type,confidence,state,result,message,raw_payload,updated_at FROM discovery_items WHERE task_id=$1 ORDER BY discovered_at`, id)
	if err != nil {
		return Task{}, nil, err
	}
	for rows.Next() {
		var item DiscoveredItem
		var raw []byte
		var itemUpdated time.Time
		if err := rows.Scan(&item.ID, &item.Name, &item.IP, &item.Type, &item.Confidence, &item.State, &item.Result, &item.Message, &raw, &itemUpdated); err != nil {
			rows.Close()
			return Task{}, nil, err
		}
		item.UpdatedAt = itemUpdated.Local().Format("2006-01-02 15:04:05")
		decodeStoredItem(raw, &item)
		task.Items = append(task.Items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Task{}, nil, err
	}
	rows.Close()
	task.Discovered = len(task.Items)
	for _, item := range task.Items {
		if item.State == "imported" {
			task.Imported++
		}
	}
	eventRows, err := s.db.Query(`SELECT id,task_id,level,event,message,created_at FROM discovery_task_events WHERE task_id=$1 ORDER BY created_at DESC LIMIT 500`, id)
	if err != nil {
		return Task{}, nil, err
	}
	defer eventRows.Close()
	events := []TaskEvent{}
	for eventRows.Next() {
		var event TaskEvent
		var eventAt time.Time
		if err := eventRows.Scan(&event.ID, &event.TaskID, &event.Level, &event.Event, &event.Message, &eventAt); err != nil {
			return Task{}, nil, err
		}
		event.CreatedAt = eventAt.Local().Format("2006-01-02 15:04:05")
		events = append(events, event)
	}
	return task, events, eventRows.Err()
}
