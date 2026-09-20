package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	_ "github.com/jackc/pgx/v5/stdlib"
	"time"
)

const jobsSchema = `CREATE TABLE IF NOT EXISTS job_executions(id text PRIMARY KEY,template_id text NOT NULL,name text NOT NULL,targets jsonb NOT NULL,status text NOT NULL,progress integer NOT NULL,operator_name text NOT NULL,started_at text NOT NULL,duration text NOT NULL DEFAULT '',logs jsonb NOT NULL DEFAULT '[]',timeout_seconds integer NOT NULL DEFAULT 300,attempt integer NOT NULL DEFAULT 0,approved_by text NOT NULL DEFAULT '',playbook_id text NOT NULL DEFAULT '',credential_id text NOT NULL DEFAULT '',variables jsonb NOT NULL DEFAULT '{}',template_version integer NOT NULL DEFAULT 0,playbook_version integer NOT NULL DEFAULT 0,parameters jsonb NOT NULL DEFAULT '{}',target_results jsonb NOT NULL DEFAULT '[]',artifact_keys jsonb NOT NULL DEFAULT '[]',updated_at timestamptz NOT NULL DEFAULT now());ALTER TABLE job_executions ADD COLUMN IF NOT EXISTS template_version integer NOT NULL DEFAULT 0;ALTER TABLE job_executions ADD COLUMN IF NOT EXISTS playbook_version integer NOT NULL DEFAULT 0;ALTER TABLE job_executions ADD COLUMN IF NOT EXISTS parameters jsonb NOT NULL DEFAULT '{}';ALTER TABLE job_executions ADD COLUMN IF NOT EXISTS target_results jsonb NOT NULL DEFAULT '[]';ALTER TABLE job_executions ADD COLUMN IF NOT EXISTS artifact_keys jsonb NOT NULL DEFAULT '[]';ALTER TABLE job_executions ADD COLUMN IF NOT EXISTS approved_by text NOT NULL DEFAULT '';ALTER TABLE job_executions ADD COLUMN IF NOT EXISTS playbook_id text NOT NULL DEFAULT '';ALTER TABLE job_executions ADD COLUMN IF NOT EXISTS variables jsonb NOT NULL DEFAULT '{}';ALTER TABLE job_executions ADD COLUMN IF NOT EXISTS credential_id text NOT NULL DEFAULT '';CREATE INDEX IF NOT EXISTS idx_job_executions_updated ON job_executions(updated_at DESC);CREATE TABLE IF NOT EXISTS job_templates(id text PRIMARY KEY,name text NOT NULL,category text NOT NULL,description text NOT NULL,command_text text NOT NULL,playbook_id text NOT NULL DEFAULT '',parameters jsonb NOT NULL DEFAULT '{}',version integer NOT NULL DEFAULT 1,updated_by text NOT NULL DEFAULT '',risk text NOT NULL,enabled boolean NOT NULL DEFAULT true,updated_at timestamptz NOT NULL DEFAULT now());ALTER TABLE job_templates ADD COLUMN IF NOT EXISTS playbook_id text NOT NULL DEFAULT '';ALTER TABLE job_templates ADD COLUMN IF NOT EXISTS parameters jsonb NOT NULL DEFAULT '{}';ALTER TABLE job_templates ADD COLUMN IF NOT EXISTS version integer NOT NULL DEFAULT 1;ALTER TABLE job_templates ADD COLUMN IF NOT EXISTS updated_by text NOT NULL DEFAULT '';`

const jobSchedulesSchema = `
CREATE TABLE IF NOT EXISTS job_schedules(
 id text PRIMARY KEY,
 name text NOT NULL,
 template_id text NOT NULL,
 targets jsonb NOT NULL DEFAULT '[]'::jsonb,
 cron text NOT NULL,
 timeout_seconds integer NOT NULL DEFAULT 300,
 enabled boolean NOT NULL DEFAULT true,
 created_by text NOT NULL DEFAULT '',
 next_run timestamptz,
 last_run timestamptz,
 last_status text NOT NULL DEFAULT '',
 last_job_id text NOT NULL DEFAULT '',
 credential_id text NOT NULL DEFAULT '',
 timezone text NOT NULL DEFAULT 'Asia/Shanghai',
 concurrency_policy text NOT NULL DEFAULT 'forbid',
 missed_policy text NOT NULL DEFAULT 'skip',
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_job_schedules_next_run ON job_schedules(enabled,next_run);
ALTER TABLE job_schedules ADD COLUMN IF NOT EXISTS credential_id text NOT NULL DEFAULT '';ALTER TABLE job_schedules ADD COLUMN IF NOT EXISTS playbook_id text NOT NULL DEFAULT '';ALTER TABLE job_schedules ADD COLUMN IF NOT EXISTS playbook_name text NOT NULL DEFAULT '';ALTER TABLE job_schedules ADD COLUMN IF NOT EXISTS variables jsonb NOT NULL DEFAULT '{}';ALTER TABLE job_schedules ADD COLUMN IF NOT EXISTS parameters jsonb NOT NULL DEFAULT '{}';ALTER TABLE job_schedules ADD COLUMN IF NOT EXISTS timezone text NOT NULL DEFAULT 'Asia/Shanghai';ALTER TABLE job_schedules ADD COLUMN IF NOT EXISTS concurrency_policy text NOT NULL DEFAULT 'forbid';ALTER TABLE job_schedules ADD COLUMN IF NOT EXISTS missed_policy text NOT NULL DEFAULT 'skip';
`

const playbooksSchema = `
CREATE TABLE IF NOT EXISTS job_playbooks(
 id text PRIMARY KEY,
 name text NOT NULL,
 description text NOT NULL DEFAULT '',
 content text NOT NULL,
 constants jsonb NOT NULL DEFAULT '{}',
 variables jsonb NOT NULL DEFAULT '{}',
 enabled boolean NOT NULL DEFAULT true,
 version integer NOT NULL DEFAULT 1,
 created_by text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_job_playbooks_updated ON job_playbooks(updated_at DESC);ALTER TABLE job_playbooks ADD COLUMN IF NOT EXISTS version integer NOT NULL DEFAULT 1;
`

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
	if _, err = db.ExecContext(ctx, jobSchedulesSchema); err != nil {
		db.Close()
		return nil, nil, err
	}
	if _, err = db.ExecContext(ctx, playbooksSchema); err != nil {
		db.Close()
		return nil, nil, err
	}
	if _, err = db.ExecContext(ctx, versionHistorySchema); err != nil {
		db.Close()
		return nil, nil, err
	}
	jobs, loadErr := loadJobs(ctx, db)
	if loadErr != nil {
		db.Close()
		return nil, nil, loadErr
	}
	out := make([]Job, 0, len(jobs))
	for _, job := range jobs {
		normalized, recovered := normalizeInterruptedJob(job)
		if recovered {
			if err = upsertJob(ctx, db, normalized); err != nil {
				db.Close()
				return nil, nil, err
			}
		}
		out = append(out, normalized)
	}
	return db, out, nil
}
func upsertJob(ctx context.Context, db *sql.DB, j Job) error {
	targets, _ := json.Marshal(j.Targets)
	logs, _ := json.Marshal(j.Logs)
	variables, _ := json.Marshal(j.Variables)
	parameters, _ := json.Marshal(j.Parameters)
	targetResults, _ := json.Marshal(j.TargetResults)
	artifactKeys, _ := json.Marshal(j.ArtifactKeys)
	_, err := db.ExecContext(ctx, `INSERT INTO job_executions(id,template_id,name,targets,status,progress,operator_name,started_at,duration,logs,timeout_seconds,attempt,approved_by,playbook_id,credential_id,variables,template_version,playbook_version,parameters,target_results,artifact_keys,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,now()) ON CONFLICT(id) DO UPDATE SET status=excluded.status,progress=excluded.progress,duration=excluded.duration,logs=excluded.logs,attempt=excluded.attempt,approved_by=excluded.approved_by,playbook_id=excluded.playbook_id,credential_id=excluded.credential_id,variables=excluded.variables,template_version=excluded.template_version,playbook_version=excluded.playbook_version,parameters=excluded.parameters,target_results=excluded.target_results,artifact_keys=excluded.artifact_keys,updated_at=now()`, j.ID, j.TemplateID, j.Name, targets, j.Status, j.Progress, j.Operator, j.StartedAt, j.Duration, logs, j.TimeoutSeconds, j.Attempt, j.ApprovedBy, j.PlaybookID, j.CredentialID, variables, j.TemplateVersion, j.PlaybookVersion, parameters, targetResults, artifactKeys)
	return err
}
func upsertTemplate(ctx context.Context, db *sql.DB, t Template) error {
	parameters, _ := json.Marshal(t.Parameters)
	_, err := db.ExecContext(ctx, `INSERT INTO job_templates(id,name,category,description,command_text,playbook_id,parameters,version,updated_by,risk,enabled,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now()) ON CONFLICT(id) DO UPDATE SET name=excluded.name,category=excluded.category,description=excluded.description,command_text=excluded.command_text,playbook_id=excluded.playbook_id,parameters=excluded.parameters,version=excluded.version,updated_by=excluded.updated_by,risk=excluded.risk,enabled=excluded.enabled,updated_at=now()`, t.ID, t.Name, t.Category, t.Description, t.Command, t.PlaybookID, parameters, t.Version, t.UpdatedBy, t.Risk, t.Enabled)
	return err
}
func loadTemplates(ctx context.Context, db *sql.DB) ([]Template, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,name,category,description,command_text,playbook_id,parameters,version,updated_by,risk,enabled,to_char(updated_at,'YYYY-MM-DD HH24:MI:SS') FROM job_templates ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Template
	for rows.Next() {
		var t Template
		var parameters []byte
		if err = rows.Scan(&t.ID, &t.Name, &t.Category, &t.Description, &t.Command, &t.PlaybookID, &parameters, &t.Version, &t.UpdatedBy, &t.Risk, &t.Enabled, &t.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(parameters, &t.Parameters)
		t.LastRun = "--"
		out = append(out, t)
	}
	return out, rows.Err()
}

func loadSchedules(ctx context.Context, db *sql.DB) ([]Schedule, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,name,template_id,playbook_id,playbook_name,variables,parameters,targets,cron,timezone,concurrency_policy,missed_policy,timeout_seconds,enabled,created_by,next_run,last_run,last_status,last_job_id,credential_id FROM job_schedules ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Schedule{}
	for rows.Next() {
		var item Schedule
		var targets, variables, parameters []byte
		var nextRun, lastRun sql.NullTime
		if err := rows.Scan(&item.ID, &item.Name, &item.TemplateID, &item.PlaybookID, &item.PlaybookName, &variables, &parameters, &targets, &item.Cron, &item.Timezone, &item.ConcurrencyPolicy, &item.MissedPolicy, &item.TimeoutSeconds, &item.Enabled, &item.CreatedBy, &nextRun, &lastRun, &item.LastStatus, &item.LastJobID, &item.CredentialID); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(targets, &item.Targets)
		_ = json.Unmarshal(variables, &item.Variables)
		_ = json.Unmarshal(parameters, &item.Parameters)
		if nextRun.Valid {
			item.NextRun = nextRun.Time.In(scheduleLocation()).Format(scheduleTimeLayout)
		}
		if lastRun.Valid {
			item.LastRun = lastRun.Time.In(scheduleLocation()).Format(scheduleTimeLayout)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func upsertSchedule(ctx context.Context, db *sql.DB, schedule Schedule) error {
	targets, _ := json.Marshal(schedule.Targets)
	variables, _ := json.Marshal(schedule.Variables)
	parameters, _ := json.Marshal(schedule.Parameters)
	var nextRun, lastRun any
	if value, err := parseScheduleTime(schedule.NextRun); err == nil && !value.IsZero() {
		nextRun = value
	}
	if value, err := parseScheduleTime(schedule.LastRun); err == nil && !value.IsZero() {
		lastRun = value
	}
	_, err := db.ExecContext(ctx, `INSERT INTO job_schedules(id,name,template_id,playbook_id,playbook_name,variables,parameters,targets,cron,timezone,concurrency_policy,missed_policy,timeout_seconds,enabled,created_by,next_run,last_run,last_status,last_job_id,credential_id,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,now()) ON CONFLICT(id) DO UPDATE SET name=excluded.name,template_id=excluded.template_id,playbook_id=excluded.playbook_id,playbook_name=excluded.playbook_name,variables=excluded.variables,parameters=excluded.parameters,targets=excluded.targets,cron=excluded.cron,timezone=excluded.timezone,concurrency_policy=excluded.concurrency_policy,missed_policy=excluded.missed_policy,timeout_seconds=excluded.timeout_seconds,enabled=excluded.enabled,next_run=excluded.next_run,last_run=excluded.last_run,last_status=excluded.last_status,last_job_id=excluded.last_job_id,credential_id=excluded.credential_id,updated_at=now()`, schedule.ID, schedule.Name, schedule.TemplateID, schedule.PlaybookID, schedule.PlaybookName, variables, parameters, targets, schedule.Cron, schedule.Timezone, schedule.ConcurrencyPolicy, schedule.MissedPolicy, schedule.TimeoutSeconds, schedule.Enabled, schedule.CreatedBy, nextRun, lastRun, schedule.LastStatus, schedule.LastJobID, schedule.CredentialID)
	return err
}

func deleteScheduleRow(ctx context.Context, db *sql.DB, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM job_schedules WHERE id=$1`, id)
	return err
}

func loadPlaybooks(ctx context.Context, db *sql.DB) ([]Playbook, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,name,description,content,constants,variables,enabled,version,created_by,to_char(updated_at,'YYYY-MM-DD HH24:MI:SS') FROM job_playbooks ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Playbook{}
	for rows.Next() {
		var item Playbook
		var constants, variables []byte
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Content, &constants, &variables, &item.Enabled, &item.Version, &item.CreatedBy, &item.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(constants, &item.Constants)
		_ = json.Unmarshal(variables, &item.Variables)
		out = append(out, clonePlaybook(item))
	}
	return out, rows.Err()
}

func upsertPlaybook(ctx context.Context, db *sql.DB, item Playbook) error {
	constants, _ := json.Marshal(item.Constants)
	variables, _ := json.Marshal(item.Variables)
	_, err := db.ExecContext(ctx, `INSERT INTO job_playbooks(id,name,description,content,constants,variables,enabled,created_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,now()) ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,content=excluded.content,constants=excluded.constants,variables=excluded.variables,enabled=excluded.enabled,updated_at=now()`, item.ID, item.Name, item.Description, item.Content, constants, variables, item.Enabled, item.CreatedBy)
	return err
}

func deleteTemplateRow(ctx context.Context, db *sql.DB, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM job_templates WHERE id=$1`, id)
	return err
}
func deletePlaybookRow(ctx context.Context, db *sql.DB, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM job_playbooks WHERE id=$1`, id)
	return err
}

func loadJobs(ctx context.Context, db *sql.DB) ([]Job, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,template_id,template_version,name,targets,status,progress,operator_name,started_at,duration,logs,timeout_seconds,attempt,approved_by,playbook_id,playbook_version,credential_id,variables,parameters,target_results,artifact_keys FROM job_executions ORDER BY updated_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func loadJobByID(ctx context.Context, db *sql.DB, id string) (Job, error) {
	row := db.QueryRowContext(ctx, `SELECT id,template_id,template_version,name,targets,status,progress,operator_name,started_at,duration,logs,timeout_seconds,attempt,approved_by,playbook_id,playbook_version,credential_id,variables,parameters,target_results,artifact_keys FROM job_executions WHERE id=$1`, id)
	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return Job{}, ErrNotFound
	}
	return job, err
}

type jobScanner interface {
	Scan(dest ...any) error
}

func scanJob(row jobScanner) (Job, error) {
	var job Job
	var targets, logs, variables, parameters, targetResults, artifactKeys []byte
	if err := row.Scan(&job.ID, &job.TemplateID, &job.TemplateVersion, &job.Name, &targets, &job.Status, &job.Progress, &job.Operator, &job.StartedAt, &job.Duration, &logs, &job.TimeoutSeconds, &job.Attempt, &job.ApprovedBy, &job.PlaybookID, &job.PlaybookVersion, &job.CredentialID, &variables, &parameters, &targetResults, &artifactKeys); err != nil {
		return Job{}, err
	}
	_ = json.Unmarshal(targets, &job.Targets)
	_ = json.Unmarshal(logs, &job.Logs)
	_ = json.Unmarshal(variables, &job.Variables)
	_ = json.Unmarshal(parameters, &job.Parameters)
	_ = json.Unmarshal(targetResults, &job.TargetResults)
	_ = json.Unmarshal(artifactKeys, &job.ArtifactKeys)
	return job, nil
}
func normalizeInterruptedJob(job Job) (Job, bool) {
	if job.Status != "running" {
		return job, false
	}
	job.Status = "failed"
	job.Logs = append(job.Logs, Log{time.Now().Format("15:04:05"), "error", "网关重启导致执行中断，可手动重试"})
	return job, true
}
