package assetgovernance

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const schema = `
CREATE TABLE IF NOT EXISTS cmdb_asset_governance_reviews (
  asset_id text PRIMARY KEY,
  status text NOT NULL DEFAULT 'pending',
  owner text NOT NULL DEFAULT '',
  project_group text NOT NULL DEFAULT '',
  location text NOT NULL DEFAULT '',
  reviewed_at timestamptz,
  reviewed_by text NOT NULL DEFAULT '',
  note text NOT NULL DEFAULT '',
  ticket_id text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cmdb_asset_governance_status ON cmdb_asset_governance_reviews(status,updated_at DESC);
`

func openPostgres(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err = db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err = db.ExecContext(ctx, governanceRuleSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func loadReviews(ctx context.Context, db *sql.DB) (map[string]Review, error) {
	rows, err := db.QueryContext(ctx, `SELECT asset_id,status,owner,project_group,location,reviewed_at,reviewed_by,note,ticket_id,updated_at FROM cmdb_asset_governance_reviews`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]Review{}
	for rows.Next() {
		var item Review
		var reviewedAt sql.NullTime
		var updatedAt time.Time
		if err = rows.Scan(&item.AssetID, &item.Status, &item.Owner, &item.ProjectGroup, &item.Location, &reviewedAt, &item.ReviewedBy, &item.Note, &item.TicketID, &updatedAt); err != nil {
			return nil, err
		}
		if reviewedAt.Valid {
			item.ReviewedAt = reviewedAt.Time.Local().Format("2006-01-02 15:04:05")
		}
		item.UpdatedAt = updatedAt.Local().Format("2006-01-02 15:04:05")
		result[item.AssetID] = item
	}
	return result, rows.Err()
}

func upsertReview(ctx context.Context, db *sql.DB, item Review) error {
	_, err := db.ExecContext(ctx, `INSERT INTO cmdb_asset_governance_reviews(asset_id,status,owner,project_group,location,reviewed_at,reviewed_by,note,ticket_id,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,now())
		ON CONFLICT(asset_id) DO UPDATE SET status=excluded.status,owner=excluded.owner,project_group=excluded.project_group,location=excluded.location,reviewed_at=excluded.reviewed_at,reviewed_by=excluded.reviewed_by,note=excluded.note,ticket_id=excluded.ticket_id,updated_at=now()`,
		item.AssetID, item.Status, item.Owner, item.ProjectGroup, item.Location, nullableReviewTime(item.ReviewedAt), item.ReviewedBy, item.Note, item.TicketID)
	return err
}

func nullableReviewTime(value string) any {
	if value == "" {
		return nil
	}
	return value
}
