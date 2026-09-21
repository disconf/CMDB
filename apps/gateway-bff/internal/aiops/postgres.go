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
  data_sources jsonb NOT NULL DEFAULT '[]'
);
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
  ticket_id text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_aiops_execution_requests_time ON aiops_execution_requests(requested_at DESC);
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
INSERT INTO aiops_analyses(id,alert_id,alert_title,title,status,severity,target,root_cause,confidence,summary,created_at,updated_at,evidence,recommendations,related_assets,impact,timeline,tickets,releases,data_sources)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
ON CONFLICT(id) DO UPDATE SET alert_title=excluded.alert_title,title=excluded.title,status=excluded.status,severity=excluded.severity,target=excluded.target,root_cause=excluded.root_cause,confidence=excluded.confidence,summary=excluded.summary,updated_at=excluded.updated_at,evidence=excluded.evidence,recommendations=excluded.recommendations,related_assets=excluded.related_assets,impact=excluded.impact,timeline=excluded.timeline,tickets=excluded.tickets,releases=excluded.releases,data_sources=excluded.data_sources`,
		item.ID, item.AlertID, item.AlertTitle, item.Title, item.Status, item.Severity, item.Target, item.RootCause, item.Confidence, item.Summary, created, updated, evidence, recommendations, assets, impact, timeline, tickets, releases, sources)
	return err
}
func (r *postgresRepository) LoadAnalyses(ctx context.Context) ([]Analysis, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id,alert_id,alert_title,title,status,severity,target,root_cause,confidence,summary,created_at,updated_at,evidence,recommendations,related_assets,impact,timeline,tickets,releases,data_sources
FROM aiops_analyses ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Analysis{}
	for rows.Next() {
		var item Analysis
		var created, updated time.Time
		var evidence, recommendations, assets, impact, timeline, tickets, releases, sources []byte
		if err = rows.Scan(&item.ID, &item.AlertID, &item.AlertTitle, &item.Title, &item.Status, &item.Severity, &item.Target, &item.RootCause, &item.Confidence, &item.Summary, &created, &updated, &evidence, &recommendations, &assets, &impact, &timeline, &tickets, &releases, &sources); err != nil {
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
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *postgresRepository) SaveExecution(ctx context.Context, item Execution) error {
	requestedAt, err := parseAITime(item.RequestedAt)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO aiops_execution_requests(id,analysis_id,runbook_id,runbook_title,status,requested_by,requested_at,approval_required,ticket_id)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(id) DO UPDATE SET status=excluded.status,ticket_id=excluded.ticket_id`,
		item.ID, item.AnalysisID, item.RunbookID, item.RunbookTitle, item.Status, item.RequestedBy, requestedAt, item.ApprovalRequired, item.TicketID)
	return err
}

func (r *postgresRepository) LoadExecutions(ctx context.Context) ([]Execution, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id,analysis_id,runbook_id,runbook_title,status,requested_by,requested_at,approval_required,ticket_id
FROM aiops_execution_requests ORDER BY requested_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Execution{}
	for rows.Next() {
		var item Execution
		var requestedAt time.Time
		if err = rows.Scan(&item.ID, &item.AnalysisID, &item.RunbookID, &item.RunbookTitle, &item.Status, &item.RequestedBy, &requestedAt, &item.ApprovalRequired, &item.TicketID); err != nil {
			return nil, err
		}
		item.RequestedAt = requestedAt.Local().Format("2006-01-02 15:04:05")
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
