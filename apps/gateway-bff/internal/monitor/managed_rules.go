package monitor

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const managedRuleConfigKey = "managed-rules.yml"

type ManagedAlertRule struct {
	ID          string `json:"id"`
	Group       string `json:"group"`
	Name        string `json:"name"`
	Query       string `json:"query"`
	Duration    string `json:"duration"`
	Severity    string `json:"severity"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

type CreateManagedAlertRuleInput struct {
	Group       string `json:"group"`
	Name        string `json:"name"`
	Query       string `json:"query"`
	Duration    string `json:"duration"`
	Severity    string `json:"severity"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
}

type ruleConfigMapClient interface {
	PatchConfigMapData(context.Context, string, string, string, string) error
}

var (
	managedRuleNamePattern     = regexp.MustCompile(`^[A-Za-z_:][A-Za-z0-9_:]*$`)
	managedRuleDurationPattern = regexp.MustCompile(`^[1-9][0-9]*(ms|s|m|h|d|w)$`)
)

func managedRuleID() string {
	data := make([]byte, 8)
	_, _ = rand.Read(data)
	return "rule-" + hex.EncodeToString(data)
}

func normalizeManagedRuleInput(input CreateManagedAlertRuleInput) (CreateManagedAlertRuleInput, error) {
	group := strings.TrimSpace(input.Group)
	if group == "" {
		group = "cmdb-custom"
	}
	name := strings.TrimSpace(input.Name)
	query := strings.TrimSpace(input.Query)
	duration := strings.ToLower(strings.TrimSpace(input.Duration))
	severity := strings.ToLower(strings.TrimSpace(input.Severity))
	summary := strings.TrimSpace(input.Summary)
	description := strings.TrimSpace(input.Description)
	if !managedRuleNamePattern.MatchString(name) {
		return input, fmt.Errorf("rule name must start with a letter, colon or underscore and contain only letters, numbers, colons or underscores")
	}
	if query == "" || len(query) > 8000 {
		return input, fmt.Errorf("PromQL expression is required and must not exceed 8000 characters")
	}
	if !managedRuleDurationPattern.MatchString(duration) {
		return input, fmt.Errorf("duration must use a format such as 30s, 5m, 1h or 1d")
	}
	switch severity {
	case "critical", "warning", "info":
	default:
		return input, fmt.Errorf("severity must be critical, warning or info")
	}
	if len(group) > 200 || len(summary) > 500 || len(description) > 2000 {
		return input, fmt.Errorf("rule fields are too long")
	}
	if summary == "" {
		summary = name
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	input.Group = group
	input.Name = name
	input.Query = query
	input.Duration = duration
	input.Severity = severity
	input.Summary = summary
	input.Description = description
	input.Enabled = &enabled
	return input, nil
}

func scanManagedAlertRule(scanner interface{ Scan(...any) error }) (ManagedAlertRule, error) {
	var item ManagedAlertRule
	err := scanner.Scan(&item.ID, &item.Group, &item.Name, &item.Query, &item.Duration, &item.Severity, &item.Summary, &item.Description, &item.Enabled)
	return item, err
}

func (s *Service) ManagedAlertRules() ([]ManagedAlertRule, error) {
	if s.db == nil {
		return []ManagedAlertRule{}, fmt.Errorf("database unavailable")
	}
	rows, err := s.db.Query(`SELECT id,group_name,name,query,duration,severity,summary,description,enabled FROM monitor_alert_rule_definitions ORDER BY group_name,name`)
	if err != nil {
		return []ManagedAlertRule{}, err
	}
	defer rows.Close()
	result := []ManagedAlertRule{}
	for rows.Next() {
		item, scanErr := scanManagedAlertRule(rows)
		if scanErr != nil {
			return []ManagedAlertRule{}, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) managedAlertRuleByID(id string) (ManagedAlertRule, error) {
	item, err := scanManagedAlertRule(s.db.QueryRow(`SELECT id,group_name,name,query,duration,severity,summary,description,enabled FROM monitor_alert_rule_definitions WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedAlertRule{}, ErrNotFound
	}
	return item, err
}

func (s *Service) managedRuleNameExists(group, name, excludeID string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM monitor_alert_rule_definitions WHERE group_name=$1 AND name=$2 AND id<>$3)`, group, name, excludeID).Scan(&exists)
	return exists, err
}

func (s *Service) CreateManagedAlertRule(ctx context.Context, input CreateManagedAlertRuleInput) (ManagedAlertRule, error) {
	if s.db == nil {
		return ManagedAlertRule{}, fmt.Errorf("database unavailable")
	}
	normalized, err := normalizeManagedRuleInput(input)
	if err != nil {
		return ManagedAlertRule{}, err
	}
	exists, err := s.managedRuleNameExists(normalized.Group, normalized.Name, "")
	if err != nil {
		return ManagedAlertRule{}, err
	}
	if exists {
		return ManagedAlertRule{}, fmt.Errorf("a rule with this name already exists in the group")
	}
	item := ManagedAlertRule{ID: managedRuleID(), Group: normalized.Group, Name: normalized.Name, Query: normalized.Query, Duration: normalized.Duration, Severity: normalized.Severity, Summary: normalized.Summary, Description: normalized.Description, Enabled: *normalized.Enabled}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO monitor_alert_rule_definitions(id,group_name,name,query,duration,severity,summary,description,enabled) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, item.ID, item.Group, item.Name, item.Query, item.Duration, item.Severity, item.Summary, item.Description, item.Enabled); err != nil {
		return ManagedAlertRule{}, err
	}
	if err = s.syncManagedAlertRules(ctx); err != nil {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM monitor_alert_rule_definitions WHERE id=$1`, item.ID)
		return ManagedAlertRule{}, fmt.Errorf("apply Prometheus rule: %w", err)
	}
	return item, nil
}

func (s *Service) UpdateManagedAlertRule(ctx context.Context, id string, input CreateManagedAlertRuleInput) (ManagedAlertRule, error) {
	if s.db == nil {
		return ManagedAlertRule{}, fmt.Errorf("database unavailable")
	}
	current, err := s.managedAlertRuleByID(id)
	if err != nil {
		return ManagedAlertRule{}, err
	}
	normalized, err := normalizeManagedRuleInput(input)
	if err != nil {
		return ManagedAlertRule{}, err
	}
	exists, err := s.managedRuleNameExists(normalized.Group, normalized.Name, id)
	if err != nil {
		return ManagedAlertRule{}, err
	}
	if exists {
		return ManagedAlertRule{}, fmt.Errorf("a rule with this name already exists in the group")
	}
	item := ManagedAlertRule{ID: id, Group: normalized.Group, Name: normalized.Name, Query: normalized.Query, Duration: normalized.Duration, Severity: normalized.Severity, Summary: normalized.Summary, Description: normalized.Description, Enabled: *normalized.Enabled}
	if _, err = s.db.ExecContext(ctx, `UPDATE monitor_alert_rule_definitions SET group_name=$2,name=$3,query=$4,duration=$5,severity=$6,summary=$7,description=$8,enabled=$9,updated_at=now() WHERE id=$1`, id, item.Group, item.Name, item.Query, item.Duration, item.Severity, item.Summary, item.Description, item.Enabled); err != nil {
		return ManagedAlertRule{}, err
	}
	if err = s.syncManagedAlertRules(ctx); err != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE monitor_alert_rule_definitions SET group_name=$2,name=$3,query=$4,duration=$5,severity=$6,summary=$7,description=$8,enabled=$9,updated_at=now() WHERE id=$1`, id, current.Group, current.Name, current.Query, current.Duration, current.Severity, current.Summary, current.Description, current.Enabled)
		return ManagedAlertRule{}, fmt.Errorf("apply Prometheus rule: %w", err)
	}
	return item, nil
}

func (s *Service) ToggleManagedAlertRule(ctx context.Context, id string) (ManagedAlertRule, error) {
	if s.db == nil {
		return ManagedAlertRule{}, fmt.Errorf("database unavailable")
	}
	current, err := s.managedAlertRuleByID(id)
	if err != nil {
		return ManagedAlertRule{}, err
	}
	item, err := scanManagedAlertRule(s.db.QueryRow(`UPDATE monitor_alert_rule_definitions SET enabled=NOT enabled,updated_at=now() WHERE id=$1 RETURNING id,group_name,name,query,duration,severity,summary,description,enabled`, id))
	if err != nil {
		return ManagedAlertRule{}, err
	}
	if err = s.syncManagedAlertRules(ctx); err != nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE monitor_alert_rule_definitions SET enabled=$2,updated_at=now() WHERE id=$1`, id, current.Enabled)
		return ManagedAlertRule{}, fmt.Errorf("apply Prometheus rule: %w", err)
	}
	return item, nil
}

func (s *Service) DeleteManagedAlertRule(ctx context.Context, id string) error {
	if s.db == nil {
		return fmt.Errorf("database unavailable")
	}
	current, err := s.managedAlertRuleByID(id)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM monitor_alert_rule_definitions WHERE id=$1`, id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrNotFound
	}
	if err = s.syncManagedAlertRules(ctx); err != nil {
		_, _ = s.db.ExecContext(ctx, `INSERT INTO monitor_alert_rule_definitions(id,group_name,name,query,duration,severity,summary,description,enabled) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, current.ID, current.Group, current.Name, current.Query, current.Duration, current.Severity, current.Summary, current.Description, current.Enabled)
		return fmt.Errorf("apply Prometheus rule: %w", err)
	}
	return nil
}

func (s *Service) schedulePrometheusReloads() {
	go func() {
		delays := []time.Duration{15 * time.Second, 30 * time.Second, 45 * time.Second}
		for _, delay := range delays {
			time.Sleep(delay)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = s.ReloadPrometheusRules(ctx)
			cancel()
		}
	}()
}
func (s *Service) validateManagedPromQL(ctx context.Context, rule ManagedAlertRule) error {
	if s.prometheusURL == "" {
		return nil
	}
	endpoint := s.prometheusURL + "/api/v1/query?" + url.Values{"query": []string{rule.Query}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	var payload struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(response.Body).Decode(&payload)
	if payload.Error == "" {
		payload.Error = response.Status
	}
	return fmt.Errorf("PromQL validation failed for %s: %s", rule.Name, payload.Error)
}
func (s *Service) syncManagedAlertRules(ctx context.Context) error {
	rules, err := s.ManagedAlertRules()
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if rule.Enabled {
			if err = s.validateManagedPromQL(ctx, rule); err != nil {
				return err
			}
		}
	}
	if s.ruleConfigMap == nil {
		if s.ruleConfigMapErr != "" {
			return fmt.Errorf("Prometheus rule ConfigMap client is unavailable: %s", s.ruleConfigMapErr)
		}
		return fmt.Errorf("PROMETHEUS_RULES_NAMESPACE and PROMETHEUS_RULES_CONFIGMAP are not configured")
	}
	if err = s.ruleConfigMap.PatchConfigMapData(ctx, s.ruleConfigMapNS, s.ruleConfigMapName, managedRuleConfigKey, renderManagedAlertRules(rules)); err != nil {
		return err
	}
	s.schedulePrometheusReloads()
	_ = s.ReloadPrometheusRules(ctx)
	return nil
}

func (s *Service) ReloadPrometheusRules(ctx context.Context) error {
	if s.prometheusURL == "" {
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.prometheusURL+"/-/reload", nil)
	if err != nil {
		return err
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Prometheus reload returned %s", response.Status)
	}
	return nil
}

func renderManagedAlertRules(rules []ManagedAlertRule) string {
	enabled := make([]ManagedAlertRule, 0, len(rules))
	for _, rule := range rules {
		if rule.Enabled {
			enabled = append(enabled, rule)
		}
	}
	if len(enabled) == 0 {
		return "groups: []\n"
	}
	sort.SliceStable(enabled, func(i, j int) bool {
		if enabled[i].Group == enabled[j].Group {
			return enabled[i].Name < enabled[j].Name
		}
		return enabled[i].Group < enabled[j].Group
	})
	var builder strings.Builder
	builder.WriteString("groups:\n")
	for index := 0; index < len(enabled); {
		group := enabled[index].Group
		builder.WriteString("  - name: " + yamlString(group) + "\n")
		builder.WriteString("    rules:\n")
		for index < len(enabled) && enabled[index].Group == group {
			rule := enabled[index]
			builder.WriteString("      - alert: " + yamlString(rule.Name) + "\n")
			builder.WriteString("        expr: " + yamlString(rule.Query) + "\n")
			builder.WriteString("        for: " + yamlString(rule.Duration) + "\n")
			builder.WriteString("        labels:\n")
			builder.WriteString("          severity: " + yamlString(rule.Severity) + "\n")
			builder.WriteString("          category: \"custom\"\n")
			builder.WriteString("          managed: \"cmdb\"\n")
			builder.WriteString("        annotations:\n")
			builder.WriteString("          summary: " + yamlString(rule.Summary) + "\n")
			if rule.Description != "" {
				builder.WriteString("          description: " + yamlString(rule.Description) + "\n")
			}
			index++
		}
	}
	return builder.String()
}

func yamlString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
