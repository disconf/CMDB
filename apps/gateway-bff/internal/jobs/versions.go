package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
)

const versionHistorySchema = `
CREATE TABLE IF NOT EXISTS job_template_versions(
 template_id text NOT NULL,
 version integer NOT NULL,
 name text NOT NULL,
 category text NOT NULL,
 description text NOT NULL,
 command_text text NOT NULL,
 playbook_id text NOT NULL DEFAULT '',
 parameters jsonb NOT NULL DEFAULT '{}',
 risk text NOT NULL,
 enabled boolean NOT NULL DEFAULT true,
 created_by text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(template_id,version)
);
CREATE TABLE IF NOT EXISTS job_playbook_versions(
 playbook_id text NOT NULL,
 version integer NOT NULL,
 name text NOT NULL,
 description text NOT NULL,
 content text NOT NULL,
 constants jsonb NOT NULL DEFAULT '{}',
 variables jsonb NOT NULL DEFAULT '{}',
 enabled boolean NOT NULL DEFAULT true,
 created_by text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(playbook_id,version)
);
`

func (s *Service) recordTemplateVersionLocked(item Template, createdBy string) {
	record := TemplateVersion{Version: item.Version, CreatedBy: createdBy, CreatedAt: time.Now().Format("2006-01-02 15:04:05"), Template: cloneTemplate(item)}
	history := s.templateVersions[item.ID]
	replaced := false
	for i := range history {
		if history[i].Version == record.Version {
			history[i] = record
			replaced = true
			break
		}
	}
	if !replaced {
		history = append(history, record)
	}
	sort.Slice(history, func(i, j int) bool { return history[i].Version < history[j].Version })
	s.templateVersions[item.ID] = history
	if s.db != nil {
		_ = upsertTemplateVersion(context.Background(), s.db, record)
	}
}

func (s *Service) recordPlaybookVersionLocked(item Playbook, createdBy string) {
	record := PlaybookVersion{Version: item.Version, CreatedBy: createdBy, CreatedAt: time.Now().Format("2006-01-02 15:04:05"), Playbook: clonePlaybook(item)}
	history := s.playbookVersions[item.ID]
	replaced := false
	for i := range history {
		if history[i].Version == record.Version {
			history[i] = record
			replaced = true
			break
		}
	}
	if !replaced {
		history = append(history, record)
	}
	sort.Slice(history, func(i, j int) bool { return history[i].Version < history[j].Version })
	s.playbookVersions[item.ID] = history
	if s.db != nil {
		_ = upsertPlaybookVersion(context.Background(), s.db, record)
	}
}

func (s *Service) backfillVersionHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.templates {
		s.recordTemplateVersionLocked(item, "system")
	}
	for _, item := range s.playbooks {
		s.recordPlaybookVersionLocked(item, item.CreatedBy)
	}
}

func cloneTemplate(item Template) Template {
	item.Parameters = cloneStringMap(item.Parameters)
	return item
}

func (s *Service) TemplateVersions(id string) ([]TemplateVersion, error) {
	if s.db != nil {
		if items, err := loadTemplateVersions(context.Background(), s.db, id); err == nil && len(items) > 0 {
			s.mu.Lock()
			s.templateVersions[id] = items
			s.mu.Unlock()
			return append([]TemplateVersion(nil), items...), nil
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, ok := s.templateVersions[id]
	if !ok || len(items) == 0 {
		return nil, ErrNotFound
	}
	result := append([]TemplateVersion(nil), items...)
	for i := range result {
		result[i].Template = cloneTemplate(result[i].Template)
	}
	return result, nil
}

func (s *Service) PlaybookVersions(id string) ([]PlaybookVersion, error) {
	if s.db != nil {
		if items, err := loadPlaybookVersions(context.Background(), s.db, id); err == nil && len(items) > 0 {
			s.mu.Lock()
			s.playbookVersions[id] = items
			s.mu.Unlock()
			return append([]PlaybookVersion(nil), items...), nil
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, ok := s.playbookVersions[id]
	if !ok || len(items) == 0 {
		return nil, ErrNotFound
	}
	result := append([]PlaybookVersion(nil), items...)
	for i := range result {
		result[i].Playbook = clonePlaybook(result[i].Playbook)
	}
	return result, nil
}

func (s *Service) TemplateVersionDiff(id string, fromVersion, toVersion int) (VersionDiff, error) {
	items, err := s.TemplateVersions(id)
	if err != nil {
		return VersionDiff{}, err
	}
	from, okFrom := findTemplateVersion(items, fromVersion)
	to, okTo := findTemplateVersion(items, toVersion)
	if !okFrom || !okTo {
		return VersionDiff{}, ErrNotFound
	}
	return VersionDiff{ID: id, FromVersion: fromVersion, ToVersion: toVersion, Changes: diffTemplate(from.Template, to.Template)}, nil
}

func (s *Service) PlaybookVersionDiff(id string, fromVersion, toVersion int) (VersionDiff, error) {
	items, err := s.PlaybookVersions(id)
	if err != nil {
		return VersionDiff{}, err
	}
	from, okFrom := findPlaybookVersion(items, fromVersion)
	to, okTo := findPlaybookVersion(items, toVersion)
	if !okFrom || !okTo {
		return VersionDiff{}, ErrNotFound
	}
	return VersionDiff{ID: id, FromVersion: fromVersion, ToVersion: toVersion, Changes: diffPlaybook(from.Playbook, to.Playbook)}, nil
}

func findTemplateVersion(items []TemplateVersion, version int) (TemplateVersion, bool) {
	for _, item := range items {
		if item.Version == version {
			return item, true
		}
	}
	return TemplateVersion{}, false
}

func findPlaybookVersion(items []PlaybookVersion, version int) (PlaybookVersion, bool) {
	for _, item := range items {
		if item.Version == version {
			return item, true
		}
	}
	return PlaybookVersion{}, false
}

func diffTemplate(before, after Template) []VersionFieldDiff {
	var changes []VersionFieldDiff
	addVersionDiff(&changes, "name", before.Name, after.Name)
	addVersionDiff(&changes, "category", before.Category, after.Category)
	addVersionDiff(&changes, "description", before.Description, after.Description)
	addVersionDiff(&changes, "command", before.Command, after.Command)
	addVersionDiff(&changes, "playbookId", before.PlaybookID, after.PlaybookID)
	addVersionDiff(&changes, "parameters", formatStringMap(before.Parameters), formatStringMap(after.Parameters))
	addVersionDiff(&changes, "risk", before.Risk, after.Risk)
	addVersionDiff(&changes, "enabled", strconv.FormatBool(before.Enabled), strconv.FormatBool(after.Enabled))
	return changes
}

func diffPlaybook(before, after Playbook) []VersionFieldDiff {
	var changes []VersionFieldDiff
	addVersionDiff(&changes, "name", before.Name, after.Name)
	addVersionDiff(&changes, "description", before.Description, after.Description)
	addVersionDiff(&changes, "content", before.Content, after.Content)
	addVersionDiff(&changes, "constants", formatStringMap(before.Constants), formatStringMap(after.Constants))
	addVersionDiff(&changes, "variables", formatStringMap(before.Variables), formatStringMap(after.Variables))
	addVersionDiff(&changes, "enabled", strconv.FormatBool(before.Enabled), strconv.FormatBool(after.Enabled))
	return changes
}

func addVersionDiff(changes *[]VersionFieldDiff, field, before, after string) {
	if before == after {
		return
	}
	*changes = append(*changes, VersionFieldDiff{Field: field, Before: before, After: after})
}

func formatStringMap(input map[string]string) string {
	if len(input) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+input[key])
	}
	return strings.Join(parts, "\n")
}

func upsertTemplateVersion(ctx context.Context, db *sql.DB, item TemplateVersion) error {
	parameters, _ := json.Marshal(item.Template.Parameters)
	_, err := db.ExecContext(ctx, `INSERT INTO job_template_versions(template_id,version,name,category,description,command_text,playbook_id,parameters,risk,enabled,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now()) ON CONFLICT(template_id,version) DO UPDATE SET name=excluded.name,category=excluded.category,description=excluded.description,command_text=excluded.command_text,playbook_id=excluded.playbook_id,parameters=excluded.parameters,risk=excluded.risk,enabled=excluded.enabled,created_by=excluded.created_by,created_at=now()`, item.Template.ID, item.Version, item.Template.Name, item.Template.Category, item.Template.Description, item.Template.Command, item.Template.PlaybookID, parameters, item.Template.Risk, item.Template.Enabled, item.CreatedBy)
	return err
}

func loadTemplateVersions(ctx context.Context, db *sql.DB, id string) ([]TemplateVersion, error) {
	rows, err := db.QueryContext(ctx, `SELECT version,name,category,description,command_text,playbook_id,parameters,risk,enabled,created_by,to_char(created_at,'YYYY-MM-DD HH24:MI:SS') FROM job_template_versions WHERE template_id=$1 ORDER BY version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []TemplateVersion{}
	for rows.Next() {
		var item TemplateVersion
		var parameters []byte
		if err := rows.Scan(&item.Version, &item.Template.Name, &item.Template.Category, &item.Template.Description, &item.Template.Command, &item.Template.PlaybookID, &parameters, &item.Template.Risk, &item.Template.Enabled, &item.CreatedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Template.ID = id
		_ = json.Unmarshal(parameters, &item.Template.Parameters)
		items = append(items, item)
	}
	return items, rows.Err()
}

func upsertPlaybookVersion(ctx context.Context, db *sql.DB, item PlaybookVersion) error {
	constants, _ := json.Marshal(item.Playbook.Constants)
	variables, _ := json.Marshal(item.Playbook.Variables)
	_, err := db.ExecContext(ctx, `INSERT INTO job_playbook_versions(playbook_id,version,name,description,content,constants,variables,enabled,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,now()) ON CONFLICT(playbook_id,version) DO UPDATE SET name=excluded.name,description=excluded.description,content=excluded.content,constants=excluded.constants,variables=excluded.variables,enabled=excluded.enabled,created_by=excluded.created_by,created_at=now()`, item.Playbook.ID, item.Version, item.Playbook.Name, item.Playbook.Description, item.Playbook.Content, constants, variables, item.Playbook.Enabled, item.CreatedBy)
	return err
}

func loadPlaybookVersions(ctx context.Context, db *sql.DB, id string) ([]PlaybookVersion, error) {
	rows, err := db.QueryContext(ctx, `SELECT version,name,description,content,constants,variables,enabled,created_by,to_char(created_at,'YYYY-MM-DD HH24:MI:SS') FROM job_playbook_versions WHERE playbook_id=$1 ORDER BY version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PlaybookVersion{}
	for rows.Next() {
		var item PlaybookVersion
		var constants, variables []byte
		if err := rows.Scan(&item.Version, &item.Playbook.Name, &item.Playbook.Description, &item.Playbook.Content, &constants, &variables, &item.Playbook.Enabled, &item.CreatedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Playbook.ID = id
		_ = json.Unmarshal(constants, &item.Playbook.Constants)
		_ = json.Unmarshal(variables, &item.Playbook.Variables)
		items = append(items, item)
	}
	return items, rows.Err()
}
