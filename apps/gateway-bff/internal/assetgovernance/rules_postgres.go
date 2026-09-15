package assetgovernance

import (
	"context"
	"database/sql"
	"time"
)

const governanceRuleSchema = `
CREATE TABLE IF NOT EXISTS cmdb_asset_governance_rules (
  id text PRIMARY KEY,
  name text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  priority integer NOT NULL DEFAULT 10,
  match_asset_type text NOT NULL DEFAULT '',
  match_source text NOT NULL DEFAULT '',
  match_ip_cidr text NOT NULL DEFAULT '',
  match_project_group text NOT NULL DEFAULT '',
  match_tag text NOT NULL DEFAULT '',
  owner text NOT NULL,
  target_project_group text NOT NULL DEFAULT '',
  location text NOT NULL DEFAULT '',
  note text NOT NULL DEFAULT '',
  created_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cmdb_asset_governance_rules_priority ON cmdb_asset_governance_rules(enabled,priority DESC);
`

func loadGovernanceRules(ctx context.Context, db *sql.DB) ([]GovernanceRule, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,name,enabled,priority,match_asset_type,match_source,match_ip_cidr,match_project_group,match_tag,owner,target_project_group,location,note,created_by,created_at,updated_at FROM cmdb_asset_governance_rules ORDER BY priority DESC,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []GovernanceRule{}
	for rows.Next() {
		var item GovernanceRule
		var created, updated time.Time
		if err = rows.Scan(&item.ID, &item.Name, &item.Enabled, &item.Priority, &item.MatchAssetType, &item.MatchSource, &item.MatchIPCIDR, &item.MatchProjectGroup, &item.MatchTag, &item.Owner, &item.TargetProjectGroup, &item.Location, &item.Note, &item.CreatedBy, &created, &updated); err != nil {
			return nil, err
		}
		item.CreatedAt = created.Local().Format("2006-01-02 15:04:05")
		item.UpdatedAt = updated.Local().Format("2006-01-02 15:04:05")
		result = append(result, item)
	}
	return result, rows.Err()
}

func upsertGovernanceRule(ctx context.Context, db *sql.DB, item GovernanceRule) error {
	_, err := db.ExecContext(ctx, `INSERT INTO cmdb_asset_governance_rules(id,name,enabled,priority,match_asset_type,match_source,match_ip_cidr,match_project_group,match_tag,owner,target_project_group,location,note,created_by,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,now(),now())
		ON CONFLICT(id) DO UPDATE SET name=excluded.name,enabled=excluded.enabled,priority=excluded.priority,match_asset_type=excluded.match_asset_type,match_source=excluded.match_source,match_ip_cidr=excluded.match_ip_cidr,match_project_group=excluded.match_project_group,match_tag=excluded.match_tag,owner=excluded.owner,target_project_group=excluded.target_project_group,location=excluded.location,note=excluded.note,updated_at=now()`,
		item.ID, item.Name, item.Enabled, item.Priority, item.MatchAssetType, item.MatchSource, item.MatchIPCIDR, item.MatchProjectGroup, item.MatchTag, item.Owner, item.TargetProjectGroup, item.Location, item.Note, item.CreatedBy)
	return err
}

func deleteGovernanceRule(ctx context.Context, db *sql.DB, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM cmdb_asset_governance_rules WHERE id=$1`, id)
	return err
}
