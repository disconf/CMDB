package system

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const schema = `
CREATE TABLE IF NOT EXISTS platform_users (
  id text PRIMARY KEY,
  username text NOT NULL UNIQUE,
  display_name text NOT NULL DEFAULT '',
  department text NOT NULL DEFAULT '',
  email text NOT NULL DEFAULT '',
  roles jsonb NOT NULL DEFAULT '[]',
  status text NOT NULL DEFAULT 'active',
  last_login text NOT NULL DEFAULT '',
  data_scope text NOT NULL DEFAULT 'groups',
  project_groups jsonb NOT NULL DEFAULT '[]',
  scope_tags jsonb NOT NULL DEFAULT '[]',
  keycloak_groups jsonb NOT NULL DEFAULT '[]',
  keycloak_id text NOT NULL DEFAULT '',
  synced_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE platform_users ADD COLUMN IF NOT EXISTS deleted_at timestamptz;
ALTER TABLE platform_users ADD COLUMN IF NOT EXISTS deleted_by text NOT NULL DEFAULT '';
ALTER TABLE platform_users ADD COLUMN IF NOT EXISTS permission_overrides jsonb NOT NULL DEFAULT '[]';
CREATE TABLE IF NOT EXISTS platform_roles (
  id text PRIMARY KEY,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  permissions jsonb NOT NULL DEFAULT '[]',
  data_scope text NOT NULL DEFAULT 'groups',
  project_groups jsonb NOT NULL DEFAULT '[]',
  scope_tags jsonb NOT NULL DEFAULT '[]',
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS platform_modules (
  code text PRIMARY KEY,
  name text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  sort_order integer NOT NULL DEFAULT 10,
  permission text NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS platform_settings (
  key text PRIMARY KEY,
  name text NOT NULL,
  value text NOT NULL DEFAULT '',
  category text NOT NULL DEFAULT '',
  description text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
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
		_ = db.Close()
		return nil, err
	}
	if _, err = db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (s *Service) seedDefaults(ctx context.Context) error {
	return seedSystemData(ctx, s.db, defaultUsers(), defaultRoles(), defaultModules(), defaultSettings())
}

func (s *Service) reload(ctx context.Context) error {
	users, roles, modules, settings, err := loadSystemData(ctx, s.db)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.users, s.roles, s.modules, s.settings = users, roles, modules, settings
	s.lastReload = time.Now()
	s.mu.Unlock()
	return nil
}

func seedSystemData(ctx context.Context, db *sql.DB, users []User, roles []Role, modules []Module, settings []Setting) error {
	if systemTableEmpty(ctx, db, "platform_users") {
		for _, item := range users {
			if err := upsertSystemUser(ctx, db, item); err != nil {
				return err
			}
		}
	}
	if systemTableEmpty(ctx, db, "platform_roles") {
		for _, item := range roles {
			if err := upsertSystemRole(ctx, db, item); err != nil {
				return err
			}
		}
	}
	if systemTableEmpty(ctx, db, "platform_modules") {
		for _, item := range modules {
			if _, err := db.ExecContext(ctx, `INSERT INTO platform_modules(code,name,enabled,sort_order,permission) VALUES($1,$2,$3,$4,$5)`, item.Code, item.Name, item.Enabled, item.Order, item.Permission); err != nil {
				return err
			}
		}
	}
	if systemTableEmpty(ctx, db, "platform_settings") {
		for _, item := range settings {
			if _, err := db.ExecContext(ctx, `INSERT INTO platform_settings(key,name,value,category,description) VALUES($1,$2,$3,$4,$5)`, item.Key, item.Name, item.Value, item.Category, item.Description); err != nil {
				return err
			}
		}
	}
	return nil
}

func systemTableEmpty(ctx context.Context, db *sql.DB, table string) bool {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
		return false
	}
	return count == 0
}

func loadSystemData(ctx context.Context, db *sql.DB) ([]User, []Role, []Module, []Setting, error) {
	users, err := loadUsers(ctx, db)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	roles, err := loadRoles(ctx, db)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	roleCounts := map[string]int{}
	for _, user := range users {
		if user.Status == "deleted" {
			continue
		}
		for _, role := range user.Roles {
			roleCounts[role]++
		}
	}
	for index := range roles {
		roles[index].Users = roleCounts[roles[index].ID]
	}
	modules, err := loadModules(ctx, db)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	settings, err := loadSettings(ctx, db)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return users, roles, modules, settings, nil
}

func loadUsers(ctx context.Context, db *sql.DB) ([]User, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,username,display_name,department,email,roles,status,last_login,data_scope,project_groups,scope_tags,keycloak_groups,keycloak_id,synced_at,deleted_at,deleted_by,permission_overrides FROM platform_users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []User{}
	for rows.Next() {
		var item User
		var roles, projectGroups, scopeTags, keycloakGroups, permissionOverrides []byte
		var syncedAt, deletedAt sql.NullTime
		if err = rows.Scan(&item.ID, &item.Username, &item.DisplayName, &item.Department, &item.Email, &roles, &item.Status, &item.LastLogin, &item.DataScope, &projectGroups, &scopeTags, &keycloakGroups, &item.KeycloakID, &syncedAt, &deletedAt, &item.DeletedBy, &permissionOverrides); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(roles, &item.Roles)
		_ = json.Unmarshal(projectGroups, &item.ProjectGroups)
		_ = json.Unmarshal(scopeTags, &item.ScopeTags)
		_ = json.Unmarshal(keycloakGroups, &item.KeycloakGroups)
		_ = json.Unmarshal(permissionOverrides, &item.PermissionOverrides)
		if syncedAt.Valid {
			item.SyncedAt = syncedAt.Time.Local().Format("2006-01-02 15:04:05")
		}
		if deletedAt.Valid {
			item.DeletedAt = deletedAt.Time.Local().Format("2006-01-02 15:04:05")
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadRoles(ctx context.Context, db *sql.DB) ([]Role, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,name,description,permissions,data_scope,project_groups,scope_tags FROM platform_roles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Role{}
	for rows.Next() {
		var item Role
		var permissions, projectGroups, scopeTags []byte
		if err = rows.Scan(&item.ID, &item.Name, &item.Description, &permissions, &item.DataScope, &projectGroups, &scopeTags); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(permissions, &item.Permissions)
		_ = json.Unmarshal(projectGroups, &item.ProjectGroups)
		_ = json.Unmarshal(scopeTags, &item.ScopeTags)
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadModules(ctx context.Context, db *sql.DB) ([]Module, error) {
	rows, err := db.QueryContext(ctx, `SELECT code,name,enabled,sort_order,permission FROM platform_modules ORDER BY sort_order,code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Module{}
	for rows.Next() {
		var item Module
		if err = rows.Scan(&item.Code, &item.Name, &item.Enabled, &item.Order, &item.Permission); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadSettings(ctx context.Context, db *sql.DB) ([]Setting, error) {
	rows, err := db.QueryContext(ctx, `SELECT key,name,value,category,description FROM platform_settings ORDER BY category,key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Setting{}
	for rows.Next() {
		var item Setting
		if err = rows.Scan(&item.Key, &item.Name, &item.Value, &item.Category, &item.Description); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func upsertSystemUser(ctx context.Context, db *sql.DB, item User) error {
	roles, _ := json.Marshal(item.Roles)
	projectGroups, _ := json.Marshal(item.ProjectGroups)
	scopeTags, _ := json.Marshal(item.ScopeTags)
	keycloakGroups, _ := json.Marshal(item.KeycloakGroups)
	permissionOverrides, _ := json.Marshal(item.PermissionOverrides)
	_, err := db.ExecContext(ctx, `INSERT INTO platform_users(id,username,display_name,department,email,roles,status,last_login,data_scope,project_groups,scope_tags,keycloak_groups,keycloak_id,synced_at,deleted_at,deleted_by,permission_overrides,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,now())
		ON CONFLICT(id) DO UPDATE SET username=excluded.username,display_name=excluded.display_name,department=excluded.department,email=excluded.email,roles=excluded.roles,status=excluded.status,last_login=excluded.last_login,data_scope=excluded.data_scope,project_groups=excluded.project_groups,scope_tags=excluded.scope_tags,keycloak_groups=excluded.keycloak_groups,keycloak_id=excluded.keycloak_id,synced_at=excluded.synced_at,deleted_at=excluded.deleted_at,deleted_by=excluded.deleted_by,permission_overrides=excluded.permission_overrides,updated_at=now()`,
		item.ID, item.Username, item.DisplayName, item.Department, item.Email, roles, item.Status, item.LastLogin, item.DataScope, projectGroups, scopeTags, keycloakGroups, item.KeycloakID, nullableTime(item.SyncedAt), nullableTime(item.DeletedAt), item.DeletedBy, permissionOverrides)
	return err
}

func upsertSystemRole(ctx context.Context, db *sql.DB, item Role) error {
	permissions, _ := json.Marshal(item.Permissions)
	projectGroups, _ := json.Marshal(item.ProjectGroups)
	scopeTags, _ := json.Marshal(item.ScopeTags)
	_, err := db.ExecContext(ctx, `INSERT INTO platform_roles(id,name,description,permissions,data_scope,project_groups,scope_tags,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,now())
		ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,permissions=excluded.permissions,data_scope=excluded.data_scope,project_groups=excluded.project_groups,scope_tags=excluded.scope_tags,updated_at=now()`, item.ID, item.Name, item.Description, permissions, item.DataScope, projectGroups, scopeTags)
	return err
}

func nullableTime(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func systemID(prefix, value string) string {
	if value == "" {
		return ""
	}
	return fmt.Sprintf("%s-%s", prefix, value)
}
