package cmdb

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func (s *Service) IntegrationConfig(provider string) (IntegrationConfig, bool) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.integrations[provider]
	return item, ok
}

func (s *Service) SaveIntegrationConfig(input IntegrationConfigInput, operator string) (IntegrationConfig, error) {
	provider := strings.TrimSpace(strings.ToLower(input.Provider))
	url := strings.TrimRight(strings.TrimSpace(input.URL), "/")
	if provider == "" || (input.Enabled && url == "") {
		return IntegrationConfig{}, ErrValidation
	}
	item := IntegrationConfig{
		Provider: provider, URL: url, Username: strings.TrimSpace(input.Username),
		CredentialID: strings.TrimSpace(input.CredentialID), AuthType: strings.TrimSpace(input.AuthType),
		Insecure: input.Insecure, Enabled: input.Enabled, UpdatedAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	if item.AuthType == "" {
		item.AuthType = "token"
	}
	if s.db != nil {
		if err := upsertIntegrationConfig(context.Background(), s.db, item); err != nil {
			return IntegrationConfig{}, err
		}
	}
	s.mu.Lock()
	if s.integrations == nil {
		s.integrations = map[string]IntegrationConfig{}
	}
	s.integrations[provider] = item
	s.mu.Unlock()
	_ = operator
	return item, nil
}

func loadIntegrationConfigs(ctx context.Context, db *sql.DB) (map[string]IntegrationConfig, error) {
	rows, err := db.QueryContext(ctx, `SELECT provider,url,username,credential_id,auth_type,insecure,enabled,updated_at FROM cmdb_integration_configs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]IntegrationConfig{}
	for rows.Next() {
		var item IntegrationConfig
		var updated time.Time
		if err = rows.Scan(&item.Provider, &item.URL, &item.Username, &item.CredentialID, &item.AuthType, &item.Insecure, &item.Enabled, &updated); err != nil {
			return nil, err
		}
		item.UpdatedAt = updated.Local().Format("2006-01-02 15:04:05")
		result[item.Provider] = item
	}
	return result, rows.Err()
}

func upsertIntegrationConfig(ctx context.Context, db *sql.DB, item IntegrationConfig) error {
	_, err := db.ExecContext(ctx, `INSERT INTO cmdb_integration_configs(provider,url,username,credential_id,auth_type,insecure,enabled,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,now())
		ON CONFLICT(provider) DO UPDATE SET url=excluded.url,username=excluded.username,credential_id=excluded.credential_id,auth_type=excluded.auth_type,insecure=excluded.insecure,enabled=excluded.enabled,updated_at=now()`,
		item.Provider, item.URL, item.Username, item.CredentialID, item.AuthType, item.Insecure, item.Enabled)
	return err
}
