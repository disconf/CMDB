package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	_ "github.com/jackc/pgx/v5/stdlib"
	"time"
)

const jobsSchema = `CREATE TABLE IF NOT EXISTS job_executions(id text PRIMARY KEY,template_id text NOT NULL,name text NOT NULL,targets jsonb NOT NULL,status text NOT NULL,progress integer NOT NULL,operator_name text NOT NULL,started_at text NOT NULL,duration text NOT NULL DEFAULT '',logs jsonb NOT NULL DEFAULT '[]',timeout_seconds integer NOT NULL DEFAULT 300,attempt integer NOT NULL DEFAULT 0,approved_by text NOT NULL DEFAULT '',updated_at timestamptz NOT NULL DEFAULT now());ALTER TABLE job_executions ADD COLUMN IF NOT EXISTS approved_by text NOT NULL DEFAULT '';CREATE INDEX IF NOT EXISTS idx_job_executions_updated ON job_executions(updated_at DESC);CREATE TABLE IF NOT EXISTS job_templates(id text PRIMARY KEY,name text NOT NULL,category text NOT NULL,description text NOT NULL,command_text text NOT NULL,risk text NOT NULL,enabled boolean NOT NULL DEFAULT true,updated_at timestamptz NOT NULL DEFAULT now());`

func openPostgres(url string) (*sql.DB, []Job, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, nil, err
	}
	if _, err = db.ExecContext(ctx, jobsSchema); err != nil {
		db.Close()
		return nil, nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id,template_id,name,targets,status,progress,operator_name,started_at,duration,logs,timeout_seconds,attempt,approved_by FROM job_executions ORDER BY updated_at DESC LIMIT 500`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		var j Job
		var targets, logs []byte
		if err = rows.Scan(&j.ID, &j.TemplateID, &j.Name, &targets, &j.Status, &j.Progress, &j.Operator, &j.StartedAt, &j.Duration, &logs, &j.TimeoutSeconds, &j.Attempt, &j.ApprovedBy); err != nil {
			return nil, nil, err
		}
		_ = json.Unmarshal(targets, &j.Targets)
		_ = json.Unmarshal(logs, &j.Logs)
		if j.Status == "running" {
			j.Status = "failed"
			j.Logs = append(j.Logs, Log{time.Now().Format("15:04:05"), "error", "网关重启导致执行中断，可手动重试"})
		}
		out = append(out, j)
	}
	return db, out, rows.Err()
}
func upsertJob(ctx context.Context, db *sql.DB, j Job) error {
	targets, _ := json.Marshal(j.Targets)
	logs, _ := json.Marshal(j.Logs)
	_, err := db.ExecContext(ctx, `INSERT INTO job_executions(id,template_id,name,targets,status,progress,operator_name,started_at,duration,logs,timeout_seconds,attempt,approved_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now()) ON CONFLICT(id) DO UPDATE SET status=excluded.status,progress=excluded.progress,duration=excluded.duration,logs=excluded.logs,attempt=excluded.attempt,approved_by=excluded.approved_by,updated_at=now()`, j.ID, j.TemplateID, j.Name, targets, j.Status, j.Progress, j.Operator, j.StartedAt, j.Duration, logs, j.TimeoutSeconds, j.Attempt, j.ApprovedBy)
	return err
}
func upsertTemplate(ctx context.Context, db *sql.DB, t Template) error {
	_, err := db.ExecContext(ctx, `INSERT INTO job_templates(id,name,category,description,command_text,risk,enabled,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,now()) ON CONFLICT(id) DO UPDATE SET name=excluded.name,category=excluded.category,description=excluded.description,command_text=excluded.command_text,risk=excluded.risk,enabled=excluded.enabled,updated_at=now()`, t.ID, t.Name, t.Category, t.Description, t.Command, t.Risk, t.Enabled)
	return err
}
func loadTemplates(ctx context.Context, db *sql.DB) ([]Template, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,name,category,description,command_text,risk,enabled FROM job_templates ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Template
	for rows.Next() {
		var t Template
		if err = rows.Scan(&t.ID, &t.Name, &t.Category, &t.Description, &t.Command, &t.Risk, &t.Enabled); err != nil {
			return nil, err
		}
		t.LastRun = "--"
		out = append(out, t)
	}
	return out, rows.Err()
}
