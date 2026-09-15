package releases

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const releasesSchema = `
CREATE TABLE IF NOT EXISTS release_deployments(
 id text PRIMARY KEY,
 application text NOT NULL,
 version text NOT NULL,
 previous_version text NOT NULL DEFAULT '',
 environment text NOT NULL,
 status text NOT NULL,
 progress integer NOT NULL DEFAULT 0,
 operator_name text NOT NULL DEFAULT '',
 started_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 strategy text NOT NULL DEFAULT '',
 approval text NOT NULL DEFAULT '',
 approval_type text NOT NULL DEFAULT 'deployment',
 approval_requester text NOT NULL DEFAULT '',
 ticket_id text NOT NULL DEFAULT '',
 source text NOT NULL DEFAULT 'manual',
 external_id text NOT NULL DEFAULT '',
 commit_sha text NOT NULL DEFAULT '',
 stages jsonb NOT NULL DEFAULT '[]'::jsonb,
 approval_comment text NOT NULL DEFAULT '',
 approved_by text NOT NULL DEFAULT '',
 approved_at timestamptz,
 authorization_hash text NOT NULL DEFAULT '',
 authorization_expires_at timestamptz,
 authorization_used_at timestamptz
);
ALTER TABLE release_deployments ADD COLUMN IF NOT EXISTS approval_type text NOT NULL DEFAULT 'deployment';
ALTER TABLE release_deployments ADD COLUMN IF NOT EXISTS approval_requester text NOT NULL DEFAULT '';
ALTER TABLE release_deployments ADD COLUMN IF NOT EXISTS approval_comment text NOT NULL DEFAULT '';
ALTER TABLE release_deployments ADD COLUMN IF NOT EXISTS approved_by text NOT NULL DEFAULT '';
ALTER TABLE release_deployments ADD COLUMN IF NOT EXISTS approved_at timestamptz;
ALTER TABLE release_deployments ADD COLUMN IF NOT EXISTS authorization_hash text NOT NULL DEFAULT '';
ALTER TABLE release_deployments ADD COLUMN IF NOT EXISTS authorization_expires_at timestamptz;
ALTER TABLE release_deployments ADD COLUMN IF NOT EXISTS authorization_used_at timestamptz;
CREATE INDEX IF NOT EXISTS idx_release_deployments_updated ON release_deployments(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_release_deployments_application ON release_deployments(application,version,environment);
DROP INDEX IF EXISTS idx_release_deployments_external;
CREATE UNIQUE INDEX IF NOT EXISTS idx_release_deployments_external ON release_deployments(external_id,application,version,environment) WHERE external_id <> '';
CREATE TABLE IF NOT EXISTS release_artifacts(
 id text PRIMARY KEY,
 application text NOT NULL,
 version text NOT NULL,
 commit_sha text NOT NULL DEFAULT '',
 size text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 security text NOT NULL DEFAULT '',
 source text NOT NULL DEFAULT '',
 url text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_release_artifacts_created ON release_artifacts(created_at DESC);
`

type releaseQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type releaseExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type releaseScanner interface {
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
	if _, err := db.ExecContext(ctx, releasesSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func contextWithTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
func upsertRelease(ctx context.Context, execer releaseExecer, item Release) error {
	stages, _ := json.Marshal(item.Stages)
	_, err := execer.ExecContext(ctx, `
INSERT INTO release_deployments(
 id,application,version,previous_version,environment,status,progress,operator_name,started_at,updated_at,
 strategy,approval,approval_type,approval_requester,ticket_id,source,external_id,commit_sha,stages,approval_comment,approved_by,approved_at,
 authorization_hash,authorization_expires_at,authorization_used_at
) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)
ON CONFLICT(id) DO UPDATE SET
 application=excluded.application,version=excluded.version,previous_version=excluded.previous_version,
 environment=excluded.environment,status=excluded.status,progress=excluded.progress,
 operator_name=excluded.operator_name,updated_at=excluded.updated_at,strategy=excluded.strategy,
 approval=excluded.approval,approval_type=excluded.approval_type,approval_requester=excluded.approval_requester,
 ticket_id=excluded.ticket_id,source=excluded.source,
 external_id=excluded.external_id,commit_sha=excluded.commit_sha,stages=excluded.stages,
 approval_comment=excluded.approval_comment,approved_by=excluded.approved_by,approved_at=excluded.approved_at,
 authorization_hash=excluded.authorization_hash,authorization_expires_at=excluded.authorization_expires_at,
 authorization_used_at=excluded.authorization_used_at`,
		item.ID, item.Application, item.Version, item.PreviousVersion, item.Environment,
		item.Status, item.Progress, item.Operator, item.startedAt, item.updatedAt,
		item.Strategy, item.Approval, item.ApprovalType, item.ApprovalRequester, item.TicketID,
		item.Source, item.ExternalID, item.Commit, stages, item.ApprovalComment,
		item.ApprovedBy, nullableTime(item.approvedAt), item.authorizationHash,
		nullableTime(item.authorizationExpiresAt), nullableTime(item.authorizationUsedAt),
	)
	return err
}
func upsertArtifact(ctx context.Context, execer releaseExecer, item Artifact) error {
	_, err := execer.ExecContext(ctx, `
INSERT INTO release_artifacts(id,application,version,commit_sha,size,created_at,security,source,url)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(id) DO UPDATE SET
 application=excluded.application,version=excluded.version,commit_sha=excluded.commit_sha,
 size=excluded.size,security=excluded.security,source=excluded.source,url=excluded.url`,
		item.ID, item.Application, item.Version, item.Commit, item.Size, item.createdAt,
		item.Security, item.Source, item.URL,
	)
	return err
}

func loadReleases(ctx context.Context, queryer releaseQueryer) ([]Release, error) {
	rows, err := queryer.QueryContext(ctx, releaseSelectSQL+` ORDER BY updated_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Release{}
	for rows.Next() {
		item, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		applyReleaseDerived(&item)
		items = append(items, item)
	}
	return items, rows.Err()
}

const releaseSelectSQL = `SELECT
 id,application,version,previous_version,environment,status,progress,operator_name,started_at,updated_at,
 strategy,approval,approval_type,approval_requester,ticket_id,source,external_id,commit_sha,stages,approval_comment,approved_by,approved_at,
 authorization_hash,authorization_expires_at,authorization_used_at
FROM release_deployments`

func loadReleaseByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (Release, error) {
	row := tx.QueryRowContext(ctx, releaseSelectSQL+` WHERE id=$1 FOR UPDATE`, id)
	item, err := scanRelease(row)
	if err == sql.ErrNoRows {
		return Release{}, ErrNotFound
	}
	if err != nil {
		return Release{}, err
	}
	applyReleaseDerived(&item)
	return item, nil
}

func findPendingManualRelease(ctx context.Context, tx *sql.Tx, in EventInput) (Release, error) {
	row := tx.QueryRowContext(ctx, releaseSelectSQL+` WHERE source='manual' AND approval='pending' AND application=$1 AND version=$2 AND environment=$3 ORDER BY updated_at DESC LIMIT 1 FOR UPDATE`, in.Application, in.Version, in.Environment)
	item, err := scanRelease(row)
	if err == sql.ErrNoRows {
		return Release{}, ErrNotFound
	}
	if err != nil {
		return Release{}, err
	}
	applyReleaseDerived(&item)
	return item, nil
}
func scanRelease(scanner releaseScanner) (Release, error) {
	var item Release
	var stages []byte
	var approvedAt, authorizationExpiresAt, authorizationUsedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID, &item.Application, &item.Version, &item.PreviousVersion,
		&item.Environment, &item.Status, &item.Progress, &item.Operator,
		&item.startedAt, &item.updatedAt, &item.Strategy, &item.Approval,
		&item.ApprovalType, &item.ApprovalRequester, &item.TicketID, &item.Source,
		&item.ExternalID, &item.Commit, &stages,
		&item.ApprovalComment, &item.ApprovedBy, &approvedAt,
		&item.authorizationHash, &authorizationExpiresAt, &authorizationUsedAt,
	); err != nil {
		return Release{}, err
	}
	if err := json.Unmarshal(stages, &item.Stages); err != nil {
		return Release{}, err
	}
	if item.Stages == nil {
		item.Stages = []Stage{}
	}
	if approvedAt.Valid {
		item.approvedAt = approvedAt.Time
	}
	if authorizationExpiresAt.Valid {
		item.authorizationExpiresAt = authorizationExpiresAt.Time
	}
	if authorizationUsedAt.Valid {
		item.authorizationUsedAt = authorizationUsedAt.Time
	}
	return item, nil
}

func loadArtifacts(ctx context.Context, queryer releaseQueryer) ([]Artifact, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT id,application,version,commit_sha,size,created_at,security,source,url FROM release_artifacts ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Artifact{}
	for rows.Next() {
		item, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		applyArtifactDerived(&item)
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanArtifact(scanner releaseScanner) (Artifact, error) {
	var item Artifact
	if err := scanner.Scan(
		&item.ID, &item.Application, &item.Version, &item.Commit, &item.Size,
		&item.createdAt, &item.Security, &item.Source, &item.URL,
	); err != nil {
		return Artifact{}, err
	}
	return item, nil
}
