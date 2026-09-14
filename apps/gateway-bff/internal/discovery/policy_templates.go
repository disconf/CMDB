package discovery

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type RemoteSecurityPolicyTemplateValues struct {
	Enabled                   bool     `json:"enabled"`
	MaxSessionMinutes         int      `json:"maxSessionMinutes"`
	MaxConcurrentSessions     int      `json:"maxConcurrentSessions"`
	RecordingRetentionDays    int      `json:"recordingRetentionDays"`
	FileTransferEnabled       bool     `json:"fileTransferEnabled"`
	UploadMaxMB               int      `json:"uploadMaxMB"`
	DownloadMaxMB             int      `json:"downloadMaxMB"`
	AllowedUploadPaths        []string `json:"allowedUploadPaths"`
	AllowedDownloadPaths      []string `json:"allowedDownloadPaths"`
	AllowControlCollaborators bool     `json:"allowControlCollaborators"`
	ApprovalMode              string   `json:"approvalMode"`
	ApprovalTTLMinutes        int      `json:"approvalTtlMinutes"`
}

type RemoteSecurityPolicyTemplate struct {
	ID             string                             `json:"id"`
	Name           string                             `json:"name"`
	Description    string                             `json:"description"`
	Enabled        bool                               `json:"enabled"`
	CurrentVersion int                                `json:"currentVersion"`
	Values         RemoteSecurityPolicyTemplateValues `json:"values"`
	CreatedBy      string                             `json:"createdBy"`
	UpdatedBy      string                             `json:"updatedBy"`
	CreatedAt      string                             `json:"createdAt"`
	UpdatedAt      string                             `json:"updatedAt"`
}

type RemoteSecurityPolicyTemplateInput struct {
	ID          string                             `json:"id"`
	Name        string                             `json:"name"`
	Description string                             `json:"description"`
	Enabled     *bool                              `json:"enabled"`
	Values      RemoteSecurityPolicyTemplateValues `json:"values"`
	ChangeNote  string                             `json:"changeNote"`
}

type RemoteSecurityPolicyTemplateVersion struct {
	TemplateID  string                             `json:"templateId"`
	Version     int                                `json:"version"`
	Name        string                             `json:"name"`
	Description string                             `json:"description"`
	Values      RemoteSecurityPolicyTemplateValues `json:"values"`
	ChangeNote  string                             `json:"changeNote"`
	CreatedBy   string                             `json:"createdBy"`
	CreatedAt   string                             `json:"createdAt"`
}

func remoteSecurityPolicyTemplateID() string {
	data := make([]byte, 9)
	_, _ = rand.Read(data)
	return "rtpl-" + hex.EncodeToString(data)
}

func templateValuesFromPolicy(item RemoteSecurityPolicy) RemoteSecurityPolicyTemplateValues {
	return RemoteSecurityPolicyTemplateValues{
		Enabled: item.Enabled, MaxSessionMinutes: item.MaxSessionMinutes, MaxConcurrentSessions: item.MaxConcurrentSessions,
		RecordingRetentionDays: item.RecordingRetentionDays, FileTransferEnabled: item.FileTransferEnabled,
		UploadMaxMB: item.UploadMaxMB, DownloadMaxMB: item.DownloadMaxMB,
		AllowedUploadPaths: append([]string(nil), item.AllowedUploadPaths...), AllowedDownloadPaths: append([]string(nil), item.AllowedDownloadPaths...),
		AllowControlCollaborators: item.AllowControlCollaborators, ApprovalMode: item.ApprovalMode, ApprovalTTLMinutes: item.ApprovalTTLMinutes,
	}
}

func defaultPolicyTemplates() []RemoteSecurityPolicyTemplate {
	standard := defaultRemoteSecurityPolicy()
	standard.Name = "标准运维"
	strict := defaultRemoteSecurityPolicy()
	strict.Name = "严格审计"
	strict.MaxSessionMinutes, strict.MaxConcurrentSessions, strict.RecordingRetentionDays = 15, 5, 90
	strict.FileTransferEnabled, strict.UploadMaxMB, strict.DownloadMaxMB = false, 10, 10
	strict.AllowedUploadPaths, strict.AllowedDownloadPaths = []string{"/tmp/**"}, []string{"/var/log/**"}
	strict.AllowControlCollaborators, strict.ApprovalMode, strict.ApprovalTTLMinutes = false, "all", 5
	emergency := defaultRemoteSecurityPolicy()
	emergency.Name = "应急运维"
	emergency.MaxSessionMinutes, emergency.MaxConcurrentSessions, emergency.RecordingRetentionDays = 10, 10, 365
	emergency.AllowedUploadPaths, emergency.AllowedDownloadPaths = []string{"/tmp/**"}, []string{"/var/log/**"}
	emergency.ApprovalTTLMinutes = 3
	now := time.Now().Format("2006-01-02 15:04:05")
	return []RemoteSecurityPolicyTemplate{
		{ID: "rtpl-standard", Name: "标准运维", Description: "30 分钟、风险审批、允许文件传输", Enabled: true, CurrentVersion: 1, Values: templateValuesFromPolicy(standard), CreatedBy: "system", UpdatedBy: "system", CreatedAt: now, UpdatedAt: now},
		{ID: "rtpl-strict", Name: "严格审计", Description: "全部命令审批、限制文件传输、缩短录像保留", Enabled: true, CurrentVersion: 1, Values: templateValuesFromPolicy(strict), CreatedBy: "system", UpdatedBy: "system", CreatedAt: now, UpdatedAt: now},
		{ID: "rtpl-break-glass", Name: "应急运维", Description: "短时高并发、风险审批、仅关键人员使用", Enabled: true, CurrentVersion: 1, Values: templateValuesFromPolicy(emergency), CreatedBy: "system", UpdatedBy: "system", CreatedAt: now, UpdatedAt: now},
	}
}

func clonePolicyTemplate(item RemoteSecurityPolicyTemplate) RemoteSecurityPolicyTemplate {
	item.Values.AllowedUploadPaths = append([]string(nil), item.Values.AllowedUploadPaths...)
	item.Values.AllowedDownloadPaths = append([]string(nil), item.Values.AllowedDownloadPaths...)
	return item
}

func (s *Service) RemoteSecurityPolicyTemplates() []RemoteSecurityPolicyTemplate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RemoteSecurityPolicyTemplate, 0, len(s.policyTemplates))
	for _, item := range s.policyTemplates {
		out = append(out, clonePolicyTemplate(item))
	}
	return out
}

func (s *Service) SaveRemoteSecurityPolicyTemplate(input RemoteSecurityPolicyTemplateInput, actor string) (RemoteSecurityPolicyTemplate, error) {
	values, err := normalizePolicyTemplateValues(input.Values)
	if err != nil {
		return RemoteSecurityPolicyTemplate{}, err
	}
	name, description, changeNote := strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), strings.TrimSpace(input.ChangeNote)
	if name == "" || len(name) > 100 || len(description) > 500 || len(changeNote) > 500 {
		return RemoteSecurityPolicyTemplate{}, ErrRemoteValidation
	}
	if len(values.AllowedUploadPaths) == 0 || len(values.AllowedDownloadPaths) == 0 {
		return RemoteSecurityPolicyTemplate{}, ErrRemoteValidation
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Format("2006-01-02 15:04:05")
	item := RemoteSecurityPolicyTemplate{ID: strings.TrimSpace(input.ID), Name: name, Description: description, Enabled: true, CurrentVersion: 1, Values: values, CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}
	if input.Enabled != nil {
		item.Enabled = *input.Enabled
	}
	if item.ID == "" {
		item.ID = remoteSecurityPolicyTemplateID()
	} else {
		for i := range s.policyTemplates {
			if s.policyTemplates[i].ID != item.ID {
				continue
			}
			item.CurrentVersion = s.policyTemplates[i].CurrentVersion + 1
			item.Enabled = s.policyTemplates[i].Enabled
			item.CreatedBy, item.CreatedAt = s.policyTemplates[i].CreatedBy, s.policyTemplates[i].CreatedAt
			if input.Enabled != nil {
				item.Enabled = *input.Enabled
			}
			break
		}
	}
	version := RemoteSecurityPolicyTemplateVersion{TemplateID: item.ID, Version: item.CurrentVersion, Name: item.Name, Description: item.Description, Values: item.Values, ChangeNote: changeNote, CreatedBy: actor, CreatedAt: now}
	if err = s.persistPolicyTemplate(item, version); err != nil {
		return RemoteSecurityPolicyTemplate{}, err
	}
	replaced := false
	for i := range s.policyTemplates {
		if s.policyTemplates[i].ID == item.ID {
			s.policyTemplates[i] = clonePolicyTemplate(item)
			replaced = true
			break
		}
	}
	if !replaced {
		s.policyTemplates = append([]RemoteSecurityPolicyTemplate{clonePolicyTemplate(item)}, s.policyTemplates...)
	}
	return clonePolicyTemplate(item), nil
}

func normalizePolicyTemplateValues(values RemoteSecurityPolicyTemplateValues) (RemoteSecurityPolicyTemplateValues, error) {
	enabled := values.Enabled
	input := RemoteSecurityPolicyInput{
		Name: "template", SubjectType: "user", Subject: "template", Enabled: &enabled,
		MaxSessionMinutes: values.MaxSessionMinutes, MaxConcurrentSessions: values.MaxConcurrentSessions,
		RecordingRetentionDays: values.RecordingRetentionDays, FileTransferEnabled: values.FileTransferEnabled,
		UploadMaxMB: values.UploadMaxMB, DownloadMaxMB: values.DownloadMaxMB,
		AllowedUploadPaths: values.AllowedUploadPaths, AllowedDownloadPaths: values.AllowedDownloadPaths,
		AllowControlCollaborators: values.AllowControlCollaborators, ApprovalMode: values.ApprovalMode, ApprovalTTLMinutes: values.ApprovalTTLMinutes,
	}
	item, err := normalizeRemoteSecurityPolicy(input, "template")
	if err != nil {
		return RemoteSecurityPolicyTemplateValues{}, err
	}
	return templateValuesFromPolicy(item), nil
}

func (s *Service) RemoteSecurityPolicyTemplateVersions(id string) ([]RemoteSecurityPolicyTemplateVersion, error) {
	if s.db == nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		for _, item := range s.policyTemplates {
			if item.ID == id {
				return []RemoteSecurityPolicyTemplateVersion{{TemplateID: item.ID, Version: item.CurrentVersion, Name: item.Name, Description: item.Description, Values: item.Values, CreatedBy: item.UpdatedBy, CreatedAt: item.UpdatedAt}}, nil
			}
		}
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(`SELECT template_id,version,name,description,policy_values,change_note,created_by,to_char(created_at,'YYYY-MM-DD HH24:MI:SS') FROM remote_security_policy_template_versions WHERE template_id=$1 ORDER BY version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RemoteSecurityPolicyTemplateVersion{}
	for rows.Next() {
		var item RemoteSecurityPolicyTemplateVersion
		var raw []byte
		if err = rows.Scan(&item.TemplateID, &item.Version, &item.Name, &item.Description, &raw, &item.ChangeNote, &item.CreatedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &item.Values); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

func (s *Service) RollbackRemoteSecurityPolicyTemplate(id string, version int, changeNote, actor string) (RemoteSecurityPolicyTemplate, error) {
	versions, err := s.RemoteSecurityPolicyTemplateVersions(id)
	if err != nil {
		return RemoteSecurityPolicyTemplate{}, err
	}
	var source *RemoteSecurityPolicyTemplateVersion
	for i := range versions {
		if versions[i].Version == version {
			source = &versions[i]
			break
		}
	}
	if source == nil {
		return RemoteSecurityPolicyTemplate{}, ErrNotFound
	}
	if strings.TrimSpace(changeNote) == "" {
		changeNote = fmt.Sprintf("回滚到版本 %d", version)
	}
	enabled := true
	return s.SaveRemoteSecurityPolicyTemplate(RemoteSecurityPolicyTemplateInput{ID: id, Name: source.Name, Description: source.Description, Enabled: &enabled, Values: source.Values, ChangeNote: changeNote}, actor)
}

func (s *Service) ToggleRemoteSecurityPolicyTemplate(id string) (RemoteSecurityPolicyTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.policyTemplates {
		if s.policyTemplates[i].ID != id {
			continue
		}
		s.policyTemplates[i].Enabled = !s.policyTemplates[i].Enabled
		s.policyTemplates[i].UpdatedBy = "system-toggle"
		s.policyTemplates[i].UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
		if s.db != nil {
			if _, err := s.db.Exec(`UPDATE remote_security_policy_templates SET enabled=$2,updated_by=$3,updated_at=now() WHERE id=$1`, id, s.policyTemplates[i].Enabled, s.policyTemplates[i].UpdatedBy); err != nil {
				return RemoteSecurityPolicyTemplate{}, err
			}
		}
		return clonePolicyTemplate(s.policyTemplates[i]), nil
	}
	return RemoteSecurityPolicyTemplate{}, ErrNotFound
}

func (s *Service) DeleteRemoteSecurityPolicyTemplate(id string) error {
	if strings.HasPrefix(id, "rtpl-") && s.db == nil {
		return ErrRemoteValidation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.policyTemplates {
		if s.policyTemplates[i].ID != id {
			continue
		}
		if s.policyTemplates[i].CreatedBy == "system" {
			return fmt.Errorf("%w: built-in template cannot be deleted", ErrRemoteValidation)
		}
		if s.db != nil {
			if _, err := s.db.Exec(`DELETE FROM remote_security_policy_templates WHERE id=$1`, id); err != nil {
				return err
			}
		}
		s.policyTemplates = append(s.policyTemplates[:i], s.policyTemplates[i+1:]...)
		return nil
	}
	return ErrNotFound
}

func (s *Service) loadRemoteSecurityPolicyTemplates() error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.Query(`SELECT t.id,t.name,t.description,t.enabled,t.current_version,v.policy_values,t.created_by,t.updated_by,to_char(t.created_at,'YYYY-MM-DD HH24:MI:SS'),to_char(t.updated_at,'YYYY-MM-DD HH24:MI:SS') FROM remote_security_policy_templates t JOIN remote_security_policy_template_versions v ON v.template_id=t.id AND v.version=t.current_version ORDER BY t.created_at,t.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []RemoteSecurityPolicyTemplate{}
	for rows.Next() {
		var item RemoteSecurityPolicyTemplate
		var raw []byte
		if err = rows.Scan(&item.ID, &item.Name, &item.Description, &item.Enabled, &item.CurrentVersion, &raw, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &item.Values); err != nil {
			return err
		}
		out = append(out, clonePolicyTemplate(item))
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(out) == 0 {
		for _, item := range defaultPolicyTemplates() {
			version := RemoteSecurityPolicyTemplateVersion{TemplateID: item.ID, Version: 1, Name: item.Name, Description: item.Description, Values: item.Values, ChangeNote: "系统内置模板", CreatedBy: "system", CreatedAt: item.CreatedAt}
			if err = s.persistPolicyTemplate(item, version); err != nil {
				return err
			}
			out = append(out, clonePolicyTemplate(item))
		}
	}
	s.policyTemplates = out
	return nil
}

func (s *Service) persistPolicyTemplate(item RemoteSecurityPolicyTemplate, version RemoteSecurityPolicyTemplateVersion) error {
	if s.db == nil {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	raw, err := json.Marshal(version.Values)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO remote_security_policy_templates(id,name,description,enabled,current_version,created_by,updated_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,now(),now()) ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,enabled=excluded.enabled,current_version=excluded.current_version,updated_by=excluded.updated_by,updated_at=now()`, item.ID, item.Name, item.Description, item.Enabled, item.CurrentVersion, item.CreatedBy, item.UpdatedBy); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO remote_security_policy_template_versions(template_id,version,name,description,policy_values,change_note,created_by) VALUES($1,$2,$3,$4,$5,$6,$7)`, item.ID, item.CurrentVersion, item.Name, item.Description, raw, version.ChangeNote, version.CreatedBy); err != nil {
		return err
	}
	return tx.Commit()
}
