package cmdb

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"
)

const integrationTimeLayout = "2006-01-02 15:04:05"

func (s *Service) IntegrationConfig(provider string) (IntegrationConfig, bool) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.integrations[provider]
	return item, ok
}

func (s *Service) IntegrationConfigs() []IntegrationConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	providers := make([]string, 0, len(s.integrations))
	for provider := range s.integrations {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	result := make([]IntegrationConfig, 0, len(providers))
	for _, provider := range providers {
		result = append(result, s.integrations[provider])
	}
	return result
}

func (s *Service) SaveIntegrationConfig(input IntegrationConfigInput, operator string) (IntegrationConfig, error) {
	provider := strings.TrimSpace(strings.ToLower(input.Provider))
	url := strings.TrimRight(strings.TrimSpace(input.URL), "/")
	if provider == "" || (input.Enabled && url == "") {
		return IntegrationConfig{}, ErrValidation
	}
	existing, _ := s.IntegrationConfig(provider)
	item := IntegrationConfig{
		Provider: provider, URL: url, Username: strings.TrimSpace(input.Username),
		CredentialID: strings.TrimSpace(input.CredentialID), AuthType: strings.TrimSpace(input.AuthType),
		Insecure: input.Insecure, Enabled: input.Enabled, UpdatedAt: time.Now().Format(integrationTimeLayout),
		AutoSyncEnabled: existing.AutoSyncEnabled, SyncIntervalMinutes: existing.SyncIntervalMinutes,
		NextSyncAt: existing.NextSyncAt, LastSyncAt: existing.LastSyncAt,
		LastSyncStatus: existing.LastSyncStatus, LastSyncMessage: existing.LastSyncMessage, LastSyncCount: existing.LastSyncCount,
	}
	if item.AuthType == "" {
		item.AuthType = "token"
	}
	if item.SyncIntervalMinutes <= 0 {
		item.SyncIntervalMinutes = 60
	}
	if err := s.persistIntegration(item); err != nil {
		return IntegrationConfig{}, err
	}
	_ = operator
	return item, nil
}

func (s *Service) SaveIntegrationSchedule(provider string, input IntegrationScheduleInput, operator string) (IntegrationConfig, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	item, ok := s.IntegrationConfig(provider)
	if !ok {
		return IntegrationConfig{}, ErrNotFound
	}
	interval := input.IntervalMinutes
	if interval <= 0 {
		interval = 60
	}
	if interval < 5 {
		interval = 5
	}
	if interval > 43200 {
		interval = 43200
	}
	item.AutoSyncEnabled = input.Enabled
	item.SyncIntervalMinutes = interval
	if input.Enabled {
		item.NextSyncAt = time.Now().Add(time.Duration(interval) * time.Minute).Format(integrationTimeLayout)
	} else {
		item.NextSyncAt = ""
	}
	item.UpdatedAt = time.Now().Format(integrationTimeLayout)
	if err := s.persistIntegration(item); err != nil {
		return IntegrationConfig{}, err
	}
	_ = operator
	return item, nil
}

func (s *Service) SaveIntegrationSyncResult(provider, status, message string, count int, nextSyncAt time.Time) (IntegrationConfig, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	item, ok := s.IntegrationConfig(provider)
	if !ok {
		return IntegrationConfig{}, ErrNotFound
	}
	item.LastSyncAt = time.Now().Format(integrationTimeLayout)
	item.LastSyncStatus = strings.TrimSpace(status)
	item.LastSyncMessage = strings.TrimSpace(message)
	item.LastSyncCount = count
	if item.AutoSyncEnabled {
		item.NextSyncAt = nextSyncAt.Format(integrationTimeLayout)
	} else {
		item.NextSyncAt = ""
	}
	item.UpdatedAt = time.Now().Format(integrationTimeLayout)
	if err := s.persistIntegration(item); err != nil {
		return IntegrationConfig{}, err
	}
	return item, nil
}

func (s *Service) persistIntegration(item IntegrationConfig) error {
	if s.db != nil {
		if err := upsertIntegrationConfig(context.Background(), s.db, item); err != nil {
			return err
		}
	}
	s.mu.Lock()
	if s.integrations == nil {
		s.integrations = map[string]IntegrationConfig{}
	}
	s.integrations[item.Provider] = item
	s.mu.Unlock()
	return nil
}

func loadIntegrationConfigs(ctx context.Context, db *sql.DB) (map[string]IntegrationConfig, error) {
	rows, err := db.QueryContext(ctx, `SELECT provider,url,username,credential_id,auth_type,insecure,enabled,updated_at,auto_sync_enabled,sync_interval_minutes,next_sync_at,last_sync_at,last_sync_status,last_sync_message,last_sync_count FROM cmdb_integration_configs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]IntegrationConfig{}
	for rows.Next() {
		var item IntegrationConfig
		var updated time.Time
		var nextSync, lastSync sql.NullTime
		if err = rows.Scan(&item.Provider, &item.URL, &item.Username, &item.CredentialID, &item.AuthType, &item.Insecure, &item.Enabled, &updated, &item.AutoSyncEnabled, &item.SyncIntervalMinutes, &nextSync, &lastSync, &item.LastSyncStatus, &item.LastSyncMessage, &item.LastSyncCount); err != nil {
			return nil, err
		}
		item.UpdatedAt = updated.Local().Format(integrationTimeLayout)
		if nextSync.Valid {
			item.NextSyncAt = nextSync.Time.Local().Format(integrationTimeLayout)
		}
		if lastSync.Valid {
			item.LastSyncAt = lastSync.Time.Local().Format(integrationTimeLayout)
		}
		result[item.Provider] = item
	}
	return result, rows.Err()
}

func upsertIntegrationConfig(ctx context.Context, db *sql.DB, item IntegrationConfig) error {
	_, err := db.ExecContext(ctx, `INSERT INTO cmdb_integration_configs(provider,url,username,credential_id,auth_type,insecure,enabled,auto_sync_enabled,sync_interval_minutes,next_sync_at,last_sync_at,last_sync_status,last_sync_message,last_sync_count,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,now())
		ON CONFLICT(provider) DO UPDATE SET url=excluded.url,username=excluded.username,credential_id=excluded.credential_id,auth_type=excluded.auth_type,insecure=excluded.insecure,enabled=excluded.enabled,auto_sync_enabled=excluded.auto_sync_enabled,sync_interval_minutes=excluded.sync_interval_minutes,next_sync_at=excluded.next_sync_at,last_sync_at=excluded.last_sync_at,last_sync_status=excluded.last_sync_status,last_sync_message=excluded.last_sync_message,last_sync_count=excluded.last_sync_count,updated_at=now()`,
		item.Provider, item.URL, item.Username, item.CredentialID, item.AuthType, item.Insecure, item.Enabled,
		item.AutoSyncEnabled, item.SyncIntervalMinutes, integrationTimeValue(item.NextSyncAt), integrationTimeValue(item.LastSyncAt),
		item.LastSyncStatus, item.LastSyncMessage, item.LastSyncCount)
	return err
}

func integrationTimeValue(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := time.ParseInLocation(integrationTimeLayout, value, time.Local)
	if err != nil {
		return nil
	}
	return parsed
}
