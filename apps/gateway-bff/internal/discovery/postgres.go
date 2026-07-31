package discovery

import (
	"context"
	"database/sql"
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
		itemRows, queryErr := db.QueryContext(ctx, `SELECT id,name,ip,type,confidence,state FROM discovery_items WHERE task_id=$1 ORDER BY discovered_at`, task.ID)
		if queryErr != nil {
			return nil, queryErr
		}
		for itemRows.Next() {
			var item DiscoveredItem
			if queryErr = itemRows.Scan(&item.ID, &item.Name, &item.IP, &item.Type, &item.Confidence, &item.State); queryErr != nil {
				itemRows.Close()
				return nil, queryErr
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
