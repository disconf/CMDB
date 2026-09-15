package networkautomation

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const schema = `
CREATE TABLE IF NOT EXISTS cmdb_network_automation_policies (
  id text PRIMARY KEY,
  name text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  priority integer NOT NULL DEFAULT 10,
  severity text NOT NULL DEFAULT 'critical',
  anomaly_type text NOT NULL DEFAULT '*',
  environment text NOT NULL DEFAULT '',
  project_group text NOT NULL DEFAULT '',
  location text NOT NULL DEFAULT '',
  auto_ticket boolean NOT NULL DEFAULT true,
  assignee text NOT NULL DEFAULT '',
  escalation_minutes integer NOT NULL DEFAULT 0,
  escalate_to text NOT NULL DEFAULT '',
  verification_template text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS cmdb_network_anomaly_lifecycle (
  anomaly_id text PRIMARY KEY,
  policy_id text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'open',
  title text NOT NULL DEFAULT '',
  severity text NOT NULL DEFAULT '',
  target text NOT NULL DEFAULT '',
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz,
  ticket_id text NOT NULL DEFAULT '',
  escalated_at timestamptz,
  verification_job_id text NOT NULL DEFAULT ''
);
`

func openPostgres(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate network automation schema: %w", err)
	}
	if _, err = db.ExecContext(ctx, complianceSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate network compliance schema: %w", err)
	}
	return db, nil
}

func loadPolicies(ctx context.Context, db *sql.DB) ([]Policy, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,name,enabled,priority,severity,anomaly_type,environment,project_group,location,auto_ticket,assignee,escalation_minutes,escalate_to,verification_template,created_at,updated_at FROM cmdb_network_automation_policies ORDER BY priority DESC,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Policy{}
	for rows.Next() {
		var item Policy
		var created, updated time.Time
		if err = rows.Scan(&item.ID, &item.Name, &item.Enabled, &item.Priority, &item.Severity, &item.AnomalyType, &item.Environment, &item.ProjectGroup, &item.Location, &item.AutoTicket, &item.Assignee, &item.EscalationMinutes, &item.EscalateTo, &item.VerificationTemplate, &created, &updated); err != nil {
			return nil, err
		}
		item.CreatedAt = created.Local().Format("2006-01-02 15:04:05")
		item.UpdatedAt = updated.Local().Format("2006-01-02 15:04:05")
		result = append(result, item)
	}
	return result, rows.Err()
}

func upsertPolicy(ctx context.Context, db *sql.DB, item Policy) error {
	_, err := db.ExecContext(ctx, `INSERT INTO cmdb_network_automation_policies(id,name,enabled,priority,severity,anomaly_type,environment,project_group,location,auto_ticket,assignee,escalation_minutes,escalate_to,verification_template,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,now(),now())
		ON CONFLICT(id) DO UPDATE SET name=excluded.name,enabled=excluded.enabled,priority=excluded.priority,severity=excluded.severity,anomaly_type=excluded.anomaly_type,environment=excluded.environment,project_group=excluded.project_group,location=excluded.location,auto_ticket=excluded.auto_ticket,assignee=excluded.assignee,escalation_minutes=excluded.escalation_minutes,escalate_to=excluded.escalate_to,verification_template=excluded.verification_template,updated_at=now()`, item.ID, item.Name, item.Enabled, item.Priority, item.Severity, item.AnomalyType, item.Environment, item.ProjectGroup, item.Location, item.AutoTicket, item.Assignee, item.EscalationMinutes, item.EscalateTo, item.VerificationTemplate)
	return err
}

func loadLifecycle(ctx context.Context, db *sql.DB) (map[string]Lifecycle, error) {
	rows, err := db.QueryContext(ctx, `SELECT anomaly_id,policy_id,status,title,severity,target,first_seen_at,last_seen_at,resolved_at,ticket_id,escalated_at,verification_job_id FROM cmdb_network_anomaly_lifecycle`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]Lifecycle{}
	for rows.Next() {
		var item Lifecycle
		var first, last time.Time
		var resolved, escalated sql.NullTime
		if err = rows.Scan(&item.AnomalyID, &item.PolicyID, &item.Status, &item.Title, &item.Severity, &item.Target, &first, &last, &resolved, &item.TicketID, &escalated, &item.VerificationJobID); err != nil {
			return nil, err
		}
		item.FirstSeenAt = first.Local().Format("2006-01-02 15:04:05")
		item.LastSeenAt = last.Local().Format("2006-01-02 15:04:05")
		if resolved.Valid {
			item.ResolvedAt = resolved.Time.Local().Format("2006-01-02 15:04:05")
		}
		if escalated.Valid {
			item.EscalatedAt = escalated.Time.Local().Format("2006-01-02 15:04:05")
		}
		result[item.AnomalyID] = item
	}
	return result, rows.Err()
}

func upsertLifecycle(ctx context.Context, db *sql.DB, item Lifecycle) error {
	var resolved, escalated any
	if item.ResolvedAt != "" {
		resolved = item.ResolvedAt
	}
	if item.EscalatedAt != "" {
		escalated = item.EscalatedAt
	}
	_, err := db.ExecContext(ctx, `INSERT INTO cmdb_network_anomaly_lifecycle(anomaly_id,policy_id,status,title,severity,target,first_seen_at,last_seen_at,resolved_at,ticket_id,escalated_at,verification_job_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT(anomaly_id) DO UPDATE SET policy_id=excluded.policy_id,status=excluded.status,title=excluded.title,severity=excluded.severity,target=excluded.target,last_seen_at=excluded.last_seen_at,resolved_at=excluded.resolved_at,ticket_id=excluded.ticket_id,escalated_at=excluded.escalated_at,verification_job_id=excluded.verification_job_id`, item.AnomalyID, item.PolicyID, item.Status, item.Title, item.Severity, item.Target, item.FirstSeenAt, item.LastSeenAt, resolved, item.TicketID, escalated, item.VerificationJobID)
	return err
}
