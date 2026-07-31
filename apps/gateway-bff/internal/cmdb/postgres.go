package cmdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const schema = `
CREATE TABLE IF NOT EXISTS cmdb_models (
  code text PRIMARY KEY,name text NOT NULL,category text NOT NULL,icon text NOT NULL DEFAULT 'Box',
  description text NOT NULL DEFAULT '',enabled boolean NOT NULL DEFAULT true,
  fields jsonb NOT NULL DEFAULT '[]',created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS cmdb_assets (
  id text PRIMARY KEY,
  name text NOT NULL,
  type text NOT NULL,
  type_name text NOT NULL,
  status text NOT NULL,
  ip text NOT NULL DEFAULT '',
  environment text NOT NULL DEFAULT '',
  project_group text NOT NULL,
  owner text NOT NULL,
  location text NOT NULL DEFAULT '',
  source text NOT NULL DEFAULT '',
  last_seen_at timestamptz NOT NULL,
  tags jsonb NOT NULL DEFAULT '[]',
  attributes jsonb NOT NULL DEFAULT '[]',
  relations jsonb NOT NULL DEFAULT '[]',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cmdb_assets_type ON cmdb_assets(type);
CREATE INDEX IF NOT EXISTS idx_cmdb_assets_status ON cmdb_assets(status);
CREATE INDEX IF NOT EXISTS idx_cmdb_assets_project_group ON cmdb_assets(project_group);
CREATE TABLE IF NOT EXISTS cmdb_asset_history (
  id text PRIMARY KEY,
  asset_id text NOT NULL REFERENCES cmdb_assets(id) ON DELETE CASCADE,
  action text NOT NULL,
  operator text NOT NULL,
  occurred_at timestamptz NOT NULL,
  changes jsonb NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS idx_cmdb_asset_history_asset_time
  ON cmdb_asset_history(asset_id, occurred_at DESC);
CREATE TABLE IF NOT EXISTS event_outbox (
  id text PRIMARY KEY,
  topic text NOT NULL,
  event_key text NOT NULL,
  payload jsonb NOT NULL,
  attempts integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  published_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_event_outbox_pending
  ON event_outbox(next_attempt_at, created_at) WHERE published_at IS NULL;
`

func loadModels(ctx context.Context, db *sql.DB) ([]Model, error) {
	rows, err := db.QueryContext(ctx, `SELECT code,name,category,icon,description,enabled,fields FROM cmdb_models ORDER BY category,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Model
	for rows.Next() {
		var m Model
		var fields []byte
		if err = rows.Scan(&m.Code, &m.Name, &m.Category, &m.Icon, &m.Description, &m.Enabled, &fields); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(fields, &m.Fields)
		out = append(out, m)
	}
	return out, rows.Err()
}
func upsertModel(ctx context.Context, db execer, m Model) error {
	fields, _ := json.Marshal(m.Fields)
	_, err := db.ExecContext(ctx, `INSERT INTO cmdb_models(code,name,category,icon,description,enabled,fields,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,now()) ON CONFLICT(code) DO UPDATE SET name=excluded.name,category=excluded.category,icon=excluded.icon,description=excluded.description,enabled=excluded.enabled,fields=excluded.fields,updated_at=now()`, m.Code, m.Name, m.Category, m.Icon, m.Description, m.Enabled, fields)
	return err
}

func openPostgres(databaseURL string, seed []Asset) (*sql.DB, []Asset, map[string][]HistoryEntry, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, nil, nil, fmt.Errorf("ping postgres: %w", err)
	}
	if _, err = db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, nil, nil, fmt.Errorf("migrate cmdb schema: %w", err)
	}
	var count int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM cmdb_assets").Scan(&count); err != nil {
		db.Close()
		return nil, nil, nil, err
	}
	if count == 0 {
		tx, txErr := db.BeginTx(ctx, nil)
		if txErr != nil {
			db.Close()
			return nil, nil, nil, txErr
		}
		for _, item := range seed {
			if txErr = insertAsset(ctx, tx, item); txErr != nil {
				_ = tx.Rollback()
				db.Close()
				return nil, nil, nil, fmt.Errorf("seed cmdb asset %s: %w", item.ID, txErr)
			}
		}
		if txErr = tx.Commit(); txErr != nil {
			db.Close()
			return nil, nil, nil, txErr
		}
	}
	assets, err := loadAssets(ctx, db)
	if err != nil {
		db.Close()
		return nil, nil, nil, err
	}
	history, err := loadHistory(ctx, db)
	if err != nil {
		db.Close()
		return nil, nil, nil, err
	}
	return db, assets, history, nil
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertAsset(ctx context.Context, db execer, a Asset) error {
	tags, _ := json.Marshal(a.Tags)
	attributes, _ := json.Marshal(a.Attributes)
	relations, _ := json.Marshal(a.Relations)
	lastSeen, err := time.ParseInLocation("2006-01-02 15:04:05", a.LastSeenAt, time.Local)
	if err != nil {
		lastSeen = time.Now()
	}
	_, err = db.ExecContext(ctx, `INSERT INTO cmdb_assets
		(id,name,type,type_name,status,ip,environment,project_group,owner,location,source,last_seen_at,tags,attributes,relations)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		a.ID, a.Name, a.Type, a.TypeName, a.Status, a.IP, a.Environment, a.ProjectGroup,
		a.Owner, a.Location, a.Source, lastSeen, tags, attributes, relations)
	return err
}

func insertHistory(ctx context.Context, db execer, h HistoryEntry) error {
	changes, _ := json.Marshal(h.Changes)
	occurredAt, err := time.ParseInLocation("2006-01-02 15:04:05", h.OccurredAt, time.Local)
	if err != nil {
		occurredAt = time.Now()
	}
	_, err = db.ExecContext(ctx, `INSERT INTO cmdb_asset_history(id,asset_id,action,operator,occurred_at,changes)
		VALUES($1,$2,$3,$4,$5,$6)`, h.ID, h.AssetID, h.Action, h.Operator, occurredAt, changes)
	return err
}

func insertAssetEvents(ctx context.Context, db execer, asset Asset, history HistoryEntry) error {
	occurredAt := time.Now().UTC().Format(time.RFC3339Nano)
	assetPayload, err := json.Marshal(map[string]any{
		"eventId": history.ID + "-asset", "eventType": "cmdb.asset.changed.v1",
		"occurredAt": occurredAt, "producer": "gateway-bff",
		"data": map[string]any{"asset": asset, "action": history.Action, "operator": history.Operator, "changes": history.Changes},
	})
	if err != nil {
		return err
	}
	auditPayload, err := json.Marshal(map[string]any{
		"eventId": history.ID + "-audit", "eventType": "audit.operation.created.v1",
		"occurredAt": occurredAt, "producer": "gateway-bff",
		"data": map[string]any{"resourceType": "cmdb.asset", "resourceId": asset.ID, "action": history.Action, "operator": history.Operator, "changes": history.Changes},
	})
	if err != nil {
		return err
	}
	for _, event := range []struct {
		id, topic string
		payload   []byte
	}{
		{history.ID + "-asset", "cmdb.asset.changed.v1", assetPayload},
		{history.ID + "-audit", "audit.operation.created.v1", auditPayload},
	} {
		if _, err = db.ExecContext(ctx, `INSERT INTO event_outbox(id,topic,event_key,payload) VALUES($1,$2,$3,$4)`, event.id, event.topic, asset.ID, event.payload); err != nil {
			return err
		}
	}
	return nil
}

func loadAssets(ctx context.Context, db *sql.DB) ([]Asset, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,name,type,type_name,status,ip,environment,project_group,
		owner,location,source,last_seen_at,tags,attributes,relations FROM cmdb_assets ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Asset
	for rows.Next() {
		var a Asset
		var lastSeen time.Time
		var tags, attributes, relations []byte
		if err = rows.Scan(&a.ID, &a.Name, &a.Type, &a.TypeName, &a.Status, &a.IP, &a.Environment,
			&a.ProjectGroup, &a.Owner, &a.Location, &a.Source, &lastSeen, &tags, &attributes, &relations); err != nil {
			return nil, err
		}
		a.LastSeenAt = lastSeen.Local().Format("2006-01-02 15:04:05")
		_ = json.Unmarshal(tags, &a.Tags)
		_ = json.Unmarshal(attributes, &a.Attributes)
		_ = json.Unmarshal(relations, &a.Relations)
		result = append(result, a)
	}
	return result, rows.Err()
}

func loadHistory(ctx context.Context, db *sql.DB) (map[string][]HistoryEntry, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,asset_id,action,operator,occurred_at,changes
		FROM cmdb_asset_history ORDER BY occurred_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string][]HistoryEntry)
	for rows.Next() {
		var h HistoryEntry
		var occurred time.Time
		var changes []byte
		if err = rows.Scan(&h.ID, &h.AssetID, &h.Action, &h.Operator, &occurred, &changes); err != nil {
			return nil, err
		}
		h.OccurredAt = occurred.Local().Format("2006-01-02 15:04:05")
		_ = json.Unmarshal(changes, &h.Changes)
		result[h.AssetID] = append(result[h.AssetID], h)
	}
	return result, rows.Err()
}
