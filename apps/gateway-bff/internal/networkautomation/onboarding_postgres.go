package networkautomation

import (
	"context"
	"database/sql"
	"time"
)

const onboardingSchema = `
CREATE TABLE IF NOT EXISTS cmdb_network_onboarding_requests (
  id text PRIMARY KEY,
  anomaly_id text NOT NULL,
  name text NOT NULL,
  ip text NOT NULL DEFAULT '',
  mac_address text NOT NULL DEFAULT '',
  vlan text NOT NULL DEFAULT '',
  asset_type text NOT NULL DEFAULT 'physical-server',
  environment text NOT NULL DEFAULT '',
  project_group text NOT NULL DEFAULT '',
  owner text NOT NULL DEFAULT '',
  location text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'pending',
  note text NOT NULL DEFAULT '',
  ticket_id text NOT NULL DEFAULT '',
  asset_id text NOT NULL DEFAULT '',
  rule_id text NOT NULL DEFAULT '',
  created_by text NOT NULL DEFAULT '',
  approved_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
`

func loadOnboardingRequests(ctx context.Context, db *sql.DB) ([]OnboardingRequest, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,anomaly_id,name,ip,mac_address,vlan,asset_type,environment,project_group,owner,location,status,note,ticket_id,asset_id,rule_id,created_by,approved_by,created_at,updated_at FROM cmdb_network_onboarding_requests ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []OnboardingRequest{}
	for rows.Next() {
		var item OnboardingRequest
		var created, updated time.Time
		if err = rows.Scan(&item.ID, &item.AnomalyID, &item.Name, &item.IP, &item.MACAddress, &item.VLAN, &item.AssetType, &item.Environment, &item.ProjectGroup, &item.Owner, &item.Location, &item.Status, &item.Note, &item.TicketID, &item.AssetID, &item.RuleID, &item.CreatedBy, &item.ApprovedBy, &created, &updated); err != nil {
			return nil, err
		}
		item.CreatedAt = created.Local().Format("2006-01-02 15:04:05")
		item.UpdatedAt = updated.Local().Format("2006-01-02 15:04:05")
		result = append(result, item)
	}
	return result, rows.Err()
}

func upsertOnboardingRequest(ctx context.Context, db *sql.DB, item OnboardingRequest) error {
	_, err := db.ExecContext(ctx, `INSERT INTO cmdb_network_onboarding_requests(id,anomaly_id,name,ip,mac_address,vlan,asset_type,environment,project_group,owner,location,status,note,ticket_id,asset_id,rule_id,created_by,approved_by,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,now(),now())
		ON CONFLICT(id) DO UPDATE SET name=excluded.name,ip=excluded.ip,mac_address=excluded.mac_address,vlan=excluded.vlan,asset_type=excluded.asset_type,environment=excluded.environment,project_group=excluded.project_group,owner=excluded.owner,location=excluded.location,status=excluded.status,note=excluded.note,ticket_id=excluded.ticket_id,asset_id=excluded.asset_id,rule_id=excluded.rule_id,approved_by=excluded.approved_by,updated_at=now()`, item.ID, item.AnomalyID, item.Name, item.IP, item.MACAddress, item.VLAN, item.AssetType, item.Environment, item.ProjectGroup, item.Owner, item.Location, item.Status, item.Note, item.TicketID, item.AssetID, item.RuleID, item.CreatedBy, item.ApprovedBy)
	return err
}
