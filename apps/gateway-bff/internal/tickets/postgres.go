package tickets

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const ticketsSchema = `
CREATE SEQUENCE IF NOT EXISTS cmdb_ticket_id_seq;
CREATE TABLE IF NOT EXISTS cmdb_tickets(
 id text PRIMARY KEY,
 title text NOT NULL,
 ticket_type text NOT NULL,
 priority text NOT NULL DEFAULT 'medium',
 status text NOT NULL,
 applicant text NOT NULL,
 assignee text NOT NULL DEFAULT '',
 description text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 sla_due_at timestamptz NOT NULL DEFAULT now(),
 risk text NOT NULL DEFAULT 'medium',
 related_asset text NOT NULL DEFAULT '',
 related_alert text NOT NULL DEFAULT '',
 related_task text NOT NULL DEFAULT '',
 related_release text NOT NULL DEFAULT '',
 project_group text NOT NULL DEFAULT '',
 automation_template text NOT NULL DEFAULT '',
 steps jsonb NOT NULL DEFAULT '[]'::jsonb,
 timeline jsonb NOT NULL DEFAULT '[]'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_cmdb_tickets_updated ON cmdb_tickets(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_cmdb_tickets_status ON cmdb_tickets(status,created_at DESC);
CREATE INDEX IF NOT EXISTS idx_cmdb_tickets_alert ON cmdb_tickets(related_alert,status);ALTER TABLE cmdb_tickets ADD COLUMN IF NOT EXISTS related_task text NOT NULL DEFAULT '';ALTER TABLE cmdb_tickets ADD COLUMN IF NOT EXISTS related_release text NOT NULL DEFAULT '';
`

type ticketQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type ticketExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type ticketScanner interface {
	Scan(...any) error
}

func openPostgres(url string) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, ticketsSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func nextTicketID(ctx context.Context, queryer ticketQueryer, now time.Time) (string, error) {
	var sequence int64
	if err := queryer.QueryRowContext(ctx, `SELECT nextval('cmdb_ticket_id_seq')`).Scan(&sequence); err != nil {
		return "", err
	}
	return fmt.Sprintf("TKT-%s-%05d", now.Format("20060102"), sequence), nil
}

func upsertTicket(ctx context.Context, execer ticketExecer, item Ticket) error {
	steps, _ := json.Marshal(item.Steps)
	timeline, _ := json.Marshal(item.Timeline)
	_, err := execer.ExecContext(ctx, `
INSERT INTO cmdb_tickets(
 id,title,ticket_type,priority,status,applicant,assignee,description,created_at,updated_at,
 sla_due_at,risk,related_asset,related_alert,related_task,related_release,project_group,automation_template,steps,timeline
) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
ON CONFLICT(id) DO UPDATE SET
 title=excluded.title,ticket_type=excluded.ticket_type,priority=excluded.priority,status=excluded.status,
 applicant=excluded.applicant,assignee=excluded.assignee,description=excluded.description,
 updated_at=excluded.updated_at,sla_due_at=excluded.sla_due_at,risk=excluded.risk,
 related_asset=excluded.related_asset,related_alert=excluded.related_alert,related_task=excluded.related_task,related_release=excluded.related_release,
 project_group=excluded.project_group,automation_template=excluded.automation_template,
 steps=excluded.steps,timeline=excluded.timeline`,
		item.ID, item.Title, item.Type, normalizePriority(item.Priority), item.Status, item.Applicant,
		item.Assignee, item.Description, item.createdAt, item.updatedAt, item.slaDueAt,
		item.Risk, item.RelatedAsset, item.RelatedAlert, item.RelatedTask, item.RelatedRelease, item.ProjectGroup,
		item.AutomationTemplate, steps, timeline,
	)
	return err
}

func loadTickets(ctx context.Context, queryer ticketQueryer) ([]Ticket, error) {
	rows, err := queryer.QueryContext(ctx, ticketSelectSQL+` ORDER BY updated_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Ticket{}
	for rows.Next() {
		item, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		applyDerived(&item, time.Now())
		items = append(items, item)
	}
	return items, rows.Err()
}

const ticketSelectSQL = `SELECT
 id,title,ticket_type,priority,status,applicant,assignee,description,created_at,updated_at,
 sla_due_at,risk,related_asset,related_alert,related_task,related_release,project_group,automation_template,steps,timeline
FROM cmdb_tickets`

func loadTicketByID(ctx context.Context, queryer ticketQueryer, id string) (Ticket, error) {
	row := queryer.QueryRowContext(ctx, ticketSelectSQL+` WHERE id=$1`, id)
	item, err := scanTicket(row)
	if err == sql.ErrNoRows {
		return Ticket{}, ErrNotFound
	}
	if err != nil {
		return Ticket{}, err
	}
	applyDerived(&item, time.Now())
	return item, nil
}

func loadTicketByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (Ticket, error) {
	row := tx.QueryRowContext(ctx, ticketSelectSQL+` WHERE id=$1 FOR UPDATE`, id)
	item, err := scanTicket(row)
	if err == sql.ErrNoRows {
		return Ticket{}, ErrNotFound
	}
	if err != nil {
		return Ticket{}, err
	}
	applyDerived(&item, time.Now())
	return item, nil
}

func findTriggeredAlertTicket(ctx context.Context, tx *sql.Tx, alertID string) (Ticket, error) {
	row := tx.QueryRowContext(ctx, ticketSelectSQL+` WHERE related_alert=$1 AND status IN ('pending','approved','processing') ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, alertID)
	item, err := scanTicket(row)
	if err == sql.ErrNoRows {
		return Ticket{}, ErrNotFound
	}
	if err != nil {
		return Ticket{}, err
	}
	applyDerived(&item, time.Now())
	return item, nil
}

func scanTicket(scanner ticketScanner) (Ticket, error) {
	var item Ticket
	var steps, timeline []byte
	if err := scanner.Scan(
		&item.ID, &item.Title, &item.Type, &item.Priority, &item.Status,
		&item.Applicant, &item.Assignee, &item.Description, &item.createdAt,
		&item.updatedAt, &item.slaDueAt, &item.Risk, &item.RelatedAsset,
		&item.RelatedAlert, &item.RelatedTask, &item.RelatedRelease, &item.ProjectGroup, &item.AutomationTemplate,
		&steps, &timeline,
	); err != nil {
		return Ticket{}, err
	}
	if err := json.Unmarshal(steps, &item.Steps); err != nil {
		return Ticket{}, err
	}
	if err := json.Unmarshal(timeline, &item.Timeline); err != nil {
		return Ticket{}, err
	}
	if item.Steps == nil {
		item.Steps = []Step{}
	}
	if item.Timeline == nil {
		item.Timeline = []Event{}
	}
	return item, nil
}
