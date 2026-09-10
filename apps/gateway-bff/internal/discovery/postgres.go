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
 status text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS discovery_items (
 id text PRIMARY KEY, task_id text NOT NULL REFERENCES discovery_tasks(id) ON DELETE CASCADE,
 name text NOT NULL, ip text NOT NULL DEFAULT '', type text NOT NULL,
 confidence integer NOT NULL DEFAULT 0, state text NOT NULL DEFAULT 'pending',
 raw_payload jsonb NOT NULL DEFAULT '{}', discovered_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_discovery_items_task_state ON discovery_items(task_id,state);
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
	rows, err := db.QueryContext(ctx, `SELECT id,name,source,scope,status,created_at FROM discovery_tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []Task
	for rows.Next() {
		var task Task
		var created time.Time
		if err = rows.Scan(&task.ID, &task.Name, &task.Source, &task.Scope, &task.Status, &created); err != nil {
			return nil, err
		}
		task.CreatedAt = created.Local().Format("2006-01-02 15:04")
		itemRows, queryErr := db.QueryContext(ctx, `SELECT id,name,ip,type,confidence,state,raw_payload FROM discovery_items WHERE task_id=$1 ORDER BY discovered_at`, task.ID)
		if queryErr != nil {
			return nil, queryErr
		}
		for itemRows.Next() {
			var item DiscoveredItem
			var raw []byte
			if queryErr = itemRows.Scan(&item.ID, &item.Name, &item.IP, &item.Type, &item.Confidence, &item.State, &raw); queryErr != nil {
				itemRows.Close()
				return nil, queryErr
			}
			if len(raw) > 0 {
				var stored DiscoveredItem
				if json.Unmarshal(raw, &stored) == nil {
					item.Attributes = stored.Attributes
				}
			}
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
	_, err := s.db.Exec(`INSERT INTO discovery_tasks(id,name,source,scope,status,created_at) VALUES($1,$2,$3,$4,$5,now()) ON CONFLICT(id) DO UPDATE SET name=excluded.name,status=excluded.status`, task.ID, task.Name, task.Source, task.Scope, task.Status)
	return err
}

func (s *Service) persistItem(taskID string, item DiscoveredItem, raw []byte) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO discovery_items(id,task_id,name,ip,type,confidence,state,raw_payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET task_id=excluded.task_id,name=excluded.name,ip=excluded.ip,type=excluded.type,confidence=excluded.confidence,raw_payload=excluded.raw_payload`, item.ID, taskID, item.Name, item.IP, item.Type, item.Confidence, item.State, raw)
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
	err := s.db.QueryRow(`SELECT i.id,i.name,i.ip,i.type,i.confidence,i.state,i.raw_payload,i.task_id,t.source
FROM discovery_items i JOIN discovery_tasks t ON t.id=i.task_id
WHERE i.id=$1 AND i.state='pending'`, itemID).Scan(&item.ID, &item.Name, &item.IP, &item.Type, &item.Confidence, &item.State, &raw, &taskID, &source)
	if err == sql.ErrNoRows {
		return DiscoveredItem{}, "", "", ErrNotFound
	}
	if err != nil {
		return DiscoveredItem{}, "", "", err
	}
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
