package aiops

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const aiopsSchema = `
CREATE TABLE IF NOT EXISTS aiops_analyses (
  id text PRIMARY KEY,
  alert_id text NOT NULL,
  alert_title text NOT NULL DEFAULT '',
  title text NOT NULL,
  status text NOT NULL,
  severity text NOT NULL DEFAULT '',
  target text NOT NULL DEFAULT '',
  root_cause text NOT NULL DEFAULT '',
  confidence integer NOT NULL DEFAULT 0,
  confidence_adjustment integer NOT NULL DEFAULT 0,
  confidence_reason text NOT NULL DEFAULT '',
  applied_feedback_count integer NOT NULL DEFAULT 0,
  summary text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  evidence jsonb NOT NULL DEFAULT '[]',
  recommendations jsonb NOT NULL DEFAULT '[]',
  related_assets jsonb NOT NULL DEFAULT '[]',
  impact jsonb NOT NULL DEFAULT '{}',
  timeline jsonb NOT NULL DEFAULT '[]',
  tickets jsonb NOT NULL DEFAULT '[]',
  releases jsonb NOT NULL DEFAULT '[]',
  data_sources jsonb NOT NULL DEFAULT '[]',
  feedback jsonb NOT NULL DEFAULT '{}'
);
ALTER TABLE aiops_analyses ADD COLUMN IF NOT EXISTS feedback jsonb NOT NULL DEFAULT '{}';
ALTER TABLE aiops_analyses ADD COLUMN IF NOT EXISTS confidence_adjustment integer NOT NULL DEFAULT 0;
ALTER TABLE aiops_analyses ADD COLUMN IF NOT EXISTS confidence_reason text NOT NULL DEFAULT '';
ALTER TABLE aiops_analyses ADD COLUMN IF NOT EXISTS applied_feedback_count integer NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX IF NOT EXISTS idx_aiops_analyses_alert_id ON aiops_analyses(alert_id);
CREATE INDEX IF NOT EXISTS idx_aiops_analyses_created_at ON aiops_analyses(created_at DESC);
CREATE TABLE IF NOT EXISTS aiops_execution_requests (
  id text PRIMARY KEY,
  analysis_id text NOT NULL,
  runbook_id text NOT NULL,
  runbook_title text NOT NULL DEFAULT '',
  status text NOT NULL,
  requested_by text NOT NULL DEFAULT '',
  requested_at timestamptz NOT NULL,
  approval_required boolean NOT NULL DEFAULT true,
  simulated boolean NOT NULL DEFAULT false,
  rollback boolean NOT NULL DEFAULT false,
  rehearsal boolean NOT NULL DEFAULT false,
  parent_execution_id text NOT NULL DEFAULT '',
  ticket_id text NOT NULL DEFAULT '',
  job_id text NOT NULL DEFAULT '',
  approved_by text NOT NULL DEFAULT '',
  approved_at timestamptz,
  finished_at timestamptz,
  result_summary text NOT NULL DEFAULT '',
  error_message text NOT NULL DEFAULT '',
  change_plan jsonb NOT NULL DEFAULT '{}',
  verification_status text NOT NULL DEFAULT '',
  verification_summary text NOT NULL DEFAULT '',
  verified_at timestamptz
);
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS job_id text NOT NULL DEFAULT '';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS approved_by text NOT NULL DEFAULT '';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS approved_at timestamptz;
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS finished_at timestamptz;
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS result_summary text NOT NULL DEFAULT '';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS error_message text NOT NULL DEFAULT '';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS simulated boolean NOT NULL DEFAULT false;
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS rollback boolean NOT NULL DEFAULT false;
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS rehearsal boolean NOT NULL DEFAULT false;
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS parent_execution_id text NOT NULL DEFAULT '';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS change_plan jsonb NOT NULL DEFAULT '{}';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS verification_status text NOT NULL DEFAULT '';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS verification_summary text NOT NULL DEFAULT '';
ALTER TABLE aiops_execution_requests ADD COLUMN IF NOT EXISTS verified_at timestamptz;
CREATE TABLE IF NOT EXISTS aiops_runbook_policies (
  runbook_id text PRIMARY KEY,
  title text NOT NULL,
  template_id text NOT NULL,
  mode text NOT NULL,
  execution_mode text NOT NULL DEFAULT 'simulate',
  risk text NOT NULL,
  approval_required boolean NOT NULL DEFAULT true,
  enabled boolean NOT NULL DEFAULT false,
  max_targets integer NOT NULL DEFAULT 1,
  allowed_environments jsonb NOT NULL DEFAULT '[]',
  change_window text NOT NULL DEFAULT '*',
  rollback_runbook_id text NOT NULL DEFAULT '',
  live_daily_limit integer NOT NULL DEFAULT 1,
  live_cooldown_minutes integer NOT NULL DEFAULT 30,
  require_rollback boolean NOT NULL DEFAULT true,
  require_canary boolean NOT NULL DEFAULT true,
  updated_by text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE aiops_runbook_policies ADD COLUMN IF NOT EXISTS execution_mode text NOT NULL DEFAULT 'simulate';
ALTER TABLE aiops_runbook_policies ADD COLUMN IF NOT EXISTS rollback_runbook_id text NOT NULL DEFAULT '';
ALTER TABLE aiops_runbook_policies ADD COLUMN IF NOT EXISTS live_daily_limit integer NOT NULL DEFAULT 1;
ALTER TABLE aiops_runbook_policies ADD COLUMN IF NOT EXISTS live_cooldown_minutes integer NOT NULL DEFAULT 30;
ALTER TABLE aiops_runbook_policies ADD COLUMN IF NOT EXISTS require_rollback boolean NOT NULL DEFAULT true;
ALTER TABLE aiops_runbook_policies ADD COLUMN IF NOT EXISTS require_canary boolean NOT NULL DEFAULT true;
CREATE INDEX IF NOT EXISTS idx_aiops_execution_requests_time ON aiops_execution_requests(requested_at DESC);
CREATE TABLE IF NOT EXISTS aiops_change_control (
  id text PRIMARY KEY,
  frozen boolean NOT NULL DEFAULT false,
  live_enabled boolean NOT NULL DEFAULT true,
  reason text NOT NULL DEFAULT '',
  freeze_until timestamptz,
  updated_by text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO aiops_change_control(id,frozen,live_enabled,reason,updated_by) VALUES('default',false,true,'','system') ON CONFLICT(id) DO NOTHING;
CREATE TABLE IF NOT EXISTS aiops_canary_validations (
  id text PRIMARY KEY,
  runbook_id text NOT NULL,
  asset_id text NOT NULL,
  asset_name text NOT NULL DEFAULT '',
  status text NOT NULL,
  job_id text NOT NULL DEFAULT '',
  summary text NOT NULL DEFAULT '',
  checks jsonb NOT NULL DEFAULT '[]',
  host_up boolean NOT NULL DEFAULT false,
  baseline jsonb NOT NULL DEFAULT '[]',
  requested_by text NOT NULL DEFAULT '',
  started_at timestamptz NOT NULL,
  finished_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE aiops_canary_validations ADD COLUMN IF NOT EXISTS host_up boolean NOT NULL DEFAULT false;
ALTER TABLE aiops_canary_validations ADD COLUMN IF NOT EXISTS baseline jsonb NOT NULL DEFAULT '[]';
CREATE INDEX IF NOT EXISTS idx_aiops_canary_validations_asset ON aiops_canary_validations(runbook_id,asset_id,started_at DESC);
`

type postgresRepository struct {
	db *sql.DB
}

func openPostgres(databaseURL string) (*postgresRepository, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err = db.ExecContext(ctx, aiopsSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate aiops schema: %w", err)
	}
	return &postgresRepository{db: db}, nil
}

func (r *postgresRepository) SaveAnalysis(ctx context.Context, item Analysis) error {
	evidence, _ := json.Marshal(item.Evidence)
	recommendations, _ := json.Marshal(item.Recommendations)
	assets, _ := json.Marshal(item.RelatedAssets)
	impact, _ := json.Marshal(item.Impact)
	timeline, _ := json.Marshal(item.Timeline)
	tickets, _ := json.Marshal(item.Tickets)
	releases, _ := json.Marshal(item.Releases)
	sources, _ := json.Marshal(item.DataSources)
	feedback, _ := json.Marshal(item.Feedback)
	created, err := parseAITime(item.CreatedAt)
	if err != nil {
		return err
	}
	updated := created
	if item.UpdatedAt != "" {
		updated, err = parseAITime(item.UpdatedAt)
		if err != nil {
			return err
		}
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO aiops_analyses(id,alert_id,alert_title,title,status,severity,target,root_cause,confidence,confidence_adjustment,confidence_reason,applied_feedback_count,summary,created_at,updated_at,evidence,recommendations,related_assets,impact,timeline,tickets,releases,data_sources,feedback)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
ON CONFLICT(id) DO UPDATE SET alert_title=excluded.alert_title,title=excluded.title,status=excluded.status,severity=excluded.severity,target=excluded.target,root_cause=excluded.root_cause,confidence=excluded.confidence,confidence_adjustment=excluded.confidence_adjustment,confidence_reason=excluded.confidence_reason,applied_feedback_count=excluded.applied_feedback_count,summary=excluded.summary,updated_at=excluded.updated_at,evidence=excluded.evidence,recommendations=excluded.recommendations,related_assets=excluded.related_assets,impact=excluded.impact,timeline=excluded.timeline,tickets=excluded.tickets,releases=excluded.releases,data_sources=excluded.data_sources,feedback=excluded.feedback`,
		item.ID, item.AlertID, item.AlertTitle, item.Title, item.Status, item.Severity, item.Target, item.RootCause, item.Confidence, item.ConfidenceAdjustment, item.ConfidenceReason, item.AppliedFeedbackCount, item.Summary, created, updated, evidence, recommendations, assets, impact, timeline, tickets, releases, sources, feedback)
	return err
}
func (r *postgresRepository) LoadAnalyses(ctx context.Context) ([]Analysis, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id,alert_id,alert_title,title,status,severity,target,root_cause,confidence,confidence_adjustment,confidence_reason,applied_feedback_count,summary,created_at,updated_at,evidence,recommendations,related_assets,impact,timeline,tickets,releases,data_sources,feedback
FROM aiops_analyses ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Analysis{}
	for rows.Next() {
		var item Analysis
		var created, updated time.Time
		var evidence, recommendations, assets, impact, timeline, tickets, releases, sources, feedback []byte
		if err = rows.Scan(&item.ID, &item.AlertID, &item.AlertTitle, &item.Title, &item.Status, &item.Severity, &item.Target, &item.RootCause, &item.Confidence, &item.ConfidenceAdjustment, &item.ConfidenceReason, &item.AppliedFeedbackCount, &item.Summary, &created, &updated, &evidence, &recommendations, &assets, &impact, &timeline, &tickets, &releases, &sources, &feedback); err != nil {
			return nil, err
		}
		item.CreatedAt = created.Local().Format("2006-01-02 15:04:05")
		item.UpdatedAt = updated.Local().Format("2006-01-02 15:04:05")
		_ = json.Unmarshal(evidence, &item.Evidence)
		_ = json.Unmarshal(recommendations, &item.Recommendations)
		_ = json.Unmarshal(assets, &item.RelatedAssets)
		_ = json.Unmarshal(impact, &item.Impact)
		_ = json.Unmarshal(timeline, &item.Timeline)
		_ = json.Unmarshal(tickets, &item.Tickets)
		_ = json.Unmarshal(releases, &item.Releases)
		_ = json.Unmarshal(sources, &item.DataSources)
		if string(feedback) != "" && string(feedback) != "{}" && string(feedback) != "null" {
			var value Feedback
			_ = json.Unmarshal(feedback, &value)
			if value.Status != "" {
				item.Feedback = &value
			}
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *postgresRepository) SaveExecution(ctx context.Context, item Execution) error {
	requestedAt, err := parseAITime(item.RequestedAt)
	if err != nil {
		return err
	}
	var approvedAt any
	if item.ApprovedAt != "" {
		approvedAt, _ = parseAITime(item.ApprovedAt)
	}
	var finishedAt any
	if item.FinishedAt != "" {
		finishedAt, _ = parseAITime(item.FinishedAt)
	}
	var verifiedAt any
	if item.VerifiedAt != "" {
		verifiedAt, _ = parseAITime(item.VerifiedAt)
	}
	changePlan, _ := json.Marshal(item.ChangePlan)
	_, err = r.db.ExecContext(ctx, `
INSERT INTO aiops_execution_requests(id,analysis_id,runbook_id,runbook_title,status,requested_by,requested_at,approval_required,simulated,rollback,rehearsal,parent_execution_id,ticket_id,job_id,approved_by,approved_at,finished_at,result_summary,error_message,change_plan,verification_status,verification_summary,verified_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
ON CONFLICT(id) DO UPDATE SET status=excluded.status,simulated=excluded.simulated,rollback=excluded.rollback,rehearsal=excluded.rehearsal,parent_execution_id=excluded.parent_execution_id,ticket_id=excluded.ticket_id,job_id=excluded.job_id,approved_by=excluded.approved_by,approved_at=excluded.approved_at,finished_at=excluded.finished_at,result_summary=excluded.result_summary,error_message=excluded.error_message,change_plan=excluded.change_plan,verification_status=excluded.verification_status,verification_summary=excluded.verification_summary,verified_at=excluded.verified_at`,
		item.ID, item.AnalysisID, item.RunbookID, item.RunbookTitle, item.Status, item.RequestedBy, requestedAt, item.ApprovalRequired, item.Simulated, item.Rollback, item.Rehearsal, item.ParentExecutionID, item.TicketID, item.JobID, item.ApprovedBy, approvedAt, finishedAt, item.ResultSummary, item.Error, changePlan, item.VerificationStatus, item.VerificationSummary, verifiedAt)
	return err
}

func (r *postgresRepository) LoadExecutions(ctx context.Context) ([]Execution, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id,analysis_id,runbook_id,runbook_title,status,requested_by,requested_at,approval_required,simulated,rollback,rehearsal,parent_execution_id,ticket_id,job_id,approved_by,approved_at,finished_at,result_summary,error_message,change_plan,verification_status,verification_summary,verified_at
FROM aiops_execution_requests ORDER BY requested_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Execution{}
	for rows.Next() {
		var item Execution
		var requestedAt time.Time
		var approvedAt, finishedAt, verifiedAt sql.NullTime
		var changePlan []byte
		if err = rows.Scan(&item.ID, &item.AnalysisID, &item.RunbookID, &item.RunbookTitle, &item.Status, &item.RequestedBy, &requestedAt, &item.ApprovalRequired, &item.Simulated, &item.Rollback, &item.Rehearsal, &item.ParentExecutionID, &item.TicketID, &item.JobID, &item.ApprovedBy, &approvedAt, &finishedAt, &item.ResultSummary, &item.Error, &changePlan, &item.VerificationStatus, &item.VerificationSummary, &verifiedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(changePlan, &item.ChangePlan)
		item.RequestedAt = requestedAt.Local().Format("2006-01-02 15:04:05")
		if approvedAt.Valid {
			item.ApprovedAt = approvedAt.Time.Local().Format("2006-01-02 15:04:05")
		}
		if finishedAt.Valid {
			item.FinishedAt = finishedAt.Time.Local().Format("2006-01-02 15:04:05")
		}
		if verifiedAt.Valid {
			item.VerifiedAt = verifiedAt.Time.Local().Format("2006-01-02 15:04:05")
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func parseAITime(value string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid aiops time %q", value)
}

func (r *postgresRepository) LoadRunbookPolicies(ctx context.Context) ([]RunbookPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT runbook_id,title,template_id,mode,execution_mode,risk,approval_required,enabled,max_targets,allowed_environments,change_window,rollback_runbook_id,live_daily_limit,live_cooldown_minutes,require_rollback,require_canary,updated_by,updated_at FROM aiops_runbook_policies ORDER BY mode,runbook_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RunbookPolicy{}
	for rows.Next() {
		var item RunbookPolicy
		var environments []byte
		var updatedAt time.Time
		if err = rows.Scan(&item.RunbookID, &item.Title, &item.TemplateID, &item.Mode, &item.ExecutionMode, &item.Risk, &item.ApprovalRequired, &item.Enabled, &item.MaxTargets, &environments, &item.ChangeWindow, &item.RollbackRunbookID, &item.LiveDailyLimit, &item.LiveCooldownMinutes, &item.RequireRollback, &item.RequireCanary, &item.UpdatedBy, &updatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(environments, &item.AllowedEnvironments)
		item.UpdatedAt = updatedAt.Local().Format("2006-01-02 15:04:05")
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *postgresRepository) LoadCanaryValidations(ctx context.Context) ([]CanaryValidation, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,runbook_id,asset_id,asset_name,status,job_id,summary,checks,host_up,baseline,requested_by,started_at,finished_at FROM aiops_canary_validations ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CanaryValidation{}
	for rows.Next() {
		var item CanaryValidation
		var checks, baseline []byte
		var startedAt time.Time
		var finishedAt sql.NullTime
		if err = rows.Scan(&item.ID, &item.RunbookID, &item.AssetID, &item.AssetName, &item.Status, &item.JobID, &item.Summary, &checks, &item.HostUp, &baseline, &item.RequestedBy, &startedAt, &finishedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(checks, &item.Checks)
		_ = json.Unmarshal(baseline, &item.Baseline)
		item.StartedAt = startedAt.Local().Format("2006-01-02 15:04:05")
		if finishedAt.Valid {
			item.FinishedAt = finishedAt.Time.Local().Format("2006-01-02 15:04:05")
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *postgresRepository) SaveCanaryValidation(ctx context.Context, item CanaryValidation) error {
	startedAt, err := parseAITime(item.StartedAt)
	if err != nil {
		return err
	}
	var finishedAt any
	if item.FinishedAt != "" {
		finishedAt, _ = parseAITime(item.FinishedAt)
	}
	checks, _ := json.Marshal(item.Checks)
	baseline, _ := json.Marshal(item.Baseline)
	_, err = r.db.ExecContext(ctx, `INSERT INTO aiops_canary_validations(id,runbook_id,asset_id,asset_name,status,job_id,summary,checks,host_up,baseline,requested_by,started_at,finished_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now()) ON CONFLICT(id) DO UPDATE SET status=excluded.status,job_id=excluded.job_id,summary=excluded.summary,checks=excluded.checks,host_up=excluded.host_up,baseline=excluded.baseline,finished_at=excluded.finished_at,updated_at=now()`, item.ID, item.RunbookID, item.AssetID, item.AssetName, item.Status, item.JobID, item.Summary, checks, item.HostUp, baseline, item.RequestedBy, startedAt, finishedAt)
	return err
}

func (r *postgresRepository) LoadChangeControl(ctx context.Context) (ChangeControl, error) {
	var item ChangeControl
	var freezeUntil sql.NullTime
	var updatedAt time.Time
	err := r.db.QueryRowContext(ctx, `SELECT id,frozen,live_enabled,reason,freeze_until,updated_by,updated_at FROM aiops_change_control WHERE id='default'`).Scan(&item.ID, &item.Frozen, &item.LiveEnabled, &item.Reason, &freezeUntil, &item.UpdatedBy, &updatedAt)
	if err != nil {
		return ChangeControl{}, err
	}
	if freezeUntil.Valid {
		item.FreezeUntil = freezeUntil.Time.Local().Format("2006-01-02 15:04:05")
	}
	item.UpdatedAt = updatedAt.Local().Format("2006-01-02 15:04:05")
	return item, nil
}

func (r *postgresRepository) SaveChangeControl(ctx context.Context, item ChangeControl) error {
	var freezeUntil any
	if item.FreezeUntil != "" {
		parsed, err := parseAITime(item.FreezeUntil)
		if err != nil {
			return err
		}
		freezeUntil = parsed
	}
	updatedAt := time.Now()
	if item.UpdatedAt != "" {
		if parsed, err := parseAITime(item.UpdatedAt); err == nil {
			updatedAt = parsed
		}
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO aiops_change_control(id,frozen,live_enabled,reason,freeze_until,updated_by,updated_at) VALUES('default',$1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO UPDATE SET frozen=excluded.frozen,live_enabled=excluded.live_enabled,reason=excluded.reason,freeze_until=excluded.freeze_until,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, item.Frozen, item.LiveEnabled, item.Reason, freezeUntil, item.UpdatedBy, updatedAt)
	return err
}

func (r *postgresRepository) SaveRunbookPolicy(ctx context.Context, item RunbookPolicy) error {
	environments, _ := json.Marshal(item.AllowedEnvironments)
	updatedAt, err := parseAITime(item.UpdatedAt)
	if err != nil {
		updatedAt = time.Now()
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO aiops_runbook_policies(runbook_id,title,template_id,mode,execution_mode,risk,approval_required,enabled,max_targets,allowed_environments,change_window,rollback_runbook_id,live_daily_limit,live_cooldown_minutes,require_rollback,require_canary,updated_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) ON CONFLICT(runbook_id) DO UPDATE SET title=excluded.title,template_id=excluded.template_id,mode=excluded.mode,execution_mode=excluded.execution_mode,risk=excluded.risk,approval_required=excluded.approval_required,enabled=excluded.enabled,max_targets=excluded.max_targets,allowed_environments=excluded.allowed_environments,change_window=excluded.change_window,rollback_runbook_id=excluded.rollback_runbook_id,live_daily_limit=excluded.live_daily_limit,live_cooldown_minutes=excluded.live_cooldown_minutes,require_rollback=excluded.require_rollback,require_canary=excluded.require_canary,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, item.RunbookID, item.Title, item.TemplateID, item.Mode, item.ExecutionMode, item.Risk, item.ApprovalRequired, item.Enabled, item.MaxTargets, environments, item.ChangeWindow, item.RollbackRunbookID, item.LiveDailyLimit, item.LiveCooldownMinutes, item.RequireRollback, item.RequireCanary, item.UpdatedBy, updatedAt)
	return err
}
