package monitor

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const monitorSchema = `
CREATE TABLE IF NOT EXISTS monitor_alerts (
 id text PRIMARY KEY,title text NOT NULL,severity text NOT NULL,status text NOT NULL,
 target_id text NOT NULL DEFAULT '',target text NOT NULL DEFAULT '',rule text NOT NULL DEFAULT '',
 started_at timestamptz NOT NULL,duration text NOT NULL DEFAULT '',owner text NOT NULL DEFAULT '',
 message text NOT NULL DEFAULT '',updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_monitor_alerts_status_severity ON monitor_alerts(status,severity);
CREATE TABLE IF NOT EXISTS monitor_alert_events (
 id bigserial PRIMARY KEY,alert_id text NOT NULL REFERENCES monitor_alerts(id) ON DELETE CASCADE,
 event_type text NOT NULL,operator text NOT NULL DEFAULT '',message text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_monitor_alert_events_alert_time ON monitor_alert_events(alert_id,created_at DESC);
CREATE TABLE IF NOT EXISTS monitor_routing_rules (
 id text PRIMARY KEY,name text NOT NULL,matcher_name text NOT NULL,matcher_value text NOT NULL,
 team text NOT NULL,owner text NOT NULL,channel_id text NOT NULL DEFAULT '',enabled boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS monitor_notification_channels (
 id text PRIMARY KEY,name text NOT NULL,endpoint text NOT NULL,template text NOT NULL DEFAULT '',enabled boolean NOT NULL DEFAULT true,
 last_status text NOT NULL DEFAULT '',last_error text NOT NULL DEFAULT '',last_sent_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE monitor_routing_rules ADD COLUMN IF NOT EXISTS channel_id text NOT NULL DEFAULT '';
ALTER TABLE monitor_notification_channels ADD COLUMN IF NOT EXISTS template text NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS monitor_notification_deliveries (
 id text PRIMARY KEY,channel_id text NOT NULL,channel_name text NOT NULL,alert_id text NOT NULL DEFAULT '',
 alert_title text NOT NULL DEFAULT '',attempt integer NOT NULL,status text NOT NULL,http_status integer NOT NULL DEFAULT 0,
 error text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_monitor_notification_deliveries_time ON monitor_notification_deliveries(created_at DESC);
CREATE TABLE IF NOT EXISTS event_outbox (
 id text PRIMARY KEY,topic text NOT NULL,event_key text NOT NULL,payload jsonb NOT NULL,
 attempts integer NOT NULL DEFAULT 0,next_attempt_at timestamptz NOT NULL DEFAULT now(),
 published_at timestamptz,created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_event_outbox_pending ON event_outbox(next_attempt_at,created_at) WHERE published_at IS NULL;
CREATE TABLE IF NOT EXISTS monitor_notification_dead_letters (
 id text PRIMARY KEY,event_id text NOT NULL,alert_id text NOT NULL DEFAULT '',payload jsonb NOT NULL,
 error text NOT NULL,attempts integer NOT NULL,created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS monitor_escalation_policies (
 id text PRIMARY KEY,name text NOT NULL,severity text NOT NULL,timeout_minutes integer NOT NULL,
 team text NOT NULL,owner text NOT NULL,channel_id text NOT NULL DEFAULT '',enabled boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS monitor_alert_rule_definitions (
 id text PRIMARY KEY,group_name text NOT NULL DEFAULT 'cmdb-custom',name text NOT NULL,query text NOT NULL,
 duration text NOT NULL DEFAULT '5m',severity text NOT NULL DEFAULT 'warning',summary text NOT NULL DEFAULT '',
 description text NOT NULL DEFAULT '',enabled boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_monitor_alert_rule_definitions_group_name ON monitor_alert_rule_definitions(group_name,name);
CREATE TABLE IF NOT EXISTS monitor_alert_escalations (
 alert_id text NOT NULL,policy_id text NOT NULL,escalated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(alert_id,policy_id)
);
`

func openPostgres(databaseURL string) (*sql.DB, []Alert, error) {
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
	if _, err = db.ExecContext(ctx, monitorSchema); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("migrate monitor schema: %w", err)
	}
	rows, err := db.QueryContext(ctx, `SELECT id,title,severity,status,target_id,target,rule,started_at,duration,owner,message FROM monitor_alerts ORDER BY updated_at DESC`)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	defer rows.Close()
	var alerts []Alert
	for rows.Next() {
		var a Alert
		var started time.Time
		if err = rows.Scan(&a.ID, &a.Title, &a.Severity, &a.Status, &a.TargetID, &a.Target, &a.Rule, &started, &a.Duration, &a.Owner, &a.Message); err != nil {
			db.Close()
			return nil, nil, err
		}
		a.StartedAt = started.Local().Format("2006-01-02 15:04")
		alerts = append(alerts, a)
	}
	return db, alerts, rows.Err()
}

type alertExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func upsertAlert(ctx context.Context, db alertExecer, a Alert) error {
	started, err := time.ParseInLocation("2006-01-02 15:04", a.StartedAt, time.Local)
	if err != nil {
		started = time.Now()
	}
	_, err = db.ExecContext(ctx, `INSERT INTO monitor_alerts(id,title,severity,status,target_id,target,rule,started_at,duration,owner,message) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(id) DO UPDATE SET title=excluded.title,severity=excluded.severity,status=excluded.status,target_id=excluded.target_id,target=excluded.target,rule=excluded.rule,duration=excluded.duration,owner=excluded.owner,message=excluded.message,updated_at=now()`, a.ID, a.Title, a.Severity, a.Status, a.TargetID, a.Target, a.Rule, started, a.Duration, a.Owner, a.Message)
	return err
}

func insertAlertEvent(ctx context.Context, db *sql.DB, event AlertEvent) (int64, time.Time, error) {
	var id int64
	var createdAt time.Time
	err := db.QueryRowContext(ctx, `INSERT INTO monitor_alert_events(alert_id,event_type,operator,message) VALUES($1,$2,$3,$4) RETURNING id,created_at`, event.AlertID, event.Type, event.Operator, event.Message).Scan(&id, &createdAt)
	return id, createdAt, err
}

func loadAlertEvents(ctx context.Context, db *sql.DB) (map[string][]AlertEvent, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,alert_id,event_type,operator,message,created_at FROM monitor_alert_events ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string][]AlertEvent)
	for rows.Next() {
		var event AlertEvent
		var createdAt time.Time
		if err := rows.Scan(&event.ID, &event.AlertID, &event.Type, &event.Operator, &event.Message, &createdAt); err != nil {
			return nil, err
		}
		event.CreatedAt = createdAt.Local().Format("2006-01-02 15:04:05")
		result[event.AlertID] = append(result[event.AlertID], event)
	}
	return result, rows.Err()
}
