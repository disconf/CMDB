package networkautomation

import (
	"context"
	"database/sql"
	"time"
)

const complianceSchema = `
CREATE TABLE IF NOT EXISTS cmdb_network_compliance_rules (
  id text PRIMARY KEY,
  name text NOT NULL,
  action text NOT NULL DEFAULT 'allow',
  type text NOT NULL DEFAULT 'mac',
  value text NOT NULL,
  environment text NOT NULL DEFAULT '',
  project_group text NOT NULL DEFAULT '',
  location text NOT NULL DEFAULT '',
  vlan text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'pending',
  description text NOT NULL DEFAULT '',
  expires_at timestamptz,
  created_by text NOT NULL DEFAULT '',
  approved_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS cmdb_network_compliance_decisions (
  id text PRIMARY KEY,
  anomaly_id text NOT NULL,
  rule_id text NOT NULL DEFAULT '',
  rule_name text NOT NULL DEFAULT '',
  status text NOT NULL,
  reason text NOT NULL DEFAULT '',
  title text NOT NULL DEFAULT '',
  ip text NOT NULL DEFAULT '',
  mac_address text NOT NULL DEFAULT '',
  vlan text NOT NULL DEFAULT '',
  environment text NOT NULL DEFAULT '',
  project_group text NOT NULL DEFAULT '',
  location text NOT NULL DEFAULT '',
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz
);
`

func loadComplianceRules(ctx context.Context, db *sql.DB) ([]ComplianceRule, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,name,action,type,value,environment,project_group,location,vlan,status,description,expires_at,created_by,approved_by,created_at,updated_at FROM cmdb_network_compliance_rules ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ComplianceRule{}
	for rows.Next() {
		var item ComplianceRule
		var expires sql.NullTime
		var created, updated time.Time
		if err = rows.Scan(&item.ID, &item.Name, &item.Action, &item.Type, &item.Value, &item.Environment, &item.ProjectGroup, &item.Location, &item.VLAN, &item.Status, &item.Description, &expires, &item.CreatedBy, &item.ApprovedBy, &created, &updated); err != nil {
			return nil, err
		}
		if expires.Valid {
			item.ExpiresAt = expires.Time.Local().Format("2006-01-02 15:04:05")
		}
		item.CreatedAt = created.Local().Format("2006-01-02 15:04:05")
		item.UpdatedAt = updated.Local().Format("2006-01-02 15:04:05")
		result = append(result, item)
	}
	return result, rows.Err()
}

func upsertComplianceRule(ctx context.Context, db *sql.DB, item ComplianceRule) error {
	var expires any
	if item.ExpiresAt != "" {
		expires = item.ExpiresAt
	}
	_, err := db.ExecContext(ctx, `INSERT INTO cmdb_network_compliance_rules(id,name,action,type,value,environment,project_group,location,vlan,status,description,expires_at,created_by,approved_by,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,now(),now())
		ON CONFLICT(id) DO UPDATE SET name=excluded.name,action=excluded.action,type=excluded.type,value=excluded.value,environment=excluded.environment,project_group=excluded.project_group,location=excluded.location,vlan=excluded.vlan,status=excluded.status,description=excluded.description,expires_at=excluded.expires_at,approved_by=excluded.approved_by,updated_at=now()`, item.ID, item.Name, item.Action, item.Type, item.Value, item.Environment, item.ProjectGroup, item.Location, item.VLAN, item.Status, item.Description, expires, item.CreatedBy, item.ApprovedBy)
	return err
}

func upsertComplianceDecision(ctx context.Context, db *sql.DB, item ComplianceDecision) error {
	var expires any
	if item.ExpiresAt != "" {
		expires = item.ExpiresAt
	}
	_, err := db.ExecContext(ctx, `INSERT INTO cmdb_network_compliance_decisions(id,anomaly_id,rule_id,rule_name,status,reason,title,ip,mac_address,vlan,environment,project_group,location,first_seen_at,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now(),$14)
		ON CONFLICT(id) DO UPDATE SET rule_id=excluded.rule_id,rule_name=excluded.rule_name,status=excluded.status,reason=excluded.reason,title=excluded.title,ip=excluded.ip,mac_address=excluded.mac_address,vlan=excluded.vlan,environment=excluded.environment,project_group=excluded.project_group,location=excluded.location,expires_at=excluded.expires_at`, item.ID, item.AnomalyID, item.RuleID, item.RuleName, item.Status, item.Reason, item.Title, item.IP, item.MACAddress, item.VLAN, item.Environment, item.ProjectGroup, item.Location, expires)
	return err
}
